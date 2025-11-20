package reports

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Report 表示一个用户保存的个人报表
type Report struct {
	ID          string            `json:"id"`
	UserID      string            `json:"user_id"`
	Title       string            `json:"title"`
	SQL         string            `json:"sql"`
	Description string            `json:"description,omitempty"`
	TemplateID  string            `json:"template_id,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Store 提供报表持久化能力
type Store struct {
	db     *sql.DB
	driver string
}

// NewStore 创建报表存储
func NewStore(db *sql.DB, driver string) (*Store, error) {
	if db == nil {
		return nil, errors.New("database handle is required for reports store")
	}
	store := &Store{db: db, driver: driver}
	if err := store.ensureTable(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) ensureTable() error {
	switch s.driver {
	case "mysql":
		const mysqlDDL = `
CREATE TABLE IF NOT EXISTS user_reports (
    id CHAR(36) NOT NULL PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    title VARCHAR(255) NOT NULL,
    sql_text LONGTEXT NOT NULL,
    description TEXT,
    template_id VARCHAR(128),
    session_id VARCHAR(128),
    parameters LONGTEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    KEY idx_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
		_, err := s.db.Exec(mysqlDDL)
		return err
	case "oracle":
		const check = `SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = 'USER_REPORTS'`
		var count int
		if err := s.db.QueryRow(check).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		const oracleDDL = `
CREATE TABLE USER_REPORTS (
    ID VARCHAR2(36) PRIMARY KEY,
    USER_ID VARCHAR2(64) NOT NULL,
    TITLE VARCHAR2(255) NOT NULL,
    SQL_TEXT CLOB NOT NULL,
    DESCRIPTION CLOB,
    TEMPLATE_ID VARCHAR2(128),
    SESSION_ID VARCHAR2(128),
    PARAMETERS CLOB,
    CREATED_AT TIMESTAMP NOT NULL,
    UPDATED_AT TIMESTAMP NOT NULL
)`
		if _, err := s.db.Exec(oracleDDL); err != nil {
			return err
		}
		_, err := s.db.Exec(`CREATE INDEX IDX_REPORT_USER ON USER_REPORTS (USER_ID)`)
		return err
	default:
		return fmt.Errorf("unsupported reports store driver: %s", s.driver)
	}
}

// Save 保存/更新报表
func (s *Store) Save(report *Report) error {
	if report.UserID == "" {
		return errors.New("userID is required")
	}
	if report.ID == "" {
		report.ID = uuid.New().String()
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now()
	}
	report.UpdatedAt = time.Now()

	paramsJSON, err := json.Marshal(report.Parameters)
	if err != nil {
		return err
	}

	if s.driver == "oracle" {
		// Oracle 没有 ON DUPLICATE KEY UPDATE，使用 MERGE
		_, err = s.db.Exec(`
MERGE INTO USER_REPORTS dst
USING (SELECT :1 AS ID FROM dual) src
ON (dst.ID = src.ID)
WHEN MATCHED THEN UPDATE SET
    USER_ID = :2,
    TITLE = :3,
    SQL_TEXT = :4,
    DESCRIPTION = :5,
    TEMPLATE_ID = :6,
    SESSION_ID = :7,
    PARAMETERS = :8,
    CREATED_AT = :9,
    UPDATED_AT = :10
WHEN NOT MATCHED THEN INSERT
    (ID, USER_ID, TITLE, SQL_TEXT, DESCRIPTION, TEMPLATE_ID, SESSION_ID, PARAMETERS, CREATED_AT, UPDATED_AT)
VALUES
    (:1, :2, :3, :4, :5, :6, :7, :8, :9, :10)
`, report.ID, report.UserID, report.Title, report.SQL, report.Description, report.TemplateID, report.SessionID, string(paramsJSON), report.CreatedAt, report.UpdatedAt)
		return err
	}

	_, err = s.db.Exec(`
INSERT INTO user_reports
    (id, user_id, title, sql_text, description, template_id, session_id, parameters, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    user_id=VALUES(user_id),
    title=VALUES(title),
    sql_text=VALUES(sql_text),
    description=VALUES(description),
    template_id=VALUES(template_id),
    session_id=VALUES(session_id),
    parameters=VALUES(parameters),
    created_at=VALUES(created_at),
    updated_at=VALUES(updated_at)
`, report.ID, report.UserID, report.Title, report.SQL, report.Description, report.TemplateID, report.SessionID, string(paramsJSON), report.CreatedAt, report.UpdatedAt)
	return err
}

// Delete 删除报表
func (s *Store) Delete(userID, reportID string) error {
	if s.driver == "oracle" {
		_, err := s.db.Exec(`DELETE FROM USER_REPORTS WHERE ID = :1 AND USER_ID = :2`, reportID, userID)
		return err
	}
	_, err := s.db.Exec(`DELETE FROM user_reports WHERE id = ? AND user_id = ?`, reportID, userID)
	return err
}

// ListByUser 返回用户的全部报表
func (s *Store) ListByUser(userID string) []*Report {
	query := `SELECT id, user_id, title, sql_text, description, template_id, session_id, parameters, created_at, updated_at
		FROM user_reports
		WHERE user_id = ?
		ORDER BY updated_at DESC`
	if s.driver == "oracle" {
		query = `SELECT id, user_id, title, sql_text, description, template_id, session_id, parameters, created_at, updated_at
			FROM USER_REPORTS
			WHERE user_id = :1
			ORDER BY updated_at DESC`
	}
	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var reports []*Report
	for rows.Next() {
		var r Report
		var params sql.NullString
		if err := rows.Scan(&r.ID, &r.UserID, &r.Title, &r.SQL, &r.Description, &r.TemplateID, &r.SessionID, &params, &r.CreatedAt, &r.UpdatedAt); err != nil {
			continue
		}
		if params.Valid && params.String != "" {
			_ = json.Unmarshal([]byte(params.String), &r.Parameters)
		}
		reports = append(reports, &r)
	}
	return reports
}

// GetByID 获取单个报表
func (s *Store) GetByID(userID, reportID string) (*Report, bool) {
	query := `SELECT id, user_id, title, sql_text, description, template_id, session_id, parameters, created_at, updated_at
		FROM user_reports WHERE user_id = ? AND id = ?`
	if s.driver == "oracle" {
		query = `SELECT id, user_id, title, sql_text, description, template_id, session_id, parameters, created_at, updated_at
			FROM USER_REPORTS WHERE user_id = :1 AND id = :2`
	}
	row := s.db.QueryRow(query, userID, reportID)
	var r Report
	var params sql.NullString
	if err := row.Scan(&r.ID, &r.UserID, &r.Title, &r.SQL, &r.Description, &r.TemplateID, &r.SessionID, &params, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, false
	}
	if params.Valid && params.String != "" {
		_ = json.Unmarshal([]byte(params.String), &r.Parameters)
	}
	return &r, true
}
