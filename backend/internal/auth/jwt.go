package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/yourusername/db_asst/internal/models"
)

type JWTManager struct {
	secretKey string
}

type CustomClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(secretKey string) *JWTManager {
	return &JWTManager{secretKey: secretKey}
}

// GenerateToken generates a JWT token for a user
func (jm *JWTManager) GenerateToken(user *models.User) (string, int, error) {
	expiresIn := 24 * time.Hour // Token expires in 24 hours
	expiresAt := time.Now().Add(expiresIn)

	claims := CustomClaims{
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jm.secretKey))
	if err != nil {
		return "", 0, err
	}

	return tokenString, int(expiresIn.Seconds()), nil
}

// VerifyToken verifies and parses a JWT token
func (jm *JWTManager) VerifyToken(tokenString string) (*CustomClaims, error) {
	claims := &CustomClaims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jm.secretKey), nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

// PasswordManager handles password hashing and verification
type PasswordManager struct{}

// NewPasswordManager creates a new password manager
func NewPasswordManager() *PasswordManager {
	return &PasswordManager{}
}

// HashPassword hashes a password using bcrypt
func (pm *PasswordManager) HashPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

// VerifyPassword verifies a password against a hash
func (pm *PasswordManager) VerifyPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// UserService manages user-related operations
type UserService struct {
	db              *sql.DB
	driver          string
	passwordManager *PasswordManager
}

// NewUserService creates a new user service backed by the persistence database
func NewUserService(db *sql.DB, driver string) (*UserService, error) {
	if db == nil {
		return nil, errors.New("database handle is required for user service")
	}
	us := &UserService{
		db:              db,
		driver:          strings.ToLower(strings.TrimSpace(driver)),
		passwordManager: NewPasswordManager(),
	}
	if err := us.ensureTable(); err != nil {
		return nil, err
	}
	return us, nil
}

func (us *UserService) ensureTable() error {
	switch us.driver {
	case "mysql":
		const mysqlDDL = `
CREATE TABLE IF NOT EXISTS users (
    id CHAR(36) NOT NULL PRIMARY KEY,
    username VARCHAR(64) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'user',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE KEY idx_users_username (username),
    UNIQUE KEY idx_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
		if _, err := us.db.Exec(mysqlDDL); err != nil {
			return err
		}
		return us.ensureRoleColumn()
	case "oracle":
		const check = `SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = 'USERS'`
		var count int
		if err := us.db.QueryRow(check).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		const oracleDDL = `
CREATE TABLE USERS (
    ID VARCHAR2(36) PRIMARY KEY,
    USERNAME VARCHAR2(64) NOT NULL,
    EMAIL VARCHAR2(255) NOT NULL,
    PASSWORD VARCHAR2(255) NOT NULL,
    ROLE VARCHAR2(32) DEFAULT 'user' NOT NULL,
    CREATED_AT TIMESTAMP NOT NULL,
    UPDATED_AT TIMESTAMP NOT NULL
)`
		if _, err := us.db.Exec(oracleDDL); err != nil {
			return err
		}
		if _, err := us.db.Exec(`CREATE UNIQUE INDEX IDX_USERS_USERNAME ON USERS (USERNAME)`); err != nil {
			return err
		}
		_, err := us.db.Exec(`CREATE UNIQUE INDEX IDX_USERS_EMAIL ON USERS (EMAIL)`)
		if err != nil {
			return err
		}
		return us.ensureRoleColumn()
	default:
		return fmt.Errorf("unsupported user service driver: %s", us.driver)
	}
}

func (us *UserService) ensureRoleColumn() error {
	switch us.driver {
	case "mysql":
		const check = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'role'`
		var count int
		if err := us.db.QueryRow(check).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		if _, err := us.db.Exec(`ALTER TABLE users ADD COLUMN role VARCHAR(32) NOT NULL DEFAULT 'user'`); err != nil {
			return err
		}
		_, err := us.db.Exec(`UPDATE users SET role = 'user' WHERE role IS NULL OR role = ''`)
		return err
	case "oracle":
		const check = `SELECT COUNT(*) FROM USER_TAB_COLUMNS WHERE TABLE_NAME = 'USERS' AND COLUMN_NAME = 'ROLE'`
		var count int
		if err := us.db.QueryRow(check).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		if _, err := us.db.Exec(`ALTER TABLE USERS ADD (ROLE VARCHAR2(32) DEFAULT 'user' NOT NULL)`); err != nil {
			return err
		}
		_, err := us.db.Exec(`UPDATE USERS SET ROLE = 'user' WHERE ROLE IS NULL`)
		return err
	default:
		return nil
	}
}

// RegisterUser registers a new user
func (us *UserService) RegisterUser(username, email, password string) (*models.User, error) {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username == "" || email == "" || strings.TrimSpace(password) == "" {
		return nil, errors.New("username, email, and password are required")
	}

	exists, err := us.userExists(username, email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("user already exists")
	}

	hashedPassword, err := us.passwordManager.HashPassword(password)
	if err != nil {
		return nil, err
	}

	// Create new user
	user := &models.User{
		ID:        uuid.New().String(),
		Username:  username,
		Email:     email,
		Role:      "user",
		Password:  hashedPassword,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if us.driver == "oracle" {
		_, err = us.db.Exec(`INSERT INTO USERS
			(ID, USERNAME, EMAIL, PASSWORD, ROLE, CREATED_AT, UPDATED_AT)
			VALUES (:1, :2, :3, :4, :5, :6, :7)`,
			user.ID, user.Username, user.Email, hashedPassword, user.Role, user.CreatedAt, user.UpdatedAt)
	} else {
		_, err = us.db.Exec(`INSERT INTO users
			(id, username, email, password, role, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			user.ID, user.Username, user.Email, hashedPassword, user.Role, user.CreatedAt, user.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}

	user.Password = ""
	return user, nil
}

// GetUserByUsername retrieves a user by username
func (us *UserService) GetUserByUsername(username string) (*models.User, error) {
	query := `SELECT id, username, email, password, role, created_at, updated_at
		FROM users WHERE username = ? LIMIT 1`
	if us.driver == "oracle" {
		query = `SELECT id, username, email, password, role, created_at, updated_at
			FROM USERS WHERE username = :1 AND ROWNUM = 1`
	}

	row := us.db.QueryRow(query, strings.TrimSpace(username))
	user := &models.User{}
	if err := row.Scan(&user.ID, &user.Username, &user.Email, &user.Password, &user.Role, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return user, nil
}

// GetUserByID retrieves a user by ID
func (us *UserService) GetUserByID(userID string) (*models.User, error) {
	query := `SELECT id, username, email, password, role, created_at, updated_at
		FROM users WHERE id = ? LIMIT 1`
	if us.driver == "oracle" {
		query = `SELECT id, username, email, password, role, created_at, updated_at
			FROM USERS WHERE id = :1 AND ROWNUM = 1`
	}

	row := us.db.QueryRow(query, userID)
	user := &models.User{}
	if err := row.Scan(&user.ID, &user.Username, &user.Email, &user.Password, &user.Role, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return user, nil
}

// VerifyUserPassword verifies a user's password
func (us *UserService) VerifyUserPassword(username, password string) (*models.User, error) {
	user, err := us.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}

	if !us.passwordManager.VerifyPassword(user.Password, password) {
		return nil, fmt.Errorf("invalid password")
	}

	return user, nil
}

func (us *UserService) userExists(username, email string) (bool, error) {
	query := `SELECT 1 FROM users WHERE username = ? OR email = ? LIMIT 1`
	if us.driver == "oracle" {
		query = `SELECT 1 FROM USERS WHERE (username = :1 OR email = :2) AND ROWNUM = 1`
	}
	var exists int
	err := us.db.QueryRow(query, username, email).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
