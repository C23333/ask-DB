package models

import "time"

// User represents a system user
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Password  string    `json:"-"` // Never expose password
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LoginRequest is the request body for login
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse is the response after login
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
	User      User   `json:"user"`
}

// RegisterRequest is the request body for user registration
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// DatabaseSchema represents table information
type TableSchema struct {
	TableName string       `json:"table_name"`
	Comment   string       `json:"comment"`
	Columns   []ColumnInfo `json:"columns"`
	CreatedAt time.Time    `json:"created_at"`
}

// ColumnInfo represents a column in a table
type ColumnInfo struct {
	ColumnName   string `json:"column_name"`
	DataType     string `json:"data_type"`
	Nullable     bool   `json:"nullable"`
	ColumnSize   int    `json:"column_size"`
	Comment      string `json:"comment"`
	IsPrimaryKey bool   `json:"is_primary_key"`
	IsIndex      bool   `json:"is_index"`
}

// SQLGenerateRequest is the request to generate SQL from natural language
type SQLGenerateRequest struct {
	Query      string `json:"query" binding:"required"`
	Context    string `json:"context"`
	TableNames string `json:"table_names"`
	SessionID  string `json:"session_id"`
	RequestID  string `json:"request_id"`
}

// SQLGenerateResponse is the response after SQL generation
type SQLGenerateResponse struct {
	SQL        string `json:"sql"`
	Reasoning  string `json:"reasoning"`
	Source     string `json:"source"`
	TemplateID string `json:"template_id,omitempty"`
	UsedMemory bool   `json:"used_memory"`
	RequestID  string `json:"request_id"`
}

// SQLExecuteRequest is the request to execute SQL
type SQLExecuteRequest struct {
	SQL      string `json:"sql" binding:"required"`
	Timeout  int    `json:"timeout"` // seconds
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// SQLExportRequest describes a SQL export job
type SQLExportRequest struct {
	SQL      string `json:"sql" binding:"required"`
	Format   string `json:"format"`
	Filename string `json:"filename"`
	Limit    int    `json:"limit"`
}

// SQLExecuteResponse is the response after SQL execution
type SQLExecuteResponse struct {
	Success       bool            `json:"success"`
	Columns       []string        `json:"columns"`
	Rows          [][]interface{} `json:"rows"`
	RowCount      int             `json:"row_count"`
	ExecTime      int64           `json:"exec_time_ms"`
	Error         string          `json:"error,omitempty"`
	Page          int             `json:"page"`
	PageSize      int             `json:"page_size"`
	HasMore       bool            `json:"has_more"`
	MaskedColumns []string        `json:"masked_columns,omitempty"`
}

// SQLHistoryRecord represents a saved SQL query
type SQLHistoryRecord struct {
	ID          string            `json:"id"`
	UserID      string            `json:"user_id"`
	SQL         string            `json:"sql"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Saved       bool              `json:"saved"`
	LastRun     time.Time         `json:"last_run"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	TemplateID  string            `json:"template_id,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	Source      string            `json:"source,omitempty"`
}

// SaveSQLRequest 用于保存个人报表
type SaveSQLRequest struct {
	SQL         string            `json:"sql" binding:"required"`
	Title       string            `json:"title" binding:"required"`
	Description string            `json:"description"`
	TemplateID  string            `json:"template_id"`
	SessionID   string            `json:"session_id"`
	Parameters  map[string]string `json:"parameters"`
}

// TemplateUpsertRequest 用于创建或更新模版
type TemplateUpsertRequest struct {
	Name        string            `json:"name" binding:"required"`
	Description string            `json:"description"`
	Keywords    []string          `json:"keywords"`
	SQL         string            `json:"sql" binding:"required"`
	Parameters  map[string]string `json:"parameters"`
}

// SQLDebugRequest is the request to debug a failed SQL query
type SQLDebugRequest struct {
	SQL   string `json:"sql" binding:"required"`
	Error string `json:"error" binding:"required"`
}

// SQLDebugResponse is the response with debugging suggestions
type SQLDebugResponse struct {
	AnalysisText string `json:"analysis_text"`
	SuggestedSQL string `json:"suggested_sql"`
	Explanation  string `json:"explanation"`
}

// MCPRequest is the base request for MCP service
type MCPRequest struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

// SchemaIndexRequest is the request to index all schema
type SchemaIndexRequest struct {
	IncludeSystemTables bool `json:"include_system_tables"`
}

// SchemaIndexResponse is the response after indexing
type SchemaIndexResponse struct {
	Tables    int       `json:"tables_count"`
	Columns   int       `json:"columns_count"`
	IndexedAt time.Time `json:"indexed_at"`
}

// AuditLog represents an audit trail entry
type AuditLog struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"` // "EXECUTE_SQL", "GENERATE_SQL", etc.
	SQL       string    `json:"sql"`
	Status    string    `json:"status"` // "SUCCESS", "FAILED"
	ErrorMsg  string    `json:"error_msg,omitempty"`
	ExecTime  int64     `json:"exec_time_ms"`
	CreatedAt time.Time `json:"created_at"`
}

// ErrorResponse is a standard error response
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// SuccessResponse is a standard success response
type SuccessResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
