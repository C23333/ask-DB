package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/sijms/go-ora/v2"

	"github.com/yourusername/db_asst/config"
	"github.com/yourusername/db_asst/internal/api"
	"github.com/yourusername/db_asst/internal/auth"
	"github.com/yourusername/db_asst/internal/chat"
	"github.com/yourusername/db_asst/internal/db"
	"github.com/yourusername/db_asst/internal/executor"
	"github.com/yourusername/db_asst/internal/llm"
	"github.com/yourusername/db_asst/internal/logger"
	"github.com/yourusername/db_asst/internal/memory"
	"github.com/yourusername/db_asst/internal/monitor"
	"github.com/yourusername/db_asst/internal/progress"
	"github.com/yourusername/db_asst/internal/reports"
	"github.com/yourusername/db_asst/internal/templates"
)

func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.InitLogger(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	log.Info("Starting DB Assistant API Server",
		zap.Int("port", cfg.ServerPort),
		zap.String("environment", cfg.Env),
	)

	// Initialize database connection
	dbClient, err := db.Initialize(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer dbClient.Close()

	log.Info("Database connected successfully")

	// Initialize services
	jwtManager := auth.NewJWTManager(cfg.JWTSecret)
	sqlExecutor := executor.New(dbClient, cfg, log)
	llmClient := llm.NewLLMClient(cfg, log)
	appDB, appDriver, err := initAppDatabase(cfg, log)
	if err != nil {
		log.Fatal("Failed to init app database", zap.Error(err))
	}
	defer appDB.Close()

	userService, err := auth.NewUserService(appDB, appDriver)
	if err != nil {
		log.Fatal("Failed to init user service", zap.Error(err))
	}
	memoryStore, err := memory.NewStore(appDB, appDriver)
	if err != nil {
		log.Fatal("Failed to init memory store", zap.Error(err))
	}
	reportStore, err := reports.NewStore(appDB, appDriver)
	if err != nil {
		log.Fatal("Failed to init report store", zap.Error(err))
	}
	chatStore, err := chat.NewStore(appDB, appDriver)
	if err != nil {
		log.Fatal("Failed to init chat store", zap.Error(err))
	}
	templateStore, err := templates.NewStore(appDB, appDriver)
	if err != nil {
		log.Fatal("Failed to init template store", zap.Error(err))
	}
	templateSvc := templates.NewService(templateStore)
	progressStore := progress.NewStore()
	monitorSvc, err := monitor.New(appDB, appDriver, cfg, log)
	if err != nil {
		log.Fatal("Failed to init monitor service", zap.Error(err))
	}

	// Create Gin router
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Setup API routes
	api.SetupRoutes(router, dbClient, jwtManager, userService, sqlExecutor, llmClient, templateSvc, memoryStore, reportStore, chatStore, monitorSvc, progressStore, cfg, log)

	// Start server in a goroutine
	go func() {
		addr := fmt.Sprintf(":%d", cfg.ServerPort)
		log.Info("Server listening", zap.String("address", addr))
		if err := router.Run(addr); err != nil {
			log.Fatal("Server error", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info("Server shutting down...")
}

func initAppDatabase(cfg *config.Config, log *zap.Logger) (*sql.DB, string, error) {
	driver, dsn, err := cfg.GetAppDBDSN()
	if err != nil {
		return nil, "", err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, "", err
	}
	if err := db.Ping(); err != nil {
		return nil, "", err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(10 * time.Minute)

	log.Info("App database connected",
		zap.String("driver", driver))

	return db, driver, nil
}
