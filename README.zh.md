# DB Assistant / 数据库聊天助手

用自然语言问数据库：结合 Oracle 元数据与对话记忆（自动压缩至约 9.6K 字符），流式生成 SQL、只读安全执行，并提供监控与告警。English version: [README.md](README.md).

## 功能特性
- 🧠 **上下文 NL2SQL**：融合数据库元数据和最近 12 条对话（自动压缩），支持流式返回与模版命中。
- 🔒 **安全默认**：强制 SELECT-only、只读账号、危险关键词拦截、分页与超时限制、敏感列脱敏。
- 🛠 **失败自愈/提示**：执行失败或缺表时返回简洁中文原因与改写建议，可请求 AI 调试 SQL。
- 📊 **监控告警**：成功率、耗时、失败趋势、最新事件面板；可选邮件告警。
- 🖥 **多终端**：React 18 + TypeScript Web 前端，可选 Tauri 桌面端。
- 🧩 **模版与报表**：支持保存/编辑/应用 SQL 模版与个人报表，会话搜索与导出。

## 目录结构
```
db_asst/
├── backend/      # Go + Gin API，Oracle、LLM、监控、记忆等
├── web/          # React 前端
├── desktop/      # Tauri 桌面端（可选）
└── scripts/...   # 其他辅助资源
```

## 环境依赖
- Go 1.21+
- Node.js 18+（推荐 pnpm） / Rust 1.72+（桌面端）
- Oracle 只读账号
- LLM API Key（OpenAI/DeepSeek/兼容 OpenAI 的 Provider）

## 快速开始
### 后端
```bash
cd backend
cp .env.example .env   # 如存在示例
# 编辑 .env，填入 Oracle、LLM、JWT、邮件配置
go run ./cmd/api       # 默认 :8080

# 可选：MCP
go run ./cmd/mcp
```

最小化 .env 示例：
```env
SERVER_PORT=8080
ENVIRONMENT=development
LOG_LEVEL=info

ORACLE_USER=readonly_user
ORACLE_PASSWORD=your_password
ORACLE_HOST=db.example.com
ORACLE_PORT=1521
ORACLE_SID=ORCL
ORACLE_SCHEMA=YX_READ

LLM_PROVIDER=openai
LLM_API_KEY=sk-***
LLM_MODEL=gpt-4o
LLM_TIMEOUT=30

JWT_SECRET=change_me
SQL_DEFAULT_PAGE_SIZE=50
SQL_MAX_PAGE_SIZE=200
SENSITIVE_COLUMNS=phone,id_card,email
EMAIL_SMTP_HOST=smtp.example.com
```

### 前端
```bash
cd web
pnpm install
pnpm dev
# 如需指定后端：
# VITE_API_URL=http://localhost:8080/api pnpm dev
```

### 桌面端（可选）
```bash
cd desktop
pnpm install
pnpm dev
pnpm build
```

## 关键配置
- **LLM**：`LLM_PROVIDER`（openai/deepseek/custom）、`LLM_MODEL`，兼容代理可配 `LLM_BASE_URL`。
- **记忆**：压缩到约 9600 字符，取最近 12 条对话；提示词要求模型在用户输入很短时也参考历史。
- **执行安全**：仅允许 SELECT，超时/分页/脱敏，强制只读账号。
- **监控告警**：配置 `EMAIL_SMTP_*`、`EMAIL_ALERT_*` 可启用失败/耗时告警；指标在前端“运行监控”面板查看。
- **会话/导出**：支持会话搜索导出，模版/报表 CRUD。

## 常见问题
- **提示缺表**：检查 Oracle schema 是否包含目标表，必要时调整 `ORACLE_SCHEMA`。
- **LLM 失败/限流**：核对 API Key/配额/并发，或切换兼容网关。
- **前端连不上后端**：确认 `VITE_API_URL`、后端端口、CORS 与网络可达性。

## 许可证
MIT（见 `LICENSE`）。
