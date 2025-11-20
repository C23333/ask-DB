# Deployment Guide

## Local Development Setup

### 1. Backend Setup

```bash
cd backend

# Create .env file from example
cp .env.example .env

# Edit with your Oracle database credentials
# ORACLE_USER=your_readonly_user
# ORACLE_PASSWORD=password
# ORACLE_HOST=your_oracle_host
# ORACLE_PORT=1521
# ORACLE_SID=ORCL
# ORACLE_SCHEMA=target_schema_owner
# LLM_API_KEY=your_api_key

# Install dependencies
go mod download

# Run the application
go run cmd/api/main.go
```

The API server will start on `http://localhost:8080`

### 2. Frontend Setup

```bash
cd web

# Install dependencies
npm install

# Start development server
npm run dev
```

The web frontend will be available at `http://localhost:5173`

### 3. Access the Application

1. Open browser to `http://localhost:5173`
2. Register: Create a new account
3. Login: Use demo/demo1234 (created on first run in dev mode)
4. Start querying!

## Production Deployment

### Option 1: Docker Compose (Recommended)

**docker-compose.yml:**
```yaml
version: '3.8'

services:
  api:
    build:
      context: .
      dockerfile: backend/Dockerfile
    environment:
      - ENVIRONMENT=production
      - SERVER_PORT=8080
      - ORACLE_USER=${ORACLE_USER}
      - ORACLE_PASSWORD=${ORACLE_PASSWORD}
      - ORACLE_HOST=${ORACLE_HOST}
      - ORACLE_PORT=1521
      - ORACLE_SID=${ORACLE_SID}
      - JWT_SECRET=${JWT_SECRET}
      - LLM_PROVIDER=${LLM_PROVIDER}
      - LLM_API_KEY=${LLM_API_KEY}
      - LLM_MODEL=${LLM_MODEL}
    ports:
      - "8080:8080"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  web:
    image: node:18-alpine
    working_dir: /app
    volumes:
      - ./web:/app
    command: npm run build && npm install -g serve && serve -s dist -l 5173
    environment:
      - VITE_API_URL=http://localhost:8080/api
    ports:
      - "5173:5173"
    depends_on:
      - api
```

**Run:**
```bash
# Create .env file with production values
cp .env.example .env
# Edit .env with production credentials

# Start services
docker-compose up -d

# View logs
docker-compose logs -f api

# Stop services
docker-compose down
```

### Option 2: Kubernetes

**deploy.yaml:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: db-assistant-api
spec:
  replicas: 2
  selector:
    matchLabels:
      app: db-assistant-api
  template:
    metadata:
      labels:
        app: db-assistant-api
    spec:
      containers:
      - name: api
        image: your-registry/db-assistant-api:latest
        ports:
        - containerPort: 8080
        env:
        - name: ORACLE_USER
          valueFrom:
            secretKeyRef:
              name: db-secrets
              key: oracle-user
        - name: ORACLE_PASSWORD
          valueFrom:
            secretKeyRef:
              name: db-secrets
              key: oracle-password
        - name: JWT_SECRET
          valueFrom:
            secretKeyRef:
              name: db-secrets
              key: jwt-secret
        - name: LLM_API_KEY
          valueFrom:
            secretKeyRef:
              name: db-secrets
              key: llm-api-key
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: db-assistant-api
spec:
  selector:
    app: db-assistant-api
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
  type: LoadBalancer
```

**Deploy:**
```bash
# Create secrets
kubectl create secret generic db-secrets \
  --from-literal=oracle-user=your_user \
  --from-literal=oracle-password=your_password \
  --from-literal=jwt-secret=your_secret \
  --from-literal=llm-api-key=your_api_key

# Deploy
kubectl apply -f deploy.yaml

# Check status
kubectl get deployments
kubectl logs -l app=db-assistant-api
```

### Option 3: Manual Server Deployment

**Linux/Ubuntu:**

```bash
# 1. Install Go 1.21+
wget https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# 2. Clone and setup
git clone <your-repo>
cd db_asst/backend

# 3. Create .env with production values
cat > .env << EOF
ENVIRONMENT=production
SERVER_PORT=8080
ORACLE_USER=your_user
ORACLE_PASSWORD=your_password
ORACLE_HOST=your_host
ORACLE_PORT=1521
ORACLE_SID=your_sid
ORACLE_SCHEMA=target_schema_owner
JWT_SECRET=generate-a-long-random-string
LLM_PROVIDER=openai
LLM_API_KEY=your_api_key
LLM_MODEL=gpt-3.5-turbo
EOF

# 4. Build binary
go build -o db_asst cmd/api/main.go

# 5. Create systemd service
sudo tee /etc/systemd/system/db-asst.service > /dev/null <<EOF
[Unit]
Description=DB Assistant API
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/db_asst
ExecStart=/opt/db_asst/db_asst
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# 6. Enable and start service
sudo systemctl daemon-reload
sudo systemctl enable db-asst
sudo systemctl start db-asst

# 7. Check status
sudo systemctl status db-asst
```

**Setup Reverse Proxy (Nginx):**

```nginx
server {
    listen 80;
    server_name your-domain.com;

    # Redirect to HTTPS
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name your-domain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    # API backend
    location /api {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # CORS headers
        add_header 'Access-Control-Allow-Origin' '*' always;
        add_header 'Access-Control-Allow-Methods' 'GET, POST, PUT, DELETE, OPTIONS' always;
    }

    # Static frontend
    location / {
        root /opt/db_asst/web/dist;
        try_files $uri /index.html;
    }
}
```

## Monitoring & Maintenance

### Logs

**Backend logs:**
```bash
# View last 100 lines
tail -100 /var/log/db-asst/api.log

# Follow logs in real-time
tail -f /var/log/db-asst/api.log

# View specific date range
journalctl -u db-asst --since "2024-01-01" --until "2024-01-02"
```

### Health Checks

```bash
# Check API health
curl http://localhost:8080/health

# Check with authentication
curl -H "Authorization: Bearer YOUR_TOKEN" http://localhost:8080/api/database/info
```

### Performance Monitoring

```bash
# Monitor resource usage
watch -n 1 'ps aux | grep db_asst'

# Check open connections
lsof -i :8080

# Monitor logs for errors
tail -f /var/log/db-asst/api.log | grep ERROR
```

### Database Maintenance

```sql
-- Monitor Oracle connections
SELECT * FROM v$session WHERE username = 'your_readonly_user';

-- Monitor slow queries (requires audit enabled)
SELECT * FROM dba_audit_trail
WHERE USERNAME = 'your_readonly_user'
ORDER BY TIMESTAMP DESC;

-- Check table statistics
SELECT table_name, num_rows, last_analyzed
FROM user_tables;
```

## Backup & Recovery

### Database Backup

```bash
# Export Oracle database
expdp your_user/password@your_sid \
    DIRECTORY=data_pump_dir \
    DUMPFILE=db_asst_backup.dmp \
    LOGFILE=db_asst_backup.log \
    FULL=Y

# Backup application data
tar -czf db-asst-backup-$(date +%Y%m%d).tar.gz \
    backend/.env \
    web/dist \
    desktop/target/release/
```

### Restore

```bash
# Restore Oracle database
impdp your_user/password@your_sid \
    DIRECTORY=data_pump_dir \
    DUMPFILE=db_asst_backup.dmp \
    LOGFILE=restore.log

# Restore application files
tar -xzf db-asst-backup-YYYYMMDD.tar.gz
```

## Troubleshooting

### API won't start

1. Check configuration:
   ```bash
   cat backend/.env | grep -v '^#' | grep -v '^$'
   ```

2. Test database connection:
   ```bash
   sqlplus your_user/password@your_sid
   ```

3. Check logs:
   ```bash
   journalctl -u db-asst -n 50
   ```

### High memory usage

1. Monitor goroutines:
   ```bash
   curl http://localhost:8080/debug/pprof/goroutine
   ```

2. Check for connection leaks:
   ```sql
   SELECT count(*) FROM v$session WHERE username = 'your_readonly_user';
   ```

3. Increase db connection limits if needed

### Performance issues

1. Check slow queries:
   ```sql
   -- Oracle
   SELECT * FROM v$sql WHERE elapsed_time > 1000000 ORDER BY elapsed_time DESC;
   ```

2. Monitor API response times in logs

3. Consider query optimization or indexing

## Security Updates

1. **Go Updates:**
   ```bash
   go get -u ./...
   go mod tidy
   ```

2. **Node Dependencies:**
   ```bash
   cd web
   npm audit
   npm update
   ```

3. **Regular Security Audits:**
   - Review access logs
   - Check for failed authentication attempts
   - Monitor unusual query patterns

## Rollback Procedure

```bash
# Tag current deployment
git tag -a v0.1-production -m "Production release"
git push origin v0.1-production

# If needed, rollback to previous version
git checkout v0.0-production
go build -o db_asst cmd/api/main.go
systemctl restart db-asst

# Verify
curl http://localhost:8080/health
```

## Performance Optimization

### Backend
- Enable query caching (Redis integration ready)
- Optimize database connection pooling
- Implement request rate limiting
- Use gzip compression

### Frontend
- Enable static file caching
- Code splitting for React components
- Optimize Monaco editor loading

### Database
- Create indexes on frequently queried columns
- Analyze query execution plans
- Archive old records if needed
