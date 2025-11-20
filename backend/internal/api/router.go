package api

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/yourusername/db_asst/config"
	"github.com/yourusername/db_asst/internal/auth"
	"github.com/yourusername/db_asst/internal/chat"
	"github.com/yourusername/db_asst/internal/db"
	"github.com/yourusername/db_asst/internal/executor"
	"github.com/yourusername/db_asst/internal/llm"
	"github.com/yourusername/db_asst/internal/memory"
	"github.com/yourusername/db_asst/internal/monitor"
	"github.com/yourusername/db_asst/internal/progress"
	"github.com/yourusername/db_asst/internal/reports"
	"github.com/yourusername/db_asst/internal/templates"
)

func SetupRoutes(
	router *gin.Engine,
	dbClient *db.OracleClient,
	jwtManager *auth.JWTManager,
	userService *auth.UserService,
	sqlExecutor *executor.SQLExecutor,
	llmClient *llm.LLMClient,
	templateSvc *templates.Service,
	memoryStore *memory.Store,
	reportStore *reports.Store,
	chatStore *chat.Store,
	monitorSvc *monitor.Monitor,
	progressStore *progress.Store,
	cfg *config.Config,
	logger *zap.Logger,
) {
	// Create handler
	handler := NewAPIHandler(dbClient, jwtManager, userService, sqlExecutor, llmClient, templateSvc, memoryStore, reportStore, chatStore, monitorSvc, progressStore, cfg, logger)

	// Apply global middleware
	router.Use(CORSMiddleware())
	router.Use(ErrorHandlingMiddleware(logger))
	router.Use(LoggingMiddleware(logger))

	// Health check (no auth required)
	router.GET("/health", handler.Health)

	// Auth routes (no auth required)
	auth := router.Group("/api/auth")
	{
		auth.POST("/register", handler.Register)
		auth.POST("/login", handler.Login)
	}

	// Protected routes
	protected := router.Group("/api")
	protected.Use(AuthMiddleware(jwtManager, logger))
	{
		// SQL operations
		sql := protected.Group("/sql")
		{
			sql.POST("/generate", handler.GenerateSQL)
			sql.POST("/execute", handler.ExecuteSQL)
			sql.POST("/debug", handler.DebugSQL)
			sql.POST("/export", handler.ExportSQLResult)
			sql.POST("/save", handler.SaveSQL)
			sql.GET("/history", handler.GetHistory)
			sql.DELETE("/history/:id", handler.DeleteHistory)
			sql.GET("/sessions", handler.ListSessions)
			sql.GET("/sessions/:session_id/history", handler.GetSessionMemory)
			sql.GET("/generate/status/:request_id", handler.GetGenerationStatus)
		}

		// Database info
		db := protected.Group("/database")
		{
			db.GET("/info", handler.GetDatabaseInfo)
		}

		chatGroup := protected.Group("/chat")
		{
			chatGroup.GET("/sessions", handler.ListChatSessions)
			chatGroup.GET("/:session_id/messages", handler.GetChatMessages)
			chatGroup.GET("/:session_id/export", handler.ExportChatSession)
		}
	}

	templatesGroup := protected.Group("/templates")
	templatesGroup.Use(AuthMiddleware(jwtManager, logger))
	{
		templatesGroup.GET("", handler.ListTemplates)
		templatesGroup.POST("", handler.CreateTemplate)
		templatesGroup.PUT("/:id", handler.UpdateTemplate)
		templatesGroup.DELETE("/:id", handler.DeleteTemplate)
	}

	monitorGroup := protected.Group("/monitor")
	monitorGroup.Use(AdminOnlyMiddleware())
	{
		monitorGroup.GET("/stats", handler.GetMonitorStats)
	}

	ws := router.Group("/api/ws")
	{
		ws.GET("/sql", handler.SQLWebSocket)
	}
}
