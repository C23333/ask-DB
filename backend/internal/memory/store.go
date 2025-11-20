package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Entry 表示一次 NL 到 SQL 的往返记录
type Entry struct {
	Query     string    `json:"query"`
	SQL       string    `json:"sql"`
	Reasoning string    `json:"reasoning"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

// Store 负责管理记忆数据（存储于 MySQL）
type Store struct {
	db     *sql.DB
	driver string
}

// NewStore 初始化记忆存储
func NewStore(db *sql.DB, driver string) (*Store, error) {
	if db == nil {
		return nil, errors.New("database handle is required for memory store")
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
CREATE TABLE IF NOT EXISTS conversation_memory (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    session_id VARCHAR(128) NOT NULL,
    query_text LONGTEXT NOT NULL,
    sql_text LONGTEXT NOT NULL,
    reasoning LONGTEXT,
    source VARCHAR(32),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_user_session_created (user_id, session_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
		_, err := s.db.Exec(mysqlDDL)
		return err
	case "oracle":
		const check = `SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = 'CONVERSATION_MEMORY'`
		var count int
		if err := s.db.QueryRow(check).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		const oracleDDL = `
CREATE TABLE CONVERSATION_MEMORY (
    ID VARCHAR2(36) PRIMARY KEY,
    USER_ID VARCHAR2(64) NOT NULL,
    SESSION_ID VARCHAR2(128) NOT NULL,
    QUERY_TEXT CLOB NOT NULL,
    SQL_TEXT CLOB NOT NULL,
    REASONING CLOB,
    SOURCE VARCHAR2(32),
    CREATED_AT TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`
		_, err := s.db.Exec(oracleDDL)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(`CREATE INDEX IDX_MEM_USER_SESSION ON CONVERSATION_MEMORY (USER_ID, SESSION_ID, CREATED_AT)`)
		return err
	default:
		return fmt.Errorf("unsupported memory store driver: %s", s.driver)
	}
}

// Append 向指定会话追加记忆
func (s *Store) Append(userID, sessionID string, entry Entry) error {
	if userID == "" {
		return errors.New("userID is required for memory")
	}
	if sessionID == "" {
		sessionID = "default"
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	id := uuid.New().String()
	if s.driver == "oracle" {
		_, err := s.db.Exec(`INSERT INTO CONVERSATION_MEMORY 
			(ID, USER_ID, SESSION_ID, QUERY_TEXT, SQL_TEXT, REASONING, SOURCE, CREATED_AT)
			VALUES (:1, :2, :3, :4, :5, :6, :7, :8)`,
			id, userID, sessionID, entry.Query, entry.SQL, entry.Reasoning, entry.Source, entry.CreatedAt)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO conversation_memory 
		(id, user_id, session_id, query_text, sql_text, reasoning, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, sessionID, entry.Query, entry.SQL, entry.Reasoning, entry.Source, entry.CreatedAt)
	return err
}

// GetRecent 返回指定会话最近的 N 条记忆
func (s *Store) GetRecent(userID, sessionID string, limit int) []Entry {
	if limit <= 0 {
		return nil
	}
	if sessionID == "" {
		sessionID = "default"
	}
	query := `SELECT query_text, sql_text, reasoning, source, created_at
		FROM conversation_memory
		WHERE user_id = ? AND session_id = ?
		ORDER BY created_at DESC
		LIMIT ?`
	var rows *sql.Rows
	var err error
	if s.driver == "oracle" {
		query = `SELECT query_text, sql_text, reasoning, source, created_at FROM (
			SELECT query_text, sql_text, reasoning, source, created_at
			FROM CONVERSATION_MEMORY
			WHERE user_id = :1 AND session_id = :2
			ORDER BY created_at DESC
		) WHERE ROWNUM <= :3`
		rows, err = s.db.Query(query, userID, sessionID, limit)
	} else {
		rows, err = s.db.Query(query, userID, sessionID, limit)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	var tmp []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Query, &e.SQL, &e.Reasoning, &e.Source, &e.CreatedAt); err != nil {
			continue
		}
		tmp = append(tmp, e)
	}
	// Reverse to keep chronological order
	for i, j := 0, len(tmp)-1; i < j; i, j = i+1, j-1 {
		tmp[i], tmp[j] = tmp[j], tmp[i]
	}
	return tmp
}

// GetSession 返回整个会话的记忆
func (s *Store) GetSession(userID, sessionID string) []Entry {
	if sessionID == "" {
		sessionID = "default"
	}
	query := `SELECT query_text, sql_text, reasoning, source, created_at
		FROM conversation_memory
		WHERE user_id = ? AND session_id = ?
		ORDER BY created_at ASC`
	var rows *sql.Rows
	var err error
	if s.driver == "oracle" {
		query = `SELECT query_text, sql_text, reasoning, source, created_at
			FROM CONVERSATION_MEMORY
			WHERE user_id = :1 AND session_id = :2
			ORDER BY created_at ASC`
		rows, err = s.db.Query(query, userID, sessionID)
	} else {
		rows, err = s.db.Query(query, userID, sessionID)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Query, &e.SQL, &e.Reasoning, &e.Source, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

// ListSessions 返回用户所有 session 的最近一次交互时间
func (s *Store) ListSessions(userID string) map[string]time.Time {
	query := `SELECT session_id, MAX(created_at) AS last_used
		FROM conversation_memory
		WHERE user_id = ?
		GROUP BY session_id`
	var rows *sql.Rows
	var err error
	if s.driver == "oracle" {
		query = `SELECT session_id, MAX(created_at) AS last_used
			FROM CONVERSATION_MEMORY
			WHERE user_id = :1
			GROUP BY session_id`
		rows, err = s.db.Query(query, userID)
	} else {
		rows, err = s.db.Query(query, userID)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]time.Time)
	for rows.Next() {
		var sessionID string
		var last time.Time
		if err := rows.Scan(&sessionID, &last); err != nil {
			continue
		}
		result[sessionID] = last
	}
	return result
}

// CleanupSessions 用于删除超时数据（可选）
func (s *Store) CleanupSessions(ctx context.Context, cutoff time.Time) error {
	if s.driver == "oracle" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM CONVERSATION_MEMORY WHERE CREATED_AT < :1`, cutoff)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM conversation_memory WHERE created_at < ?`, cutoff)
	return err
}
