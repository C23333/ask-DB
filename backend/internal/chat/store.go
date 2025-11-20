package chat

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Message represents one chat turn
type Message struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	db     *sql.DB
	driver string
}

func NewStore(db *sql.DB, driver string) (*Store, error) {
	if db == nil {
		return nil, errors.New("chat store requires db handle")
	}
	s := &Store{db: db, driver: driver}
	if err := s.ensureTable(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureTable() error {
	switch s.driver {
	case "mysql":
		const ddl = `CREATE TABLE IF NOT EXISTS chat_messages (
            id CHAR(36) PRIMARY KEY,
            user_id VARCHAR(64) NOT NULL,
            session_id VARCHAR(128) NOT NULL,
            role VARCHAR(16) NOT NULL,
            content LONGTEXT NOT NULL,
            created_at DATETIME NOT NULL,
            KEY idx_chat_user_session (user_id, session_id, created_at)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`
		_, err := s.db.Exec(ddl)
		return err
	case "oracle":
		const check = `SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = 'CHAT_MESSAGES'`
		var count int
		if err := s.db.QueryRow(check).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		const create = `CREATE TABLE CHAT_MESSAGES (
            ID VARCHAR2(36) PRIMARY KEY,
            USER_ID VARCHAR2(64) NOT NULL,
            SESSION_ID VARCHAR2(128) NOT NULL,
            ROLE VARCHAR2(16) NOT NULL,
            CONTENT CLOB NOT NULL,
            CREATED_AT TIMESTAMP NOT NULL
        )`
		if _, err := s.db.Exec(create); err != nil {
			return err
		}
		_, err := s.db.Exec(`CREATE INDEX IDX_CHAT_USER_SESSION ON CHAT_MESSAGES (USER_ID, SESSION_ID, CREATED_AT)`)
		return err
	default:
		return fmt.Errorf("unsupported driver %s", s.driver)
	}
}

func (s *Store) SaveMessage(userID, sessionID, role, content string) error {
	if userID == "" || sessionID == "" {
		return errors.New("userID and sessionID required")
	}
	id := uuid.New().String()
	now := time.Now()
	if s.driver == "oracle" {
		_, err := s.db.Exec(`INSERT INTO CHAT_MESSAGES (ID, USER_ID, SESSION_ID, ROLE, CONTENT, CREATED_AT) VALUES (:1, :2, :3, :4, :5, :6)`,
			id, userID, sessionID, role, content, now)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO chat_messages (id, user_id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, sessionID, role, content, now)
	return err
}

func (s *Store) GetMessages(userID, sessionID, keyword string, limit int) ([]*Message, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if s.driver == "oracle" {
		query := `SELECT id, user_id, session_id, role, content, created_at FROM (
            SELECT id, user_id, session_id, role, content, created_at
            FROM CHAT_MESSAGES
            WHERE user_id = :1 AND session_id = :2`
		args := []interface{}{userID, sessionID}
		if keyword != "" {
			query += " AND content LIKE :3"
			args = append(args, "%"+keyword+"%")
		}
		query += `
            ORDER BY created_at DESC
        ) WHERE ROWNUM <= :` + fmt.Sprintf("%d", len(args)+1)
		args = append(args, limit)
		rows, err = s.db.Query(query, args...)
	} else {
		query := `SELECT id, user_id, session_id, role, content, created_at
        FROM chat_messages
        WHERE user_id = ? AND session_id = ?`
		args := []interface{}{userID, sessionID}
		if keyword != "" {
			query += " AND content LIKE ?"
			args = append(args, "%"+keyword+"%")
		}
		query += " ORDER BY created_at DESC LIMIT ?"
		args = append(args, limit)
		rows, err = s.db.Query(query, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Message
	for rows.Next() {
		msg := &Message{}
		if err := rows.Scan(&msg.ID, &msg.UserID, &msg.SessionID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, msg)
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}

func (s *Store) ListSessions(userID string) (map[string]time.Time, error) {
	query := `SELECT session_id, MAX(created_at) FROM chat_messages WHERE user_id = ? GROUP BY session_id`
	if s.driver == "oracle" {
		query = `SELECT session_id, MAX(created_at) FROM CHAT_MESSAGES WHERE user_id = :1 GROUP BY session_id`
	}
	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]time.Time)
	for rows.Next() {
		var sessionID string
		var last time.Time
		if err := rows.Scan(&sessionID, &last); err != nil {
			return nil, err
		}
		result[sessionID] = last
	}
	return result, nil
}

func (s *Store) ExportSession(userID, sessionID string) ([]*Message, error) {
	return s.GetMessages(userID, sessionID, "", 1000)
}
