package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"

	"go.uber.org/zap"

	"github.com/yourusername/db_asst/internal/db"
	"github.com/yourusername/db_asst/internal/models"
)

type MCPServer struct {
	dbClient *db.OracleClient
	logger   *zap.Logger
	listener net.Listener
	mu       sync.RWMutex
}

type MCPRequest struct {
	ID     string                 `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

type MCPResponse struct {
	ID     string      `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// NewMCPServer creates a new MCP server
func NewMCPServer(dbClient *db.OracleClient, logger *zap.Logger) *MCPServer {
	return &MCPServer{
		dbClient: dbClient,
		logger:   logger,
	}
}

// Start starts the MCP server
func (s *MCPServer) Start(addr string) error {
	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.logger.Info("MCP server started", zap.String("addr", addr))

	go s.acceptConnections()

	return nil
}

// Stop stops the MCP server
func (s *MCPServer) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *MCPServer) acceptConnections() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.logger.Error("Failed to accept connection", zap.Error(err))
			break
		}

		go s.handleConnection(conn)
	}
}

func (s *MCPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req MCPRequest
		if err := decoder.Decode(&req); err != nil {
			if err != io.EOF {
				s.logger.Error("Failed to decode request", zap.Error(err))
			}
			break
		}

		resp := s.processRequest(context.Background(), &req)
		if err := encoder.Encode(resp); err != nil {
			s.logger.Error("Failed to encode response", zap.Error(err))
			break
		}
	}
}

func (s *MCPServer) processRequest(ctx context.Context, req *MCPRequest) *MCPResponse {
	resp := &MCPResponse{ID: req.ID}

	switch req.Method {
	case "get_tables":
		result, err := s.GetTables(ctx)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = result
		}

	case "get_table_schema":
		tableName, ok := req.Params["table_name"].(string)
		if !ok {
			resp.Error = "table_name parameter is required"
			break
		}
		result, err := s.GetTableSchema(ctx, tableName)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = result
		}

	case "get_all_schemas":
		result, err := s.GetAllSchemas(ctx)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = result
		}

	case "search_tables":
		pattern, ok := req.Params["pattern"].(string)
		if !ok {
			resp.Error = "pattern parameter is required"
			break
		}
		result, err := s.SearchTables(ctx, pattern)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = result
		}

	case "get_table_columns":
		tableName, ok := req.Params["table_name"].(string)
		if !ok {
			resp.Error = "table_name parameter is required"
			break
		}
		result, err := s.GetTableColumns(ctx, tableName)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = result
		}

	default:
		resp.Error = fmt.Sprintf("unknown method: %s", req.Method)
	}

	return resp
}

// GetTables returns a list of all tables
func (s *MCPServer) GetTables(ctx context.Context) (map[string]interface{}, error) {
	tables, err := s.dbClient.GetAllTables(ctx, true)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"tables": tables,
		"count":  len(tables),
	}, nil
}

// GetTableSchema returns the schema for a specific table
func (s *MCPServer) GetTableSchema(ctx context.Context, tableName string) (interface{}, error) {
	schema, err := s.dbClient.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	return schema, nil
}

// GetAllSchemas returns the schema for all tables
func (s *MCPServer) GetAllSchemas(ctx context.Context) (interface{}, error) {
	tables, err := s.dbClient.GetAllTables(ctx, true)
	if err != nil {
		return nil, err
	}

	var schemas []*models.TableSchema
	for _, table := range tables {
		schema, err := s.dbClient.GetTableSchema(ctx, table)
		if err != nil {
			s.logger.Warn("Failed to get schema", zap.String("table", table), zap.Error(err))
			continue
		}
		schemas = append(schemas, schema)
	}

	return map[string]interface{}{
		"schemas": schemas,
		"count":   len(schemas),
	}, nil
}

// SearchTables searches for tables by name pattern
func (s *MCPServer) SearchTables(ctx context.Context, pattern string) (interface{}, error) {
	allTables, err := s.dbClient.GetAllTables(ctx, true)
	if err != nil {
		return nil, err
	}

	var matchedTables []string
	for _, table := range allTables {
		// Simple pattern matching (contains)
		if contains(table, pattern) {
			matchedTables = append(matchedTables, table)
		}
	}

	return map[string]interface{}{
		"tables": matchedTables,
		"count":  len(matchedTables),
	}, nil
}

// GetTableColumns returns the columns for a specific table
func (s *MCPServer) GetTableColumns(ctx context.Context, tableName string) (interface{}, error) {
	columns, err := s.dbClient.GetTableColumns(ctx, tableName)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"table_name": tableName,
		"columns":    columns,
		"count":      len(columns),
	}, nil
}

// Helper function
func contains(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
