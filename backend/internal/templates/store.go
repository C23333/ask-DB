package templates

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Record represents a template entry persisted in DB
type Record struct {
	ID          string
	Name        string
	Description string
	Keywords    []string
	SQL         string
	Parameters  map[string]string
	OwnerID     string
	IsSystem    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Store struct {
	db     *sql.DB
	driver string
}

func NewStore(db *sql.DB, driver string) (*Store, error) {
	s := &Store{db: db, driver: driver}
	if err := s.ensureTable(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureTable() error {
	switch s.driver {
	case "mysql":
		const ddl = `CREATE TABLE IF NOT EXISTS sql_templates (
            id CHAR(36) PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            description TEXT,
            keywords JSON,
            sql_text LONGTEXT NOT NULL,
            parameters JSON,
            owner_id VARCHAR(64),
            is_system TINYINT(1) DEFAULT 0,
            created_at DATETIME NOT NULL,
            updated_at DATETIME NOT NULL
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`
		_, err := s.db.Exec(ddl)
		return err
	case "oracle":
		const check = `SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = 'SQL_TEMPLATES'`
		var count int
		if err := s.db.QueryRow(check).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		const ddl = `CREATE TABLE SQL_TEMPLATES (
            ID VARCHAR2(36) PRIMARY KEY,
            NAME VARCHAR2(255) NOT NULL,
            DESCRIPTION CLOB,
            KEYWORDS CLOB,
            SQL_TEXT CLOB NOT NULL,
            PARAMETERS CLOB,
            OWNER_ID VARCHAR2(64),
            IS_SYSTEM NUMBER(1) DEFAULT 0,
            CREATED_AT TIMESTAMP NOT NULL,
            UPDATED_AT TIMESTAMP NOT NULL
        )`
		if _, err := s.db.Exec(ddl); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported driver %s", s.driver)
	}
}

func (s *Store) ListAll() ([]*Record, error) {
	query := "SELECT id, name, description, keywords, sql_text, parameters, owner_id, is_system, created_at, updated_at FROM sql_templates"
	if s.driver == "oracle" {
		query = "SELECT id, name, description, keywords, sql_text, parameters, owner_id, is_system, created_at, updated_at FROM SQL_TEMPLATES"
	}
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Record
	for rows.Next() {
		rec := &Record{}
		var keywordsRaw sql.NullString
		var paramsRaw sql.NullString
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Description, &keywordsRaw, &rec.SQL, &paramsRaw, &rec.OwnerID, &rec.IsSystem, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		if keywordsRaw.Valid {
			_ = json.Unmarshal([]byte(keywordsRaw.String), &rec.Keywords)
		}
		if paramsRaw.Valid {
			_ = json.Unmarshal([]byte(paramsRaw.String), &rec.Parameters)
		}
		result = append(result, rec)
	}
	return result, nil
}

func (s *Store) Get(id string) (*Record, error) {
	if id == "" {
		return nil, errors.New("id required")
	}
	query := "SELECT id, name, description, keywords, sql_text, parameters, owner_id, is_system, created_at, updated_at FROM sql_templates WHERE id = ?"
	if s.driver == "oracle" {
		query = "SELECT id, name, description, keywords, sql_text, parameters, owner_id, is_system, created_at, updated_at FROM SQL_TEMPLATES WHERE id = :1"
	}
	row := s.db.QueryRow(query, id)
	rec := &Record{}
	var keywordsRaw sql.NullString
	var paramsRaw sql.NullString
	if err := row.Scan(&rec.ID, &rec.Name, &rec.Description, &keywordsRaw, &rec.SQL, &paramsRaw, &rec.OwnerID, &rec.IsSystem, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		return nil, err
	}
	if keywordsRaw.Valid {
		_ = json.Unmarshal([]byte(keywordsRaw.String), &rec.Keywords)
	}
	if paramsRaw.Valid {
		_ = json.Unmarshal([]byte(paramsRaw.String), &rec.Parameters)
	}
	return rec, nil
}

func (s *Store) Save(rec *Record) error {
	if rec.ID == "" {
		rec.ID = uuid.New().String()
	}
	now := time.Now()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now

	keywordsJSON, _ := json.Marshal(rec.Keywords)
	paramsJSON, _ := json.Marshal(rec.Parameters)

	switch s.driver {
	case "mysql":
		_, err := s.db.Exec(`INSERT INTO sql_templates (id, name, description, keywords, sql_text, parameters, owner_id, is_system, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON DUPLICATE KEY UPDATE name=VALUES(name), description=VALUES(description), keywords=VALUES(keywords), sql_text=VALUES(sql_text), parameters=VALUES(parameters), owner_id=VALUES(owner_id), is_system=VALUES(is_system), updated_at=VALUES(updated_at)`,
			rec.ID, rec.Name, rec.Description, string(keywordsJSON), rec.SQL, string(paramsJSON), rec.OwnerID, rec.IsSystem, rec.CreatedAt, rec.UpdatedAt)
		return err
	case "oracle":
		_, err := s.db.Exec(`MERGE INTO SQL_TEMPLATES dst USING (SELECT :1 AS ID FROM dual) src
            ON (dst.ID = src.ID)
            WHEN MATCHED THEN UPDATE SET NAME=:2, DESCRIPTION=:3, KEYWORDS=:4, SQL_TEXT=:5, PARAMETERS=:6, OWNER_ID=:7, IS_SYSTEM=:8, UPDATED_AT=:9
            WHEN NOT MATCHED THEN INSERT (ID, NAME, DESCRIPTION, KEYWORDS, SQL_TEXT, PARAMETERS, OWNER_ID, IS_SYSTEM, CREATED_AT, UPDATED_AT)
            VALUES (:1, :2, :3, :4, :5, :6, :7, :8, :10, :9)`, rec.ID, rec.Name, rec.Description, string(keywordsJSON), rec.SQL, string(paramsJSON), rec.OwnerID, boolToInt(rec.IsSystem), rec.UpdatedAt, rec.CreatedAt)
		return err
	default:
		return errors.New("unsupported driver")
	}
}

func (s *Store) Delete(id string) error {
	if id == "" {
		return errors.New("id required")
	}
	query := "DELETE FROM sql_templates WHERE id = ?"
	if s.driver == "oracle" {
		query = "DELETE FROM SQL_TEMPLATES WHERE id = :1"
	}
	_, err := s.db.Exec(query, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func splitKeywords(input string) []string {
	parts := strings.Split(input, ",")
	var result []string
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
