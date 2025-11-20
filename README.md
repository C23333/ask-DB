# DB Assistant

Natural-language-to-SQL assistant for Oracle: ask in plain language, get streaming SQL with context memory, execute safely, and monitor everything. Looking for Chinese docs? See [README.zh.md](README.zh.md).

## Features
- **NL2SQL with context**: merges DB metadata and recent conversation memory (auto-compressed to ~9.6K chars), supports streaming tokens and template hits.
- **Safe by default**: SELECT-only, read-only DB account, dangerous keyword blocking, pagination/timeouts, sensitive-column masking.
- **Recovery & hints**: if execution fails or schema lacks a table, returns concise Chinese errors and rewrite suggestions; can auto-debug SQL.
- **Monitoring & alerts**: success rate, latency, failure trends, recent events panel; optional email alerts.
- **Multi-endpoint**: React 18 + TypeScript web app; optional Tauri desktop build.
- **Templates & reports**: save/edit/apply SQL templates and personal reports; session search/export supported.

## Structure
```
db_asst/
├── backend/      # Go + Gin API, Oracle, LLM, monitoring, memory, templates, chat, etc.
├── web/          # React frontend
├── desktop/      # Tauri desktop (optional)
└── scripts/...   # helper resources
```

## Prerequisites
- Go 1.21+
- Node.js 18+ (pnpm/npm) / Rust 1.72+ (desktop only)
- Oracle read-only credentials
- LLM API key (OpenAI/DeepSeek/OpenAI-compatible)

## Quick Start
### Backend
```bash
cd backend
cp .env.example .env   # if available
# edit .env with Oracle, LLM, JWT, email settings
go run ./cmd/api       # default :8080

# optional: MCP
go run ./cmd/mcp
```

Minimal `.env` sample:
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

### Web Frontend
```bash
cd web
pnpm install
pnpm dev
# specify backend if needed:
# VITE_API_URL=http://localhost:8080/api pnpm dev
```

### Desktop (optional)
```bash
cd desktop
pnpm install      # installs Tauri deps
pnpm dev          # dev mode
pnpm build        # installers
```

## Configuration Highlights
- **LLM**: `LLM_PROVIDER` (openai/deepseek/custom), `LLM_MODEL`, optional `LLM_BASE_URL` for OpenAI-compatible proxies.
- **Memory**: auto-compress to ~9600 chars, pulls last 12 turns; prompt nudges model to infer intent from history if the latest user text is brief.
- **Execution safety**: SELECT-only, timeouts, pagination, masking; strongly prefer a read-only DB account.
- **Monitoring/alerts**: configure `EMAIL_SMTP_*` and `EMAIL_ALERT_*` to enable failure/latency alerts; metrics visible in the “运行监控” panel.
- **Sessions/export**: session search, text export; templates/reports CRUD.

## Troubleshooting
- **Missing tables**: verify Oracle schema or adjust `ORACLE_SCHEMA`.
- **LLM rate/availability**: check API key, quotas, and proxy; try a compatible gateway.
- **Frontend can’t reach backend**: verify `VITE_API_URL`, backend port, CORS, and network reachability.

## License
MIT (see `LICENSE`).
