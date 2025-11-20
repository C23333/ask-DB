package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/yourusername/db_asst/config"
	"github.com/yourusername/db_asst/internal/auth"
	"github.com/yourusername/db_asst/internal/chat"
	"github.com/yourusername/db_asst/internal/db"
	"github.com/yourusername/db_asst/internal/executor"
	"github.com/yourusername/db_asst/internal/llm"
	"github.com/yourusername/db_asst/internal/memory"
	"github.com/yourusername/db_asst/internal/models"
	"github.com/yourusername/db_asst/internal/monitor"
	"github.com/yourusername/db_asst/internal/progress"
	"github.com/yourusername/db_asst/internal/reports"
	"github.com/yourusername/db_asst/internal/templates"
)

type APIHandler struct {
	dbClient        *db.OracleClient
	jwtManager      *auth.JWTManager
	userService     *auth.UserService
	sqlExecutor     *executor.SQLExecutor
	llmClient       *llm.LLMClient
	logger          *zap.Logger
	templateSvc     *templates.Service
	memoryStore     *memory.Store
	reportStore     *reports.Store
	chatStore       *chat.Store
	monitor         *monitor.Monitor
	progressStore   *progress.Store
	generateTimeout time.Duration
	cfg             *config.Config
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type wsMessage struct {
	Type       string `json:"type"`
	Stage      string `json:"stage,omitempty"`
	Message    string `json:"message,omitempty"`
	Chunk      string `json:"chunk,omitempty"`
	Done       bool   `json:"done,omitempty"`
	SQL        string `json:"sql,omitempty"`
	Reasoning  string `json:"reasoning,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
	UsedMemory bool   `json:"used_memory,omitempty"`
	Error      string `json:"error,omitempty"`
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(
	dbClient *db.OracleClient,
	jwtManager *auth.JWTManager,
	userService *auth.UserService,
	sqlExecutor *executor.SQLExecutor,
	llmClient *llm.LLMClient,
	templateSvc *templates.Service,
	memoryStore *memory.Store,
	reportStore *reports.Store,
	chatStore *chat.Store,
	monitor *monitor.Monitor,
	progressStore *progress.Store,
	cfg *config.Config,
	logger *zap.Logger,
) *APIHandler {
	timeout := time.Duration(cfg.SQLGenerateTimeout)
	if timeout <= 0 {
		timeout = 120
	}
	return &APIHandler{
		dbClient:        dbClient,
		jwtManager:      jwtManager,
		userService:     userService,
		sqlExecutor:     sqlExecutor,
		llmClient:       llmClient,
		templateSvc:     templateSvc,
		memoryStore:     memoryStore,
		reportStore:     reportStore,
		chatStore:       chatStore,
		monitor:         monitor,
		progressStore:   progressStore,
		generateTimeout: timeout * time.Second,
		cfg:             cfg,
		logger:          logger,
	}
}

// Health checks if the server is running
func (h *APIHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Server is healthy",
	})
}

// Register registers a new user
func (h *APIHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid request",
			Details: err.Error(),
		})
		return
	}

	user, err := h.userService.RegisterUser(req.Username, req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Registration failed",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, models.SuccessResponse{
		Code:    http.StatusCreated,
		Message: "User registered successfully",
		Data:    user,
	})
}

// Login logs in a user and returns a JWT token
func (h *APIHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid request",
			Details: err.Error(),
		})
		return
	}

	user, err := h.userService.VerifyUserPassword(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Code:    http.StatusUnauthorized,
			Message: "Invalid credentials",
		})
		return
	}

	token, expiresIn, err := h.jwtManager.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Token generation failed",
		})
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Login successful",
		Data: models.LoginResponse{
			Token:     token,
			ExpiresIn: expiresIn,
			User:      *user,
		},
	})
}

// GenerateSQL generates SQL from natural language
func (h *APIHandler) GenerateSQL(c *gin.Context) {
	var req models.SQLGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid request",
			Details: err.Error(),
		})
		return
	}

	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Code:    http.StatusUnauthorized,
			Message: "Unauthorized",
		})
		return
	}
	userID := userIDVal.(string)
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = userID
	}
	h.saveChatMessage(userID, sessionID, "user", req.Query)
	start := time.Now()
	success := false
	metricExtra := map[string]interface{}{"session_id": sessionID}
	defer func() {
		h.recordMetric("generate_rest", start, success, metricExtra)
	}()

	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = uuid.New().String()
	}
	h.updateProgress(requestID, "received", "已收到生成请求")

	// Step1: 尝试命中模版
	if h.templateSvc != nil {
		if tpl := h.templateSvc.Match(req.Query); tpl != nil {
			h.updateProgress(requestID, "template_matched", fmt.Sprintf("命中模版：%s", tpl.Name))
			resp := &models.SQLGenerateResponse{
				SQL:        tpl.SQL,
				Reasoning:  fmt.Sprintf("命中内置报表模版：%s", tpl.Name),
				Source:     "template",
				TemplateID: tpl.ID,
				UsedMemory: false,
				RequestID:  requestID,
			}
			h.appendMemory(userID, sessionID, req.Query, resp)
			metricExtra["template_id"] = tpl.ID
			success = true
			h.completeProgress(requestID, "生成完成（模版）")
			h.saveChatMessage(userID, sessionID, "assistant", formatSQLChatMessage(resp))
			c.JSON(http.StatusOK, models.SuccessResponse{
				Code:    http.StatusOK,
				Message: "SQL generated from template",
				Data:    resp,
			})
			return
		}
	}

	h.updateProgress(requestID, "prepare_context", "正在加载数据库元数据")
	ctx, cancel := context.WithTimeout(context.Background(), h.generateTimeout)
	defer cancel()

	// Get database schema context
	schemaContext, err := h.getDatabaseSchemaContext(ctx, req.TableNames)
	if err != nil {
		h.logger.Warn("Failed to get schema context", zap.Error(err))
		schemaContext = "Error retrieving schema"
	}

	var memEntries []memory.Entry
	if h.memoryStore != nil {
		memEntries = h.memoryStore.GetRecent(userID, sessionID, 5)
	}
	memoryContext := buildMemoryContext(memEntries)
	if len(memEntries) > 0 {
		h.updateProgress(requestID, "memory_loaded", fmt.Sprintf("命中 %d 条历史记忆", len(memEntries)))
	}

	// Call LLM to generate SQL
	h.updateProgress(requestID, "llm_call", "LLM 正在生成 SQL")
	resp, err := h.llmClient.GenerateSQL(ctx, &req, schemaContext, memoryContext)
	if err != nil {
		if guidance := h.generateGuidanceResponse(req, schemaContext, err.Error(), requestID); guidance != nil {
			h.saveChatMessage(userID, sessionID, "assistant", guidance.Reasoning)
			c.JSON(http.StatusOK, models.SuccessResponse{
				Code:    http.StatusOK,
				Message: "SQL generation guidance",
				Data:    guidance,
			})
			return
		}
		h.saveChatMessage(userID, sessionID, "assistant", fmt.Sprintf("生成失败：%s", err.Error()))
		h.failProgress(requestID, err.Error())
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to generate SQL",
			Details: err.Error(),
		})
		return
	}
	resp.Source = "llm"
	resp.UsedMemory = len(memEntries) > 0
	resp.RequestID = requestID

	if h.needsGuidance(resp.SQL) {
		if guidance := h.generateGuidanceResponse(req, schemaContext, resp.SQL, requestID); guidance != nil {
			h.saveChatMessage(userID, sessionID, "assistant", guidance.Reasoning)
			h.completeProgress(requestID, "生成完成（提示）")
			c.JSON(http.StatusOK, models.SuccessResponse{
				Code:    http.StatusOK,
				Message: "SQL guidance",
				Data:    guidance,
			})
			return
		}
	}

	h.appendMemory(userID, sessionID, req.Query, resp)
	success = true
	h.completeProgress(requestID, "生成完成")
	h.saveChatMessage(userID, sessionID, "assistant", formatSQLChatMessage(resp))

	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "SQL generated successfully",
		Data:    resp,
	})
}

// ExecuteSQL executes a SQL query
func (h *APIHandler) ExecuteSQL(c *gin.Context) {
	var req models.SQLExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid request",
			Details: err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Execute SQL
	start := time.Now()
	success := false
	metricExtra := map[string]interface{}{
		"user_id":   c.GetString("user_id"),
		"page":      req.Page,
		"page_size": req.PageSize,
	}
	defer func() {
		h.recordMetric("execute_sql", start, success, metricExtra)
	}()

	result, err := h.sqlExecutor.ExecuteSQL(ctx, req)
	if err != nil {
		metricExtra["error"] = err.Error()
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to execute SQL",
			Details: err.Error(),
		})
		return
	}

	// Log to audit trail
	userID := c.GetString("user_id")
	h.logAuditTrail(userID, "EXECUTE_SQL", req.SQL, result.Success, result.Error)
	if result.Success {
		success = true
	} else {
		metricExtra["error"] = result.Error
	}

	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "SQL executed successfully",
		Data:    result,
	})
}

// ExportSQLResult runs SQL and streams the result as an Excel/Word file
func (h *APIHandler) ExportSQLResult(c *gin.Context) {
	var req models.SQLExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid request",
			Details: err.Error(),
		})
		return
	}

	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "excel"
	}
	if format != "excel" && format != "word" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Unsupported format",
		})
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}

	if err := h.sqlExecutor.ValidateSQL(req.SQL); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid SQL",
			Details: err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()

	result, err := h.dbClient.ExecuteQueryRange(ctx, req.SQL, 0, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "SQL execution failed",
			Details: err.Error(),
		})
		return
	}
	if result == nil || !result.Success {
		message := "SQL execution failed"
		if result != nil && result.Error != "" {
			message = result.Error
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: message,
		})
		return
	}

	rows := stringifySQLRows(result.Rows)
	notes := []string{
		fmt.Sprintf("导出时间：%s", time.Now().Format("2006-01-02 15:04:05")),
		fmt.Sprintf("总行数：%d%s", result.RowCount, func() string {
			if result.HasMore {
				return fmt.Sprintf("（已限制在前 %d 行）", limit)
			}
			return ""
		}()),
		"SQL：" + truncateText(strings.TrimSpace(req.SQL), 400),
	}

	doc := buildTableDocument("SQL 查询结果导出", notes, result.Columns, rows)
	filename := req.Filename
	if strings.TrimSpace(filename) == "" {
		filename = fmt.Sprintf("sql_result_%s", time.Now().Format("20060102_150405"))
	}
	ext := ".xls"
	contentType := "application/vnd.ms-excel"
	if format == "word" {
		ext = ".doc"
		contentType = "application/msword"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ext) {
		filename += ext
	}
	sendDocumentAttachment(c, filename, contentType, doc)
}

// DebugSQL provides debugging suggestions for a failed SQL query
func (h *APIHandler) DebugSQL(c *gin.Context) {
	var req models.SQLDebugRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid request",
			Details: err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get schema context
	schemaContext, err := h.getDatabaseSchemaContext(ctx, "")
	if err != nil {
		schemaContext = "Error retrieving schema"
	}

	// Call LLM to debug
	resp, err := h.llmClient.DebugSQL(ctx, &req, schemaContext)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to debug SQL",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Debug suggestions provided",
		Data:    resp,
	})
}

// SaveSQL saves a SQL query to history
func (h *APIHandler) SaveSQL(c *gin.Context) {
	start := time.Now()
	success := false
	extra := map[string]interface{}{}
	defer func() {
		h.recordMetric("report_save", start, success, extra)
	}()

	var req models.SaveSQLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		extra["error"] = err.Error()
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid request",
			Details: err.Error(),
		})
		return
	}

	userID := c.GetString("user_id")
	extra["title"] = req.Title
	extra["session_id"] = req.SessionID

	record := &models.SQLHistoryRecord{
		ID:          uuid.New().String(),
		UserID:      userID,
		SQL:         req.SQL,
		Title:       req.Title,
		Description: req.Description,
		Saved:       true,
		SessionID:   req.SessionID,
		TemplateID:  req.TemplateID,
		Parameters:  req.Parameters,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if h.reportStore != nil {
		report := &reports.Report{
			ID:          record.ID,
			UserID:      record.UserID,
			Title:       record.Title,
			SQL:         record.SQL,
			Description: record.Description,
			TemplateID:  record.TemplateID,
			Parameters:  record.Parameters,
			SessionID:   record.SessionID,
			CreatedAt:   record.CreatedAt,
			UpdatedAt:   record.UpdatedAt,
		}
		if err := h.reportStore.Save(report); err != nil {
			h.logger.Error("Failed to persist report", zap.Error(err))
			extra["error"] = err.Error()
		}
	}
	extra["report_id"] = record.ID
	if _, hasErr := extra["error"]; !hasErr {
		success = true
	}

	c.JSON(http.StatusCreated, models.SuccessResponse{
		Code:    http.StatusCreated,
		Message: "SQL saved successfully",
		Data:    record,
	})
}

// GetHistory retrieves SQL history for the current user or a scoped user
func (h *APIHandler) GetHistory(c *gin.Context) {
	targetUser := scopedUserID(c)
	start := time.Now()
	extra := map[string]interface{}{
		"user_id":      c.GetString("user_id"),
		"target_user":  targetUser,
		"is_admin_req": isAdmin(c),
	}
	defer h.recordMetric("report_list", start, true, extra)

	var histories []*models.SQLHistoryRecord
	if h.reportStore != nil {
		for _, report := range h.reportStore.ListByUser(targetUser) {
			histories = append(histories, convertReportToHistory(report))
		}
	}

	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "History retrieved successfully",
		Data:    histories,
	})
}

// DeleteHistory 删除个人报表
func (h *APIHandler) DeleteHistory(c *gin.Context) {
	if h.reportStore == nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Report store is not configured",
		})
		return
	}
	reportID := c.Param("id")
	if reportID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Report ID is required",
		})
		return
	}
	start := time.Now()
	success := false
	extra := map[string]interface{}{"report_id": reportID}
	defer func() {
		h.recordMetric("report_delete", start, success, extra)
	}()
	targetUser := scopedUserID(c)
	extra["user_id"] = c.GetString("user_id")
	extra["target_user"] = targetUser
	extra["is_admin_req"] = isAdmin(c)
	if err := h.reportStore.Delete(targetUser, reportID); err != nil {
		extra["error"] = err.Error()
		h.logger.Error("Failed to delete report", zap.Error(err))
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to delete report",
			Details: err.Error(),
		})
		return
	}
	success = true
	c.Status(http.StatusNoContent)
}

// ListTemplates 返回所有内置模版
func (h *APIHandler) ListTemplates(c *gin.Context) {
	if h.templateSvc == nil {
		c.JSON(http.StatusOK, models.SuccessResponse{
			Code:    http.StatusOK,
			Message: "Templates disabled",
			Data:    []templates.Template{},
		})
		return
	}
	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)
	start := time.Now()
	success := false
	extra := map[string]interface{}{"user_id": userID}
	defer func() {
		h.recordMetric("template_list", start, success, extra)
	}()
	list, err := h.templateSvc.ListForUser(userID)
	if err != nil {
		extra["error"] = err.Error()
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to list templates",
			Details: err.Error(),
		})
		return
	}
	success = true
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Templates retrieved",
		Data:    list,
	})
}

func (h *APIHandler) CreateTemplate(c *gin.Context) {
	if h.templateSvc == nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Templates disabled",
		})
		return
	}
	start := time.Now()
	success := false
	extra := map[string]interface{}{}
	defer func() {
		h.recordMetric("template_create", start, success, extra)
	}()
	var req models.TemplateUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		extra["error"] = err.Error()
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid template payload",
			Details: err.Error(),
		})
		return
	}
	userID, _ := c.Get("user_id")
	extra["name"] = req.Name
	tpl := &templates.Template{
		Name:        req.Name,
		Description: req.Description,
		Keywords:    normalizeKeywords(req.Keywords),
		SQL:         req.SQL,
		Parameters:  req.Parameters,
		OwnerID:     userID.(string),
	}
	if err := h.templateSvc.SaveTemplate(tpl); err != nil {
		extra["error"] = err.Error()
		h.logger.Error("Failed to save template", zap.Error(err))
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to save template",
			Details: err.Error(),
		})
		return
	}
	list, _ := h.templateSvc.ListForUser(userID.(string))
	extra["template_id"] = tpl.ID
	if _, ok := extra["error"]; !ok {
		success = true
	}
	c.JSON(http.StatusCreated, models.SuccessResponse{
		Code:    http.StatusCreated,
		Message: "Template created",
		Data:    list,
	})
}

func (h *APIHandler) UpdateTemplate(c *gin.Context) {
	if h.templateSvc == nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Templates disabled",
		})
		return
	}
	id := c.Param("id")
	userID, _ := c.Get("user_id")
	start := time.Now()
	success := false
	extra := map[string]interface{}{"template_id": id}
	defer func() {
		h.recordMetric("template_update", start, success, extra)
	}()
	existing, err := h.templateSvc.GetTemplate(id, userID.(string))
	if err != nil {
		extra["error"] = "not_found"
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Code:    http.StatusNotFound,
			Message: "Template not found",
		})
		return
	}
	if !existing.Editable {
		extra["error"] = "forbidden"
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Code:    http.StatusForbidden,
			Message: "Template is not editable",
		})
		return
	}
	var req models.TemplateUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		extra["error"] = err.Error()
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid template payload",
			Details: err.Error(),
		})
		return
	}
	existing.Name = req.Name
	existing.Description = req.Description
	existing.Keywords = normalizeKeywords(req.Keywords)
	existing.SQL = req.SQL
	existing.Parameters = req.Parameters
	if err := h.templateSvc.SaveTemplate(existing); err != nil {
		extra["error"] = err.Error()
		h.logger.Error("Failed to update template", zap.Error(err))
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to update template",
		})
		return
	}
	list, _ := h.templateSvc.ListForUser(userID.(string))
	if _, ok := extra["error"]; !ok {
		success = true
	}
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Template updated",
		Data:    list,
	})
}

func (h *APIHandler) DeleteTemplate(c *gin.Context) {
	if h.templateSvc == nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Templates disabled",
		})
		return
	}
	id := c.Param("id")
	userID, _ := c.Get("user_id")
	start := time.Now()
	success := false
	extra := map[string]interface{}{"template_id": id}
	defer func() {
		h.recordMetric("template_delete", start, success, extra)
	}()
	existing, err := h.templateSvc.GetTemplate(id, userID.(string))
	if err != nil {
		extra["error"] = "not_found"
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Code:    http.StatusNotFound,
			Message: "Template not found",
		})
		return
	}
	if !existing.Editable {
		extra["error"] = "forbidden"
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Code:    http.StatusForbidden,
			Message: "Template is not editable",
		})
		return
	}
	if err := h.templateSvc.DeleteTemplate(id); err != nil {
		extra["error"] = err.Error()
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to delete template",
			Details: err.Error(),
		})
		return
	}
	list, _ := h.templateSvc.ListForUser(userID.(string))
	success = true
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Template deleted",
		Data:    list,
	})
}

func (h *APIHandler) GetMonitorStats(c *gin.Context) {
	if h.monitor == nil {
		empty := monitor.Dashboard{
			Summary: monitor.Summary{WindowHours: 24},
			Stats:   []monitor.Stats{},
			Trend:   []monitor.TrendPoint{},
			Recent:  []monitor.Event{},
		}
		c.JSON(http.StatusOK, models.SuccessResponse{
			Code:    http.StatusOK,
			Message: "Monitor disabled",
			Data:    empty,
		})
		return
	}
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	if fromStr := c.Query("from"); fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = parsed
		}
	}
	if toStr := c.Query("to"); toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = parsed
		}
	}
	bucket := 15
	if bucketStr := c.Query("bucket_minutes"); bucketStr != "" {
		if parsed, err := strconv.Atoi(bucketStr); err == nil && parsed > 0 && parsed <= 360 {
			bucket = parsed
		}
	}
	dashboard, err := h.monitor.QueryDashboard(from, to, bucket)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to load stats",
			Details: err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Monitor stats retrieved",
		Data:    dashboard,
	})
}

// GetGenerationStatus 返回生成请求的进度
func (h *APIHandler) GetGenerationStatus(c *gin.Context) {
	if h.progressStore == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Code:    http.StatusNotFound,
			Message: "Progress store not configured",
		})
		return
	}
	requestID := c.Param("request_id")
	if requestID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "request_id is required",
		})
		return
	}
	entry, ok := h.progressStore.Get(requestID)
	if !ok {
		entry = &progress.Entry{
			ID:        requestID,
			Stage:     "pending",
			Message:   "任务排队中",
			Done:      false,
			Success:   false,
			UpdatedAt: time.Now(),
		}
	}
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Progress retrieved",
		Data:    entry,
	})
}

func (h *APIHandler) ListChatSessions(c *gin.Context) {
	if h.chatStore == nil {
		c.JSON(http.StatusOK, models.SuccessResponse{
			Code:    http.StatusOK,
			Message: "Chat store disabled",
			Data:    map[string]time.Time{},
		})
		return
	}
	requestUser := c.GetString("user_id")
	targetUser := scopedUserID(c)
	start := time.Now()
	success := false
	extra := map[string]interface{}{
		"user_id":      requestUser,
		"target_user":  targetUser,
		"is_admin_req": isAdmin(c),
	}
	defer func() {
		h.recordMetric("chat_sessions", start, success, extra)
	}()
	sessions, err := h.chatStore.ListSessions(targetUser)
	if err != nil {
		extra["error"] = err.Error()
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to list sessions",
			Details: err.Error(),
		})
		return
	}
	success = true
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Chat sessions retrieved",
		Data:    sessions,
	})
}

func (h *APIHandler) GetChatMessages(c *gin.Context) {
	if h.chatStore == nil {
		c.JSON(http.StatusOK, models.SuccessResponse{
			Code:    http.StatusOK,
			Message: "Chat store disabled",
			Data:    []chat.Message{},
		})
		return
	}
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "session_id required",
		})
		return
	}
	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil {
			limit = parsed
		}
	}
	keyword := c.Query("keyword")
	start := time.Now()
	success := false
	extra := map[string]interface{}{
		"session_id": sessionID,
		"keyword":    keyword,
		"limit":      limit,
	}
	targetUser := scopedUserID(c)
	extra["user_id"] = c.GetString("user_id")
	extra["target_user"] = targetUser
	extra["is_admin_req"] = isAdmin(c)
	defer func() {
		h.recordMetric("chat_messages", start, success, extra)
	}()
	messages, err := h.chatStore.GetMessages(targetUser, sessionID, keyword, limit)
	if err != nil {
		extra["error"] = err.Error()
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to load messages",
			Details: err.Error(),
		})
		return
	}
	success = true
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Chat history retrieved",
		Data:    messages,
	})
}

func (h *APIHandler) ExportChatSession(c *gin.Context) {
	if h.chatStore == nil {
		c.Status(http.StatusNotFound)
		return
	}
	requestUser := c.GetString("user_id")
	sessionID := c.Param("session_id")
	format := strings.ToLower(c.DefaultQuery("format", "text"))
	start := time.Now()
	success := false
	extra := map[string]interface{}{
		"session_id": sessionID,
		"user_id":    requestUser,
	}
	defer func() {
		h.recordMetric("chat_export", start, success, extra)
	}()
	targetUser := scopedUserID(c)
	extra["target_user"] = targetUser
	extra["is_admin_req"] = isAdmin(c)
	messages, err := h.chatStore.ExportSession(targetUser, sessionID)
	if err != nil {
		extra["error"] = err.Error()
		c.Status(http.StatusInternalServerError)
		return
	}
	switch format {
	case "excel", "word":
		rows := make([][]string, len(messages))
		for i, msg := range messages {
			rows[i] = []string{
				msg.CreatedAt.Format("2006-01-02 15:04:05"),
				strings.ToUpper(msg.Role),
				msg.Content,
			}
		}
		notes := []string{
			fmt.Sprintf("会话：%s", sessionID),
			fmt.Sprintf("导出条数：%d", len(messages)),
			fmt.Sprintf("导出时间：%s", time.Now().Format("2006-01-02 15:04:05")),
		}
		doc := buildTableDocument("对话导出", notes, []string{"时间", "角色", "内容"}, rows)
		filename := fmt.Sprintf("chat_%s_%s", sessionID, time.Now().Format("20060102_150405"))
		contentType := "application/vnd.ms-excel"
		ext := ".xls"
		if format == "word" {
			contentType = "application/msword"
			ext = ".doc"
		}
		filename += ext
		sendDocumentAttachment(c, filename, contentType, doc)
	default:
		var builder strings.Builder
		for _, msg := range messages {
			builder.WriteString(fmt.Sprintf("[%s][%s] %s\n", msg.CreatedAt.Format(time.RFC3339), msg.Role, msg.Content))
		}
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"chat_%s.txt\"", sessionID))
		c.String(http.StatusOK, builder.String())
	}
	success = true
}

func (h *APIHandler) AdminListUsers(c *gin.Context) {
	if h.userService == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Code:    http.StatusServiceUnavailable,
			Message: "User service unavailable",
		})
		return
	}
	users, err := h.userService.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to load users",
			Details: err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Users retrieved",
		Data:    users,
	})
}

func (h *APIHandler) AdminUserUsage(c *gin.Context) {
	if h.monitor == nil {
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Code:    http.StatusServiceUnavailable,
			Message: "Monitor service unavailable",
		})
		return
	}
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	if fromStr := c.Query("from"); fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = parsed
		}
	}
	if toStr := c.Query("to"); toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = parsed
		}
	}
	usage, err := h.monitor.QueryUserUsage(from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to load usage stats",
			Details: err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Usage stats retrieved",
		Data:    usage,
	})
}

// GetDatabaseInfo returns database information
func (h *APIHandler) GetDatabaseInfo(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	success := false
	defer func() {
		h.recordMetric("database_info", start, success, nil)
	}()

	info, err := h.dbClient.GetDatabaseInfo(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Code:    http.StatusInternalServerError,
			Message: "Failed to get database info",
			Details: err.Error(),
		})
		return
	}
	success = true

	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Database info retrieved successfully",
		Data:    info,
	})
}

// SQLWebSocket handles streaming SQL generation over WebSocket
func (h *APIHandler) SQLWebSocket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		h.logger.Warn("WebSocket missing token", zap.String("ip", c.ClientIP()))
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Code:    http.StatusUnauthorized,
			Message: "Missing token",
		})
		return
	}

	claims, err := h.jwtManager.VerifyToken(token)
	if err != nil {
		h.logger.Warn("WebSocket invalid token", zap.String("ip", c.ClientIP()), zap.Error(err))
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Code:    http.StatusUnauthorized,
			Message: "Invalid token",
			Details: err.Error(),
		})
		return
	}

	h.logger.Info("WebSocket request received",
		zap.String("ip", c.ClientIP()),
		zap.String("user_id", claims.UserID),
		zap.String("origin", c.Request.Header.Get("Origin")),
	)

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Warn("WebSocket upgrade failed",
			zap.String("ip", c.ClientIP()),
			zap.String("user_id", claims.UserID),
			zap.Error(err))
		return
	}
	defer func() {
		_ = conn.Close()
		h.logger.Info("WebSocket connection closed", zap.String("user_id", claims.UserID))
	}()

	var req models.SQLGenerateRequest
	if err := conn.ReadJSON(&req); err != nil {
		h.logger.Warn("WebSocket payload invalid",
			zap.String("user_id", claims.UserID),
			zap.Error(err))
		h.writeWSError(conn, "Invalid request payload")
		return
	}

	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		h.logger.Warn("WebSocket empty query", zap.String("user_id", claims.UserID))
		h.writeWSError(conn, "Query is required")
		return
	}

	if req.SessionID == "" {
		req.SessionID = claims.UserID
	}
	if req.RequestID == "" {
		req.RequestID = uuid.New().String()
	}

	h.logger.Info("WebSocket generation started",
		zap.String("user_id", claims.UserID),
		zap.String("session_id", req.SessionID),
		zap.String("request_id", req.RequestID),
	)

	h.handleWebSocketGeneration(conn, claims.UserID, &req)
}

// ListSessions returns available memory sessions for the user
func (h *APIHandler) ListSessions(c *gin.Context) {
	if h.memoryStore == nil {
		c.JSON(http.StatusOK, models.SuccessResponse{
			Code:    http.StatusOK,
			Message: "Memory store disabled",
			Data:    map[string]string{},
		})
		return
	}
	targetUser := scopedUserID(c)
	sessions := h.memoryStore.ListSessions(targetUser)
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Sessions retrieved",
		Data:    sessions,
	})
}

// GetSessionMemory returns full history of a session
func (h *APIHandler) GetSessionMemory(c *gin.Context) {
	if h.memoryStore == nil {
		c.JSON(http.StatusOK, models.SuccessResponse{
			Code:    http.StatusOK,
			Message: "Memory store disabled",
			Data:    []memory.Entry{},
		})
		return
	}
	sessionID := c.Param("session_id")
	targetUser := scopedUserID(c)
	entries := h.memoryStore.GetSession(targetUser, sessionID)
	c.JSON(http.StatusOK, models.SuccessResponse{
		Code:    http.StatusOK,
		Message: "Session history retrieved",
		Data:    entries,
	})
}

// Helper methods

func scopedUserID(c *gin.Context) string {
	userID := c.GetString("user_id")
	if isAdmin(c) {
		if override := strings.TrimSpace(c.Query("user_id")); override != "" {
			return override
		}
	}
	return userID
}

func isAdmin(c *gin.Context) bool {
	return strings.EqualFold(getUserRole(c), "admin")
}

func getUserRole(c *gin.Context) string {
	if roleVal, exists := c.Get("role"); exists {
		if role, ok := roleVal.(string); ok {
			return role
		}
	}
	return ""
}

func (h *APIHandler) getDatabaseSchemaContext(ctx context.Context, tableNames string) (string, error) {
	start := time.Now()
	h.logger.Info("Schema context build started", zap.String("table_filter", tableNames))

	var targetTables []string
	if trimmed := strings.TrimSpace(tableNames); trimmed != "" {
		for _, name := range strings.Split(trimmed, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				targetTables = append(targetTables, strings.ToUpper(name))
			}
		}
	}

	if len(targetTables) == 0 {
		tables, err := h.dbClient.GetAllTables(ctx, true)
		if err != nil {
			h.logger.Error("Schema context failed: list tables", zap.Error(err))
			return "", err
		}
		targetTables = tables
	}

	if len(targetTables) == 0 {
		h.logger.Warn("Schema context empty tables list")
		return "Database schema metadata is unavailable.", nil
	}

	var builder strings.Builder
	builder.WriteString("Available tables in the database:\n")

	const maxTables = 10
	const maxAttempts = 40
	successCount := 0
	attemptCount := 0

	var fallbackTables []string

	for _, table := range targetTables {
		if ctx.Err() != nil {
			h.logger.Warn("Schema context cancelled", zap.Error(ctx.Err()))
			break
		}

		if attemptCount >= maxAttempts && successCount > 0 {
			h.logger.Warn("Schema context attempt limit reached",
				zap.Int("attempts", attemptCount),
				zap.Int("tables_included", successCount))
			break
		}

		if successCount >= maxTables {
			builder.WriteString("\n... and more\n")
			break
		}

		h.logger.Debug("Fetching table metadata", zap.String("table", table))

		tableCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		schema, err := h.dbClient.GetTableSchema(tableCtx, table)
		cancel()
		attemptCount++

		if err != nil {
			h.logger.Warn("Table metadata unavailable", zap.String("table", table), zap.Error(err))
			fallbackTables = append(fallbackTables, table)
			continue
		}

		builder.WriteString("\nTable: " + schema.TableName + "\n")
		if schema.Comment != "" {
			builder.WriteString("Comment: " + schema.Comment + "\n")
		}
		builder.WriteString("Columns:\n")
		for _, col := range schema.Columns {
			builder.WriteString("  - " + col.ColumnName + " (" + col.DataType + ")")
			if col.Comment != "" {
				builder.WriteString(" - " + col.Comment)
			}
			builder.WriteString("\n")
		}
		successCount++
		h.logger.Debug("Table metadata collected",
			zap.String("table", schema.TableName),
			zap.Int("columns", len(schema.Columns)))
	}

	if successCount == 0 && len(fallbackTables) > 0 {
		builder.WriteString("\nMetadata for specific tables is restricted. Known table names include:\n")
		limit := len(fallbackTables)
		if limit > maxTables {
			limit = maxTables
		}
		for i := 0; i < limit; i++ {
			builder.WriteString("  - " + fallbackTables[i] + "\n")
		}
	}

	h.logger.Info("Schema context build completed",
		zap.Int("tables_requested", len(targetTables)),
		zap.Int("tables_included", successCount),
		zap.Int("tables_attempted", attemptCount),
		zap.Duration("elapsed", time.Since(start)))

	return builder.String(), nil
}

func (h *APIHandler) logAuditTrail(userID, action, sql string, success bool, errMsg string) {
	status := "SUCCESS"
	if !success {
		status = "FAILED"
	}

	log := &models.AuditLog{
		ID:        uuid.New().String(),
		UserID:    userID,
		Action:    action,
		SQL:       sql,
		Status:    status,
		ErrorMsg:  errMsg,
		CreatedAt: time.Now(),
	}

	h.logger.Info("Audit trail",
		zap.String("user_id", log.UserID),
		zap.String("action", log.Action),
		zap.String("status", log.Status),
	)
}

func buildMemoryContext(entries []memory.Entry) string {
	if len(entries) == 0 {
		return ""
	}
	var builder strings.Builder
	for idx, entry := range entries {
		builder.WriteString(fmt.Sprintf("%d) USER: %s\n", idx+1, entry.Query))
		builder.WriteString("   SQL: ")
		builder.WriteString(entry.SQL)
		builder.WriteString("\n")
	}
	return builder.String()
}

func (h *APIHandler) appendMemory(userID, sessionID, query string, resp *models.SQLGenerateResponse) {
	if h.memoryStore == nil || resp == nil {
		return
	}
	entry := memory.Entry{
		Query:     query,
		SQL:       resp.SQL,
		Reasoning: resp.Reasoning,
		Source:    resp.Source,
	}
	if err := h.memoryStore.Append(userID, sessionID, entry); err != nil {
		h.logger.Warn("Failed to persist memory entry", zap.Error(err))
	}
}

func convertReportToHistory(report *reports.Report) *models.SQLHistoryRecord {
	if report == nil {
		return nil
	}
	return &models.SQLHistoryRecord{
		ID:          report.ID,
		UserID:      report.UserID,
		SQL:         report.SQL,
		Title:       report.Title,
		Description: report.Description,
		Saved:       true,
		LastRun:     report.UpdatedAt,
		CreatedAt:   report.CreatedAt,
		UpdatedAt:   report.UpdatedAt,
		TemplateID:  report.TemplateID,
		Parameters:  report.Parameters,
		SessionID:   report.SessionID,
		Source:      "saved_report",
	}
}

func (h *APIHandler) updateProgress(requestID, stage, message string) {
	if h.progressStore == nil || requestID == "" {
		return
	}
	if _, ok := h.progressStore.Get(requestID); !ok {
		h.progressStore.Init(requestID, stage, message)
		return
	}
	h.progressStore.Update(requestID, stage, message)
}

func (h *APIHandler) completeProgress(requestID, message string) {
	if h.progressStore == nil || requestID == "" {
		return
	}
	h.progressStore.Complete(requestID, message)
}

func (h *APIHandler) failProgress(requestID, errMsg string) {
	if h.progressStore == nil || requestID == "" {
		return
	}
	h.progressStore.Fail(requestID, errMsg)
}

func (h *APIHandler) handleWebSocketGeneration(conn *websocket.Conn, userID string, req *models.SQLGenerateRequest) {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = userID
	}
	start := time.Now()
	success := false
	metricExtra := map[string]interface{}{"session_id": sessionID}
	defer func() {
		h.recordMetric("generate_ws", start, success, metricExtra)
	}()

	h.writeWSProgress(conn, "received", "已收到生成请求")
	h.saveChatMessage(userID, sessionID, "user", req.Query)

	if h.templateSvc != nil {
		if tpl := h.templateSvc.Match(req.Query); tpl != nil {
			h.writeWSProgress(conn, "template_matched", fmt.Sprintf("命中模版：%s", tpl.Name))
			resp := &models.SQLGenerateResponse{
				SQL:        tpl.SQL,
				Reasoning:  fmt.Sprintf("命中内置报表模版：%s", tpl.Name),
				Source:     "template",
				TemplateID: tpl.ID,
				UsedMemory: false,
				RequestID:  req.RequestID,
			}
			h.appendMemory(userID, sessionID, req.Query, resp)
			h.saveChatMessage(userID, sessionID, "assistant", formatSQLChatMessage(resp))
			metricExtra["template_id"] = tpl.ID
			success = true
			h.writeWSComplete(conn, resp)
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.generateTimeout)
	defer cancel()

	h.writeWSProgress(conn, "prepare_context", "正在加载数据库元数据")
	schemaContext, err := h.getDatabaseSchemaContext(ctx, req.TableNames)
	if err != nil {
		h.logger.Warn("Failed to get schema context", zap.Error(err))
		schemaContext = "Error retrieving schema"
	}

	var memEntries []memory.Entry
	if h.memoryStore != nil {
		memEntries = h.memoryStore.GetRecent(userID, sessionID, 5)
	}
	memoryContext := buildMemoryContext(memEntries)
	if len(memEntries) > 0 {
		h.writeWSProgress(conn, "memory_loaded", fmt.Sprintf("复用 %d 条历史", len(memEntries)))
	}

	h.writeWSProgress(conn, "llm_call", "LLM 正在生成 SQL")
	resp, err := h.llmClient.GenerateSQLStream(ctx, req, schemaContext, memoryContext, func(chunk string) {
		h.writeWSChunk(conn, chunk, false)
	})
	if err != nil {
		metricExtra["error"] = err.Error()
		h.writeWSError(conn, err.Error())
		return
	}
	resp.Source = "llm"
	resp.UsedMemory = len(memEntries) > 0
	resp.RequestID = req.RequestID

	h.appendMemory(userID, sessionID, req.Query, resp)
	h.saveChatMessage(userID, sessionID, "assistant", formatSQLChatMessage(resp))
	success = true
	h.writeWSComplete(conn, resp)
}

func (h *APIHandler) writeWSProgress(conn *websocket.Conn, stage, message string) {
	_ = conn.WriteJSON(wsMessage{
		Type:    "progress",
		Stage:   stage,
		Message: message,
	})
}

func (h *APIHandler) writeWSChunk(conn *websocket.Conn, chunk string, done bool) {
	_ = conn.WriteJSON(wsMessage{
		Type:  "chunk",
		Chunk: chunk,
		Done:  done,
	})
}

func (h *APIHandler) writeWSComplete(conn *websocket.Conn, resp *models.SQLGenerateResponse) {
	_ = conn.WriteJSON(wsMessage{
		Type:       "complete",
		SQL:        resp.SQL,
		Reasoning:  resp.Reasoning,
		TemplateID: resp.TemplateID,
		UsedMemory: resp.UsedMemory,
	})
}

func (h *APIHandler) writeWSError(conn *websocket.Conn, message string) {
	_ = conn.WriteJSON(wsMessage{
		Type:  "error",
		Error: message,
	})
}

func (h *APIHandler) saveChatMessage(userID, sessionID, role, content string) {
	if h.chatStore == nil || strings.TrimSpace(content) == "" {
		return
	}
	if err := h.chatStore.SaveMessage(userID, sessionID, role, content); err != nil {
		h.logger.Warn("Failed to save chat message", zap.Error(err))
	}
}

func formatSQLChatMessage(resp *models.SQLGenerateResponse) string {
	if resp == nil {
		return ""
	}
	message := strings.TrimSpace(resp.SQL)
	reasoning := strings.TrimSpace(resp.Reasoning)
	if reasoning != "" {
		if message != "" {
			message += "\n\n"
		}
		message += "Reasoning:\n" + reasoning
	}
	if message == "" {
		message = "生成结果已返回，请查看上方提示。"
	}
	return message
}

func (h *APIHandler) needsGuidance(sql string) bool {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return true
	}
	upper := strings.ToUpper(sql)
	if strings.HasPrefix(upper, "ERROR") {
		return true
	}
	keywords := []string{"NOT FOUND", "DOES NOT CONTAIN", "NO TABLE"}
	for _, k := range keywords {
		if strings.Contains(upper, k) {
			return true
		}
	}
	return false
}

func (h *APIHandler) generateGuidanceResponse(req models.SQLGenerateRequest, schemaContext, issue string, requestID string) *models.SQLGenerateResponse {
	if h.llmClient == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hint, err := h.llmClient.GenerateGuidance(ctx, req.Query, schemaContext, issue)
	if err != nil {
		h.logger.Warn("Failed to get guidance", zap.Error(err))
		return nil
	}
	return &models.SQLGenerateResponse{
		SQL:        "",
		Reasoning:  hint,
		Source:     "assistant_hint",
		RequestID:  requestID,
		UsedMemory: false,
	}
}

func normalizeKeywords(keywords []string) []string {
	var result []string
	for _, kw := range keywords {
		if trimmed := strings.TrimSpace(kw); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (h *APIHandler) recordMetric(event string, start time.Time, success bool, extra map[string]interface{}) {
	if h.monitor == nil {
		return
	}
	if extra == nil {
		extra = make(map[string]interface{})
	}
	h.monitor.Record(event, time.Since(start), success, extra)
}

func sendDocumentAttachment(c *gin.Context, filename, contentType, payload string) {
	safeName := sanitizeFilename(filename)
	escaped := url.PathEscape(safeName)
	c.Header("Content-Type", contentType+"; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=utf-8''%s", safeName, escaped))
	c.String(http.StatusOK, payload)
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("export_%s", time.Now().Format("20060102_150405"))
	}
	replacer := strings.NewReplacer(`"`, "", "\n", "_", "\r", "_", "/", "_", "\\", "_")
	return replacer.Replace(name)
}

func truncateText(value string, maxLen int) string {
	if maxLen <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxLen {
		return value
	}
	return string(runes[:maxLen]) + "..."
}
