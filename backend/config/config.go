package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// Server
	ServerPort int
	Env        string

	// Oracle Database
	OracleUser     string
	OraclePassword string
	OracleHost     string
	OraclePort     int
	OracleSID      string
	OracleSchema   string

	// App persistence database (memory, reports)
	AppDBDriver       string
	AppMySQLHost      string
	AppMySQLPort      int
	AppMySQLUser      string
	AppMySQLPassword  string
	AppMySQLDatabase  string
	AppOracleUser     string
	AppOraclePassword string
	AppOracleHost     string
	AppOraclePort     int
	AppOracleSID      string

	// JWT
	JWTSecret string

	// LLM Configuration
	LLMProvider string // "openai", "claude", "custom"
	LLMAPIKey   string
	LLMModel    string
	LLMBaseURL  string // For custom/proxy services
	LLMTimeout  int    // seconds

	// SQL generation
	SQLGenerateTimeout int // seconds
	SQLDefaultPageSize int
	SQLMaxPageSize     int
	SensitiveColumns   []string

	// Redis (Optional, for caching)
	RedisHost     string
	RedisPort     int
	RedisPassword string

	// Audit & Logging
	LogLevel string

	// Email alerts
	EmailSMTPHost           string
	EmailSMTPPort           int
	EmailSMTPUser           string
	EmailSMTPPassword       string
	EmailAlertTo            string
	EmailAlertEventTypes    []string
	EmailAlertMinDurationMs int
	EmailAlertCooldownSec   int
}

func LoadConfig() *Config {
	// Load .env file if exists
	_ = godotenv.Load(".env")

	oracleUser := getEnv("ORACLE_USER", "")
	oraclePass := getEnv("ORACLE_PASSWORD", "")
	oracleHost := getEnv("ORACLE_HOST", "")
	oraclePort := getEnvInt("ORACLE_PORT", 1521)
	oracleSID := getEnv("ORACLE_SID", "")

	return &Config{
		// Server
		ServerPort: getEnvInt("SERVER_PORT", 8080),
		Env:        getEnv("ENVIRONMENT", "development"),

		// Oracle Database - User needs to fill these
		OracleUser:     oracleUser,
		OraclePassword: oraclePass,
		OracleHost:     oracleHost,
		OraclePort:     oraclePort,
		OracleSID:      oracleSID,
		OracleSchema:   strings.TrimSpace(getEnv("ORACLE_SCHEMA", "")),

		// Persistence database config
		AppDBDriver:       strings.ToLower(strings.TrimSpace(getEnv("APP_DB_DRIVER", getEnv("PERSIST_DB_DRIVER", "mysql")))),
		AppMySQLHost:      getEnv("APP_MYSQL_HOST", getEnv("MYSQL_HOST", "127.0.0.1")),
		AppMySQLPort:      getEnvInt("APP_MYSQL_PORT", getEnvInt("MYSQL_PORT", 3306)),
		AppMySQLUser:      getEnv("APP_MYSQL_USER", getEnv("MYSQL_USER", "")),
		AppMySQLPassword:  getEnv("APP_MYSQL_PASSWORD", getEnv("MYSQL_PASSWORD", "")),
		AppMySQLDatabase:  getEnv("APP_MYSQL_DATABASE", getEnv("MYSQL_DATABASE", "")),
		AppOracleUser:     getEnv("APP_ORACLE_USER", oracleUser),
		AppOraclePassword: getEnv("APP_ORACLE_PASSWORD", oraclePass),
		AppOracleHost:     getEnv("APP_ORACLE_HOST", oracleHost),
		AppOraclePort:     getEnvInt("APP_ORACLE_PORT", oraclePort),
		AppOracleSID:      getEnv("APP_ORACLE_SID", oracleSID),

		// JWT
		JWTSecret: getEnv("JWT_SECRET", "your-secret-key-change-in-production"),

		// LLM Configuration
		LLMProvider:        strings.ToLower(strings.TrimSpace(getEnv("LLM_PROVIDER", "openai"))),
		LLMAPIKey:          getEnv("LLM_API_KEY", ""),
		LLMModel:           getEnv("LLM_MODEL", "gpt-3.5-turbo"),
		LLMBaseURL:         getEnv("LLM_BASE_URL", "https://api.openai.com/v1"), // Can override for proxy
		LLMTimeout:         getEnvInt("LLM_TIMEOUT", 120),
		SQLGenerateTimeout: getEnvInt("SQL_GENERATE_TIMEOUT", 120),
		SQLDefaultPageSize: getEnvInt("SQL_DEFAULT_PAGE_SIZE", 50),
		SQLMaxPageSize:     getEnvInt("SQL_MAX_PAGE_SIZE", 200),
		SensitiveColumns:   splitAndTrim(getEnv("SENSITIVE_COLUMNS", "")),

		// Redis
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnvInt("REDIS_PORT", 6379),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),

		// Logging
		LogLevel: getEnv("LOG_LEVEL", "info"),

		// Email
		EmailSMTPHost:           getEnv("EMAIL_SMTP_HOST", ""),
		EmailSMTPPort:           getEnvInt("EMAIL_SMTP_PORT", 587),
		EmailSMTPUser:           getEnv("EMAIL_SMTP_USER", ""),
		EmailSMTPPassword:       getEnv("EMAIL_SMTP_PASSWORD", ""),
		EmailAlertTo:            getEnv("EMAIL_ALERT_TO", ""),
		EmailAlertEventTypes:    getEnvListWithDefault("EMAIL_ALERT_EVENT_TYPES", []string{"generate_rest", "generate_ws", "execute_sql"}),
		EmailAlertMinDurationMs: getEnvInt("EMAIL_ALERT_MIN_DURATION_MS", 0),
		EmailAlertCooldownSec:   getEnvInt("EMAIL_ALERT_COOLDOWN_SEC", 300),
	}
}

func (c *Config) Validate() error {
	if c.OracleUser == "" {
		return fmt.Errorf("ORACLE_USER is required")
	}
	if c.OraclePassword == "" {
		return fmt.Errorf("ORACLE_PASSWORD is required")
	}
	if c.OracleHost == "" {
		return fmt.Errorf("ORACLE_HOST is required")
	}
	if c.OracleSID == "" {
		return fmt.Errorf("ORACLE_SID is required")
	}
	if c.LLMAPIKey == "" {
		return fmt.Errorf("LLM_API_KEY is required")
	}
	if _, _, err := c.GetAppDBDSN(); err != nil {
		return err
	}
	return nil
}

func (c *Config) GetOracleConnStr() string {
	// go-ora driver requires oracle:// scheme; for SID connections service segment stays empty and SID goes in query
	return fmt.Sprintf("oracle://%s:%s@%s:%d/?SID=%s",
		c.OracleUser,
		c.OraclePassword,
		c.OracleHost,
		c.OraclePort,
		c.OracleSID,
	)
}

func (c *Config) GetOracleSchema() string {
	if c.OracleSchema != "" {
		return strings.ToUpper(c.OracleSchema)
	}
	return strings.ToUpper(c.OracleUser)
}

func (c *Config) GetAppDBDSN() (string, string, error) {
	driver := strings.ToLower(strings.TrimSpace(c.AppDBDriver))
	if driver == "" {
		driver = "mysql"
	}

	switch driver {
	case "mysql":
		if c.AppMySQLUser == "" || c.AppMySQLPassword == "" || c.AppMySQLDatabase == "" {
			return "", "", fmt.Errorf("APP_MYSQL_* configuration is incomplete")
		}
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=Local&charset=utf8mb4",
			c.AppMySQLUser,
			c.AppMySQLPassword,
			c.AppMySQLHost,
			c.AppMySQLPort,
			c.AppMySQLDatabase,
		)
		return "mysql", dsn, nil
	case "oracle":
		if c.AppOracleUser == "" || c.AppOraclePassword == "" || c.AppOracleHost == "" || c.AppOracleSID == "" {
			return "", "", fmt.Errorf("APP_ORACLE_* configuration is incomplete")
		}
		return "oracle", fmt.Sprintf("oracle://%s:%s@%s:%d/?SID=%s",
			c.AppOracleUser,
			c.AppOraclePassword,
			c.AppOracleHost,
			c.AppOraclePort,
			c.AppOracleSID,
		), nil
	default:
		return "", "", fmt.Errorf("unsupported APP_DB_DRIVER: %s", driver)
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvListWithDefault(key string, defaultValue []string) []string {
	raw := getEnv(key, "")
	if strings.TrimSpace(raw) == "" {
		return append([]string{}, defaultValue...)
	}
	return splitAndTrim(raw)
}

func splitAndTrim(input string) []string {
	if strings.TrimSpace(input) == "" {
		return []string{}
	}
	parts := strings.Split(input, ",")
	var result []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
