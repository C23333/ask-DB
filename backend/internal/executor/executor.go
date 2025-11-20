package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/yourusername/db_asst/config"
	"github.com/yourusername/db_asst/internal/db"
	"github.com/yourusername/db_asst/internal/models"
)

type SQLExecutor struct {
	dbClient        *db.OracleClient
	logger          *zap.Logger
	timeout         time.Duration
	defaultPageSize int
	maxPageSize     int
	sensitiveCols   map[string]struct{}
}

func New(dbClient *db.OracleClient, cfg *config.Config, logger *zap.Logger) *SQLExecutor {
	exec := &SQLExecutor{
		dbClient: dbClient,
		logger:   logger,
		timeout:  30 * time.Second,
	}
	if cfg != nil {
		if cfg.SQLDefaultPageSize > 0 {
			exec.defaultPageSize = cfg.SQLDefaultPageSize
		}
		if cfg.SQLMaxPageSize > 0 {
			exec.maxPageSize = cfg.SQLMaxPageSize
		}
		if len(cfg.SensitiveColumns) > 0 {
			exec.sensitiveCols = make(map[string]struct{})
			for _, col := range cfg.SensitiveColumns {
				if trimmed := strings.TrimSpace(col); trimmed != "" {
					exec.sensitiveCols[strings.ToUpper(trimmed)] = struct{}{}
				}
			}
		}
	}
	if exec.defaultPageSize <= 0 {
		exec.defaultPageSize = 50
	}
	if exec.maxPageSize <= 0 {
		exec.maxPageSize = 200
	}
	if exec.maxPageSize < exec.defaultPageSize {
		exec.maxPageSize = exec.defaultPageSize
	}
	return exec
}

// ExecuteSQL executes a SQL query with safety checks
func (e *SQLExecutor) ExecuteSQL(ctx context.Context, req models.SQLExecuteRequest) (*models.SQLExecuteResponse, error) {
	sql := req.SQL
	// Validate and sanitize SQL
	validationErr := e.ValidateSQL(sql)
	if validationErr != nil {
		return &models.SQLExecuteResponse{
			Success: false,
			Error:   validationErr.Error(),
		}, nil
	}

	// Apply timeout
	execTimeout := e.timeout
	if req.Timeout > 0 && req.Timeout < 300 {
		execTimeout = time.Duration(req.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = e.defaultPageSize
	}
	if pageSize > e.maxPageSize {
		pageSize = e.maxPageSize
	}
	offset := (page - 1) * pageSize

	// Execute the query
	result, err := e.dbClient.ExecuteQueryRange(ctx, sql, offset, pageSize)
	if err != nil {
		e.logger.Error("Failed to execute SQL", zap.String("sql", sql), zap.Error(err))
	}
	if result != nil {
		result.Page = page
		result.PageSize = pageSize
		e.applyMasking(result)
	}

	return result, nil
}

// ValidateSQL performs security checks on SQL
func (e *SQLExecutor) ValidateSQL(sql string) error {
	if sql == "" {
		return fmt.Errorf("SQL query cannot be empty")
	}

	// Trim whitespace
	sql = strings.TrimSpace(sql)

	// 1. Check if only SELECT statements are allowed
	if !isSelectQuery(sql) {
		return fmt.Errorf("only SELECT queries are allowed")
	}

	// 2. Check for dangerous keywords
	if hasDangerousKeywords(sql) {
		return fmt.Errorf("query contains forbidden keywords")
	}

	// 3. Check for SQL injection patterns
	if hasSQLInjectionPatterns(sql) {
		return fmt.Errorf("potential SQL injection detected")
	}

	// 4. Check for suspicious patterns (like stacked queries)
	if hasStackedQueries(sql) {
		return fmt.Errorf("stacked queries are not allowed")
	}

	return nil
}

// isSelectQuery checks if the query is a SELECT statement
func isSelectQuery(sql string) bool {
	// Normalize the SQL
	normalized := strings.ToUpper(strings.TrimSpace(sql))

	// Allow WITH clauses before SELECT (CTEs)
	if strings.HasPrefix(normalized, "WITH ") {
		// Make sure it eventually has SELECT
		return strings.Contains(normalized, "SELECT")
	}

	// Check if it starts with SELECT or parenthesis (subquery)
	return strings.HasPrefix(normalized, "SELECT") ||
		strings.HasPrefix(normalized, "(")
}

// hasDangerousKeywords checks for dangerous SQL keywords
func hasDangerousKeywords(sql string) bool {
	dangerous := []string{
		"INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER",
		"TRUNCATE", "GRANT", "REVOKE", "EXEC", "EXECUTE",
		"PRAGMA", "VACUUM", "ANALYZE", "ATTACH", "DETACH",
	}

	upperSQL := strings.ToUpper(sql)

	for _, keyword := range dangerous {
		// Use word boundaries to avoid false positives
		pattern := regexp.MustCompile(`(?:^|[\s\(;])(` + keyword + `)(?:[\s\(;]|$)`)
		if pattern.MatchString(upperSQL) {
			return true
		}
	}

	return false
}

// hasSQLInjectionPatterns checks for common SQL injection patterns
func hasSQLInjectionPatterns(sql string) bool {
	patterns := []string{
		`'.*--`,                        // SQL comment
		`'.*;`,                         // Statement terminator in string
		`(\bOR\b|\bAND\b).*'?\s*=\s*'`, // OR/AND conditions
		`UNION.*SELECT`,                // UNION based injection
		`'.*OR.*'='`,                   // Classic injection
		`"\s*OR\s*"`,                   // Double quote injection
		`/\*.*\*/`,                     // Comment injection
		`xp_`,                          // Extended stored procedures
		`sp_`,                          // System stored procedures
	}

	upperSQL := strings.ToUpper(sql)

	for _, pattern := range patterns {
		regex := regexp.MustCompile(pattern)
		if regex.MatchString(upperSQL) {
			// Whitelist: UNION is allowed in legitimate queries
			if pattern == `UNION.*SELECT` && !hasMultipleRoots(sql) {
				continue
			}
		}
	}

	return false
}

// hasStackedQueries checks for stacked/batched queries
func hasStackedQueries(sql string) bool {
	// Remove comments
	commentRegex := regexp.MustCompile(`/\*.*?\*/|--.*?$`)
	cleaned := commentRegex.ReplaceAllString(sql, "")

	// Count semicolons that are not in strings
	inString := false
	stringChar := rune(0)
	var semicolonCount int

	for i, char := range cleaned {
		// Handle string literals
		if (char == '\'' || char == '"') && (i == 0 || cleaned[i-1] != '\\') {
			if !inString {
				inString = true
				stringChar = char
			} else if char == stringChar {
				inString = false
			}
		}

		if char == ';' && !inString {
			semicolonCount++
		}
	}

	// More than one semicolon suggests stacked queries
	return semicolonCount > 1
}

// hasMultipleRoots checks if a query has multiple independent SELECT statements
func hasMultipleRoots(sql string) bool {
	// This is a simplified check - a real implementation would use a SQL parser
	selectCount := 0
	upperSQL := strings.ToUpper(sql)

	// Count SELECT keywords that are not inside parentheses
	parenDepth := 0
	for i := 0; i < len(upperSQL); i++ {
		if upperSQL[i] == '(' {
			parenDepth++
		} else if upperSQL[i] == ')' {
			parenDepth--
		} else if parenDepth == 0 && strings.HasPrefix(upperSQL[i:], "SELECT") {
			selectCount++
		}
	}

	return selectCount > 1
}

// FormatSQL formats SQL for display
func (e *SQLExecutor) FormatSQL(sql string) string {
	// Basic SQL formatting
	sql = strings.TrimSpace(sql)

	// Normalize whitespace
	whitespaceRegex := regexp.MustCompile(`\s+`)
	sql = whitespaceRegex.ReplaceAllString(sql, " ")

	return sql
}

func (e *SQLExecutor) applyMasking(resp *models.SQLExecuteResponse) {
	if resp == nil || !resp.Success || len(e.sensitiveCols) == 0 {
		return
	}
	var indexes []int
	var masked []string
	for idx, col := range resp.Columns {
		keys := maskingKeys(col)
		for _, key := range keys {
			if _, ok := e.sensitiveCols[key]; ok {
				indexes = append(indexes, idx)
				masked = append(masked, col)
				break
			}
		}
	}
	if len(indexes) == 0 {
		return
	}
	for i := range resp.Rows {
		for _, idx := range indexes {
			if idx < len(resp.Rows[i]) && resp.Rows[i][idx] != nil {
				resp.Rows[i][idx] = "***"
			}
		}
	}
	resp.MaskedColumns = masked
}

func maskingKeys(col string) []string {
	col = strings.TrimSpace(col)
	upper := strings.ToUpper(col)
	if !strings.HasSuffix(col, ")") {
		return []string{upper}
	}
	openIdx := strings.LastIndex(col, "(")
	if openIdx == -1 || openIdx >= len(col)-1 {
		return []string{upper}
	}
	original := strings.TrimSpace(col[openIdx+1 : len(col)-1])
	if original == "" {
		return []string{upper}
	}
	return []string{upper, strings.ToUpper(original)}
}

// ExplainSQL provides execution plan information
func (e *SQLExecutor) ExplainSQL(ctx context.Context, sql string) (map[string]interface{}, error) {
	// Validate SQL first
	if err := e.ValidateSQL(sql); err != nil {
		return nil, err
	}

	// In Oracle, use EXPLAIN PLAN
	explainSQL := fmt.Sprintf("EXPLAIN PLAN FOR %s", sql)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := e.dbClient.ExecuteQuery(ctx, explainSQL)
	if err != nil {
		return nil, err
	}

	// Try to retrieve the plan
	planSQL := `
		SELECT PLAN_TABLE_OUTPUT
		FROM TABLE(DBMS_XPLAN.DISPLAY())
	`

	result, err := e.dbClient.ExecuteQuery(ctx, planSQL)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"plan":      result.Rows,
		"row_count": result.RowCount,
	}, nil
}
