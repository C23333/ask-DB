package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/yourusername/db_asst/config"
	"github.com/yourusername/db_asst/internal/db"
	"github.com/yourusername/db_asst/internal/logger"
	"github.com/yourusername/db_asst/internal/mcp"
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

	log.Info("Starting MCP Server")

	// Initialize database connection
	dbClient, err := db.Initialize(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer dbClient.Close()

	log.Info("Database connected successfully")

	// Create MCP server
	mcpServer := mcp.NewMCPServer(dbClient, log)

	// Start MCP server on port 9000
	if err := mcpServer.Start(":9000"); err != nil {
		log.Fatal("Failed to start MCP server", zap.Error(err))
	}

	log.Info("MCP server started on :9000")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info("MCP server shutting down...")
	if err := mcpServer.Stop(); err != nil {
		log.Error("Error stopping MCP server", zap.Error(err))
	}
}
