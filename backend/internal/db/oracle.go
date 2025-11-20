package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/sijms/go-ora/v2"
	"go.uber.org/zap"

	"github.com/yourusername/db_asst/config"
	"github.com/yourusername/db_asst/internal/models"
)

type OracleClient struct {
	db     *sql.DB
	logger *zap.Logger
	schema string
	mu     sync.RWMutex
	// columnCommentCache caches COLUMN_NAME -> comment for quick decoration
	columnCommentCache map[string]string
}

var (
	oracleInstance *OracleClient
	once           sync.Once
)

// Initialize creates a singleton Oracle connection pool
func Initialize(cfg *config.Config, logger *zap.Logger) (*OracleClient, error) {
	var err error
	once.Do(func() {
		connStr := cfg.GetOracleConnStr()
		schema := cfg.GetOracleSchema()
		logger.Info("Connecting to Oracle database",
			zap.String("host", cfg.OracleHost),
			zap.String("schema", schema),
		)

		// Use go-ora/v2 driver
		db, connErr := sql.Open("oracle", connStr)
		if connErr != nil {
			err = fmt.Errorf("failed to open Oracle connection: %w", connErr)
			return
		}

		// Test the connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if pingErr := db.PingContext(ctx); pingErr != nil {
			err = fmt.Errorf("failed to ping Oracle database: %w", pingErr)
			return
		}

		// Set connection pool parameters
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

		oracleInstance = &OracleClient{
			db:     db,
			logger: logger,
			schema: schema,
		}

		logger.Info("Oracle database connected successfully")
	})

	return oracleInstance, err
}

// GetInstance returns the singleton Oracle client
func GetInstance() *OracleClient {
	return oracleInstance
}

// Close closes the database connection
func (c *OracleClient) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// GetAllTables returns all tables in the database
func (c *OracleClient) GetAllTables(ctx context.Context, excludeSystemTables bool) ([]string, error) {
	query := `
		SELECT TABLE_NAME
		FROM ALL_TABLES
		WHERE OWNER = :1
		ORDER BY TABLE_NAME
	`

	rows, err := c.db.QueryContext(ctx, query, c.schema)
	if err != nil {
		c.logger.Error("Failed to query tables", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}

	c.logger.Info("Fetched table list",
		zap.Int("count", len(tables)),
		zap.String("schema", c.schema))

	return tables, rows.Err()
}

// GetTableSchema returns the schema information for a specific table
func (c *OracleClient) GetTableSchema(ctx context.Context, tableName string) (*models.TableSchema, error) {
	// Get table comment
	commentQuery := `
		SELECT COMMENTS
		FROM ALL_TAB_COMMENTS
		WHERE TABLE_NAME = :1
		  AND OWNER = :2
	`

	var comment sql.NullString
	err := c.db.QueryRowContext(ctx, commentQuery, tableName, c.schema).Scan(&comment)
	if err != nil && err != sql.ErrNoRows {
		c.logger.Warn("Failed to get table comment", zap.String("table", tableName), zap.Error(err))
	}

	// Get columns
	columns, err := c.GetTableColumns(ctx, tableName)
	if err != nil {
		return nil, err
	}

	schema := &models.TableSchema{
		TableName: tableName,
		Comment:   comment.String,
		Columns:   columns,
		CreatedAt: time.Now(),
	}

	c.logger.Debug("Table schema retrieved",
		zap.String("table", tableName),
		zap.Int("columns", len(columns)))

	return schema, nil
}

// GetTableColumns returns the column information for a specific table
func (c *OracleClient) GetTableColumns(ctx context.Context, tableName string) ([]models.ColumnInfo, error) {
	query := `
		SELECT
			c.COLUMN_NAME,
			c.DATA_TYPE,
			c.DATA_LENGTH,
			CASE WHEN c.NULLABLE = 'N' THEN 0 ELSE 1 END AS NULLABLE,
			COALESCE(cc.COMMENTS, '') AS COMMENTS,
			CASE WHEN pk.CONSTRAINT_NAME IS NOT NULL THEN 1 ELSE 0 END AS IS_PK,
			CASE WHEN idx.INDEX_NAME IS NOT NULL THEN 1 ELSE 0 END AS IS_INDEX
		FROM ALL_TAB_COLUMNS c
		LEFT JOIN ALL_COL_COMMENTS cc
			ON c.OWNER = cc.OWNER
			AND c.TABLE_NAME = cc.TABLE_NAME
			AND c.COLUMN_NAME = cc.COLUMN_NAME
		LEFT JOIN (
			SELECT acc.OWNER, acc.TABLE_NAME, acc.COLUMN_NAME, acc.CONSTRAINT_NAME
			FROM ALL_CONSTRAINTS ac
			JOIN ALL_CONS_COLUMNS acc
				ON ac.OWNER = acc.OWNER
				AND ac.CONSTRAINT_NAME = acc.CONSTRAINT_NAME
			WHERE ac.CONSTRAINT_TYPE = 'P'
		) pk
			ON c.OWNER = pk.OWNER
			AND c.TABLE_NAME = pk.TABLE_NAME
			AND c.COLUMN_NAME = pk.COLUMN_NAME
		LEFT JOIN ALL_IND_COLUMNS idx
			ON c.TABLE_NAME = idx.TABLE_NAME
			AND c.COLUMN_NAME = idx.COLUMN_NAME
			AND c.OWNER = idx.TABLE_OWNER
		WHERE c.TABLE_NAME = :1
		  AND c.OWNER = :2
		ORDER BY c.COLUMN_ID
	`

	rows, err := c.db.QueryContext(ctx, query, tableName, c.schema)
	if err != nil {
		c.logger.Error("Failed to query columns", zap.String("table", tableName), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var columns []models.ColumnInfo
	for rows.Next() {
		var (
			columnName   string
			dataType     string
			dataLength   int
			nullable     int
			comments     string
			isPrimaryKey int
			isIndex      int
		)

		if err := rows.Scan(&columnName, &dataType, &dataLength, &nullable, &comments, &isPrimaryKey, &isIndex); err != nil {
			return nil, err
		}

		columns = append(columns, models.ColumnInfo{
			ColumnName:   columnName,
			DataType:     dataType,
			ColumnSize:   dataLength,
			Nullable:     nullable == 1,
			Comment:      comments,
			IsPrimaryKey: isPrimaryKey == 1,
			IsIndex:      isIndex == 1,
		})
	}

	c.logger.Debug("Table columns retrieved",
		zap.String("table", tableName),
		zap.Int("columns", len(columns)))

	return columns, rows.Err()
}

// ExecuteQuery executes a SELECT query and returns the results
func (c *OracleClient) ExecuteQuery(ctx context.Context, query string) (*models.SQLExecuteResponse, error) {
	start := time.Now()

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		c.logger.Error("Failed to execute query", zap.String("query", query), zap.Error(err))
		return &models.SQLExecuteResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return &models.SQLExecuteResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	columns = c.decorateColumns(ctx, columns)

	// Fetch all rows
	var result [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return &models.SQLExecuteResponse{
				Success: false,
				Error:   err.Error(),
			}, nil
		}

		result = append(result, values)
	}

	execTime := time.Since(start).Milliseconds()

	return &models.SQLExecuteResponse{
		Success:  true,
		Columns:  columns,
		Rows:     result,
		RowCount: len(result),
		ExecTime: execTime,
	}, rows.Err()
}

// ExecuteQueryRange executes a query with pagination
func (c *OracleClient) ExecuteQueryRange(ctx context.Context, query string, offset int, limit int) (*models.SQLExecuteResponse, error) {
	if limit <= 0 {
		return c.ExecuteQuery(ctx, query)
	}
	start := time.Now()
	sanitized := strings.TrimSpace(query)
	sanitized = strings.TrimSuffix(sanitized, ";")
	if sanitized == "" {
		return &models.SQLExecuteResponse{
			Success: false,
			Error:   "query cannot be empty",
		}, nil
	}
	if offset < 0 {
		offset = 0
	}
	maxRow := offset + limit + 1
	wrapped := fmt.Sprintf(`SELECT * FROM (
    SELECT inner_query.*, ROWNUM rnum FROM (%s) inner_query WHERE ROWNUM <= :1
) WHERE rnum > :2`, sanitized)

	rows, err := c.db.QueryContext(ctx, wrapped, maxRow, offset)
	if err != nil {
		c.logger.Error("Failed to execute paginated query", zap.Error(err))
		return &models.SQLExecuteResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return &models.SQLExecuteResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	rowNumIdx := -1
	for idx, col := range columns {
		if strings.EqualFold(col, "RNUM") {
			rowNumIdx = idx
			break
		}
	}

	var resultRows [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return &models.SQLExecuteResponse{
				Success: false,
				Error:   err.Error(),
			}, nil
		}
		if rowNumIdx >= 0 && rowNumIdx < len(values) {
			values = append(values[:rowNumIdx], values[rowNumIdx+1:]...)
		}
		resultRows = append(resultRows, values)
	}

	hasMore := false
	if len(resultRows) > limit {
		hasMore = true
		resultRows = resultRows[:limit]
	}
	if rowNumIdx >= 0 && rowNumIdx < len(columns) {
		columns = append(columns[:rowNumIdx], columns[rowNumIdx+1:]...)
	}
	columns = c.decorateColumns(ctx, columns)

	execTime := time.Since(start).Milliseconds()
	return &models.SQLExecuteResponse{
		Success:  true,
		Columns:  columns,
		Rows:     resultRows,
		RowCount: len(resultRows),
		ExecTime: execTime,
		HasMore:  hasMore,
	}, rows.Err()
}

func (c *OracleClient) decorateColumns(ctx context.Context, columns []string) []string {
	if len(columns) == 0 {
		return columns
	}
	commentMap, err := c.lookupColumnComments(ctx, columns)
	if err != nil {
		c.logger.Warn("Failed to load column comments", zap.Error(err))
		return columns
	}
	decorated := make([]string, len(columns))
	for i, col := range columns {
		key := strings.ToUpper(strings.TrimSpace(col))
		if comment, ok := commentMap[key]; ok && comment != "" {
			decorated[i] = fmt.Sprintf("%s(%s)", comment, col)
		} else {
			decorated[i] = col
		}
	}
	return decorated
}

func (c *OracleClient) lookupColumnComments(ctx context.Context, columns []string) (map[string]string, error) {
	unique := make([]string, 0, len(columns))
	seen := make(map[string]struct{})
	for _, col := range columns {
		key := strings.ToUpper(strings.TrimSpace(col))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}
	if len(unique) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(unique))

	var toFetch []string
	c.mu.RLock()
	if c.columnCommentCache == nil {
		toFetch = append(toFetch, unique...)
	} else {
		for _, key := range unique {
			if comment, ok := c.columnCommentCache[key]; ok {
				if comment != "" {
					result[key] = comment
				}
				continue
			}
			toFetch = append(toFetch, key)
		}
	}
	c.mu.RUnlock()

	if len(toFetch) > 0 {
		fetched, err := c.fetchColumnComments(ctx, toFetch)
		if err != nil {
			return result, err
		}
		c.mu.Lock()
		if c.columnCommentCache == nil {
			c.columnCommentCache = make(map[string]string)
		}
		for key, comment := range fetched {
			c.columnCommentCache[key] = comment
			if comment != "" {
				result[key] = comment
			}
		}
		for _, key := range toFetch {
			if _, ok := fetched[key]; !ok {
				c.columnCommentCache[key] = ""
			}
		}
		c.mu.Unlock()
	}

	return result, nil
}

func (c *OracleClient) fetchColumnComments(ctx context.Context, columns []string) (map[string]string, error) {
	const chunkSize = 900 // stay well below Oracle IN clause limit
	result := make(map[string]string)
	for start := 0; start < len(columns); start += chunkSize {
		end := start + chunkSize
		if end > len(columns) {
			end = len(columns)
		}
		chunk := columns[start:end]
		chunkResult, err := c.fetchColumnCommentsChunk(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for k, v := range chunkResult {
			result[k] = v
		}
	}
	return result, nil
}

func (c *OracleClient) fetchColumnCommentsChunk(ctx context.Context, columns []string) (map[string]string, error) {
	if len(columns) == 0 {
		return map[string]string{}, nil
	}
	args := make([]interface{}, 0, len(columns)+1)
	args = append(args, strings.ToUpper(c.schema))
	placeholders := make([]string, len(columns))
	for i, col := range columns {
		placeholders[i] = fmt.Sprintf(":%d", i+2)
		args = append(args, col)
	}
	query := fmt.Sprintf(`SELECT COLUMN_NAME, MAX(COMMENTS) AS COMMENTS
FROM ALL_COL_COMMENTS
WHERE OWNER = :1 AND COLUMN_NAME IN (%s)
GROUP BY COLUMN_NAME`, strings.Join(placeholders, ","))

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string, len(columns))
	for rows.Next() {
		var columnName string
		var comment sql.NullString
		if err := rows.Scan(&columnName, &comment); err != nil {
			return nil, err
		}
		if comment.Valid {
			result[strings.ToUpper(columnName)] = strings.TrimSpace(comment.String)
		} else {
			result[strings.ToUpper(columnName)] = ""
		}
	}
	return result, rows.Err()
}

// TestConnection tests the database connection
func (c *OracleClient) TestConnection(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.db.PingContext(ctx)
}

// GetDatabaseInfo returns basic database information
func (c *OracleClient) GetDatabaseInfo(ctx context.Context) (map[string]interface{}, error) {
	query := `
		SELECT
			SUBSTR(BANNER, 1, 30) AS DB_VERSION,
			USER AS CURRENT_USER
		FROM V$VERSION
		WHERE ROWNUM = 1
	`

	var dbVersion, currentUser string
	err := c.db.QueryRowContext(ctx, query).Scan(&dbVersion, &currentUser)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	info := map[string]interface{}{
		"database_version": dbVersion,
		"current_user":     currentUser,
	}

	return info, nil
}

// QueryBuilder wraps oracle for advanced features
func (c *OracleClient) GetDB() *sql.DB {
	return c.db
}

// GetOracleConnStr returns formatted Oracle connection string
func (c *OracleClient) GetOracleConnStr(cfg *config.Config) string {
	// For go-ora/v2 format: oracle://user:pass@host:port/sid
	return fmt.Sprintf("oracle://%s:%s@%s:%d/%s",
		cfg.OracleUser,
		cfg.OraclePassword,
		cfg.OracleHost,
		cfg.OraclePort,
		cfg.OracleSID,
	)
}
