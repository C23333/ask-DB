# DB Assistant - Natural Language to SQL

A powerful application that helps business users query databases using natural language, powered by LLM (Large Language Models). Ask in plain English, get SQL queries instantly, execute them safely, and debug with AI assistance.

## Features

✨ **Core Features**
- **Natural Language SQL Generation**: Describe what you want to retrieve in plain English, AI generates the SQL
- **Web & Desktop Access**: Use via browser or download as a desktop app (Windows, Mac, Linux)
- **Safe SQL Execution**: Only SELECT queries allowed, read-only database account, comprehensive security checks
- **AI-Powered Debugging**: When SQL fails, ask AI to fix it with one click
- **Query History & Saving**: Save frequently used queries, access them anytime
- **Database Schema Awareness**: AI understands your database structure for accurate SQL generation

🔒 **Security**
- JWT-based authentication
- SQL injection prevention
- SELECT-only query enforcement
- Read-only database account support
- Comprehensive audit logging
- Input validation and sanitization

🚀 **Tech Stack**
- **Backend**: Go + Gin (Web API), Oracle Database
- **Frontend**: React 18 + TypeScript
- **Desktop**: Tauri (cross-platform)
- **LLM Integration**: Flexible API-based (OpenAI, Claude, or any OpenAI-compatible API)

## Project Structure

```
db_asst/
├── backend/
│   ├── cmd/
│   │   ├── api/          # Web API server entry point
│   │   └── mcp/          # MCP service entry point
│   ├── internal/
│   │   ├── api/          # API handlers and routes
│   │   ├── auth/         # JWT and user management
│   │   ├── db/           # Oracle database connection
│   │   ├── executor/     # SQL execution engine
│   │   ├── llm/          # LLM integration layer
│   │   ├── mcp/          # MCP server
│   │   ├── logger/       # Logging utility
│   │   └── models/       # Data models
│   ├── config/           # Configuration
│   └── go.mod            # Go dependencies
├── web/                  # React web frontend
│   ├── src/
│   │   ├── pages/        # Page components
│   │   ├── components/   # Reusable components
│   │   ├── services/     # API client
│   │   ├── stores/       # Zustand stores
│   │   └── types/        # TypeScript types
│   └── package.json
├── desktop/              # Tauri desktop application
│   ├── src/              # Tauri Rust code
│   ├── Cargo.toml
│   └── tauri.conf.json
└── docs/                 # Documentation
```

## Quick Start

### Prerequisites

- Go 1.21+
- Node.js 18+ and npm
- Rust (for building Tauri)
- Oracle Database (configured and accessible)
- LLM API key (OpenAI, Claude, or compatible service)

### 1. Setup Backend

```bash
cd backend

# Copy example environment file
cp .env.example .env

# Edit .env with your configuration
# - Oracle database credentials
# - LLM API key
# - JWT secret
nano .env
```

**Sample .env:**
```env
SERVER_PORT=8080
ENVIRONMENT=development
LOG_LEVEL=info

# Oracle Database
ORACLE_USER=your_readonly_user
ORACLE_PASSWORD=your_password
ORACLE_HOST=your_oracle_host
ORACLE_PORT=1521
ORACLE_SID=your_sid
# Optional: override schema owner used for metadata queries
ORACLE_SCHEMA=target_schema_owner

# LLM Configuration
LLM_PROVIDER=openai
LLM_API_KEY=sk-...
LLM_MODEL=gpt-3.5-turbo
LLM_TIMEOUT=30

# Optional: Override for proxy services
# LLM_BASE_URL=https://api-proxy.example.com/v1
```

**Start Web API Server:**
```bash
go run cmd/api/main.go
# Server will start on http://localhost:8080
```

**Start MCP Server (optional, in another terminal):**
```bash
go run cmd/mcp/main.go
# MCP server will listen on :9000
```

### 2. Setup Web Frontend

```bash
cd web

# Install dependencies
npm install

# Start development server
npm run dev
# Frontend will be available at http://localhost:5173
```

**Build for production:**
```bash
npm run build
# Output in dist/
```

### 3. Build Desktop App (Optional)

```bash
cd desktop

# Install Tauri CLI
npm install

# Run in development mode
npm run dev

# Build for production
npm run build
# Output in src-tauri/target/release/
```

## Usage

### Web Access
1. Open http://localhost:5173
2. Register or login (demo/demo1234)
3. Use natural language to query:
   - Ask: "Show me the total sales by region"
   - AI generates SQL: `SELECT region, SUM(amount) FROM sales GROUP BY region`
4. Execute, view results, debug, and save queries

### Desktop App
1. Run the desktop application
2. Same features as web, but installed locally
3. No browser required

## API Endpoints

### Authentication
- `POST /api/auth/register` - Register new user
- `POST /api/auth/login` - Login and get JWT token

### SQL Operations (require authentication)
- `POST /api/sql/generate` - Generate SQL from natural language
- `POST /api/sql/execute` - Execute SQL query
- `POST /api/sql/debug` - Debug failed SQL with AI
- `POST /api/sql/save` - Save query to history
- `GET /api/sql/history` - Get user's query history

### Database Info
- `GET /api/database/info` - Get database information

### Health Check
- `GET /health` - Check if server is running

## Monitoring & Alerting

- 所有关键操作（SQL 生成、执行、报表保存、模版 CRUD、聊天导出等）都会写入 `system_metrics` 表，可通过前端的“运行监控”面板查看成功率、延迟、失败趋势以及最新异常。
- 前端内置迷你折线图，可快速观察最近 24 小时的调用波动，也可以点击“刷新监控”实时刷新。
- 支持邮件报警，配置 `.env` 中的 SMTP 信息即可开启，常用环境变量如下：

```env
EMAIL_SMTP_HOST=smtp.example.com
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USER=bot@example.com
EMAIL_SMTP_PASSWORD=change_me
EMAIL_ALERT_TO=ops@example.com,owner@example.com
# 逗号分隔的事件类型，留空则默认仅针对 SQL 生成/执行
EMAIL_ALERT_EVENT_TYPES=generate_rest,generate_ws,execute_sql,report_save
# 只有耗时超过阈值（毫秒）时才会报警
EMAIL_ALERT_MIN_DURATION_MS=5000
# 同一事件的报警冷却时间（秒），避免邮件轰炸
EMAIL_ALERT_COOLDOWN_SEC=300
```

当满足配置条件且事件失败时，系统会自动发送报警邮件（支持多个收件人）。

## Query Pagination & Masking

- 每次执行 SQL 默认只拉取 50 行，可在前端查询结果区域自由调整（20/50/100/200），也可以使用 `.env` 中的 `SQL_DEFAULT_PAGE_SIZE`、`SQL_MAX_PAGE_SIZE` 做全局控制。
- API 自动返回 `page`, `page_size`, `has_more` 等信息，前端“上一页/下一页/刷新”按钮直接调用即可。
- 如需自动脱敏特定字段（如手机号、证件号），配置 `SENSITIVE_COLUMNS`（逗号分隔）后系统会在返回结果和导出中统一替换为 `***`。

## Configuration

### LLM Providers

**OpenAI:**
```env
LLM_PROVIDER=openai
LLM_API_KEY=sk-...
LLM_MODEL=gpt-3.5-turbo
```

**Claude (Anthropic):**
```env
LLM_PROVIDER=claude
LLM_API_KEY=sk-ant-...
LLM_MODEL=claude-3-sonnet-20240229
```

**Custom/Proxy Service (OpenAI-compatible):**
```env
LLM_PROVIDER=custom
LLM_API_KEY=your-key
LLM_MODEL=gpt-3.5-turbo
LLM_BASE_URL=https://api-proxy.example.com/v1
```

### Oracle Database Connection

The application uses Oracle's native connection string format:
```
USER/PASSWORD@HOST:PORT/SID
```

For example:
- `readonly_user/password@db.company.com:1521/ORCL`

**Recommendations:**
1. Create a dedicated read-only user:
   ```sql
   CREATE USER readonly_user IDENTIFIED BY password;
   GRANT SELECT ON schema.* TO readonly_user;
   ```

2. Restrict network access at the firewall level
3. Use connection pooling (configured in `internal/db/oracle.go`)
4. Monitor query execution with Oracle audit logs

## Security Considerations

1. **SQL Injection Prevention**
   - All queries validated before execution
   - Only SELECT statements allowed
   - Pattern-matching against dangerous keywords
   - Parameter verification (implementation-ready)

2. **Database Access**
   - Read-only accounts only
   - Network-level security
   - Connection pooling limits
   - Query timeout enforcement (30s default)

3. **User Authentication**
   - JWT tokens with 24h expiration
   - Password hashing with bcrypt
   - Token refresh support (can be added)
   - CORS enabled for web access

4. **Audit & Monitoring**
   - All SQL execution logged
   - User action tracking
   - Error recording
   - Execution time metrics

5. **Data Protection**
   - HTTPS support (configure in production)
   - Sensitive fields not exposed in responses
   - Configuration stored in environment variables
   - No secrets in code

## Production Deployment

### Docker Setup (Recommended)

**Dockerfile for Backend:**
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY backend .
RUN go build -o api cmd/api/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/api /app/api
EXPOSE 8080
CMD ["./app/api"]
```

### Deployment Checklist

- [ ] Use production-grade database (Oracle Cloud, etc.)
- [ ] Configure HTTPS/TLS certificates
- [ ] Set strong JWT_SECRET
- [ ] Enable database audit logging
- [ ] Setup monitoring and alerting
- [ ] Configure backups
- [ ] Rate limiting on API endpoints
- [ ] Load balancing if needed
- [ ] Environment-specific configuration
- [ ] Regular security audits

## Troubleshooting

**Cannot connect to Oracle:**
- Verify Oracle connection string in .env
- Check firewall rules
- Test connection: `tnsping ORACLE_SID`

**LLM API errors:**
- Check API key validity
- Verify API quota/limits
- Check rate limiting
- Test with simple prompts first

**Frontend cannot reach backend:**
- Verify backend is running on port 8080
- Check CORS configuration
- Verify API_URL in frontend config

## Future Enhancements

- [ ] Multi-database support (MySQL, PostgreSQL, etc.)
- [ ] Advanced query optimization suggestions
- [ ] Query performance analysis
- [ ] Scheduled query execution
- [ ] Result visualization and charting
- [ ] Team collaboration features
- [ ] Query templates library
- [ ] Advanced permission management
- [ ] Batch query execution
- [ ] Data export to multiple formats

## Contributing

Contributions are welcome! Please ensure:
- Code follows Go/TypeScript conventions
- Tests are included for new features
- Security best practices are followed
- Documentation is updated

## License

MIT License - See LICENSE file for details

## Support

For issues or questions:
1. Check the troubleshooting section
2. Review existing documentation
3. Create an issue with:
   - Clear description
   - Steps to reproduce
   - Environment details (OS, versions)
   - Error messages/logs

## Roadmap

**v0.2 (Next Release)**
- PostgreSQL and MySQL support
- Advanced permission management
- Query performance analysis

**v0.3**
- Real-time collaboration
- Result visualization
- Batch operations

**v1.0**
- Full production release
- Multi-database support
- Enterprise features
