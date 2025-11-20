import axios, { AxiosInstance } from 'axios';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api';
const WS_BASE_URL =
  import.meta.env.VITE_WS_URL ||
  `${API_BASE_URL.replace(/^http/i, (match: string) =>
    match.toLowerCase() === 'https' ? 'wss' : 'ws'
  ).replace(/\/$/, '')}/ws`;

interface LoginResponse {
  token: string;
  expires_in: number;
  user: {
    id: string;
    username: string;
    email: string;
  };
}

interface SQLGenerateRequest {
  query: string;
  context?: string;
  table_names?: string;
  session_id?: string;
  request_id?: string;
}

interface SQLGenerateResponse {
  sql: string;
  reasoning: string;
  source?: 'llm' | 'template';
  template_id?: string;
  used_memory?: boolean;
  request_id?: string;
}

interface SQLExecuteRequest {
  sql: string;
  timeout?: number;
  page?: number;
  page_size?: number;
}

interface SQLExecuteResponse {
  success: boolean;
  columns: string[];
  rows: any[][];
  row_count: number;
  exec_time_ms: number;
  error?: string;
  page?: number;
  page_size?: number;
  has_more?: boolean;
  masked_columns?: string[];
}

interface SQLDebugRequest {
  sql: string;
  error: string;
}

interface SQLDebugResponse {
  analysis_text: string;
  suggested_sql: string;
  explanation: string;
}

interface SQLHistoryRecord {
  id: string;
  user_id: string;
  sql: string;
  title: string;
  description?: string;
  saved: boolean;
  last_run: string;
  created_at: string;
  updated_at: string;
  template_id?: string;
  parameters?: Record<string, string>;
  session_id?: string;
  source?: string;
}

interface TemplateInfo {
  id: string;
  name: string;
  description: string;
  keywords: string[];
  sql: string;
  parameters?: Record<string, string>;
  owner_id?: string;
  editable?: boolean;
  is_system?: boolean;
}

interface TemplatePayload {
  name: string;
  description?: string;
  keywords?: string[];
  sql: string;
  parameters?: Record<string, string>;
}

interface SessionMap {
  [sessionId: string]: string;
}

interface SaveSQLRequest {
  sql: string;
  title: string;
  description?: string;
  template_id?: string;
  session_id?: string;
  parameters?: Record<string, string>;
}

interface GenerationStatus {
  id: string;
  stage: string;
  message: string;
  done: boolean;
  success: boolean;
  error?: string;
  updated_at: string;
}

type SQLStreamHandlers = {
  onProgress?: (stage: string, message: string) => void;
  onChunk?: (chunk: string) => void;
  onComplete?: (payload: SQLGenerateResponse) => void;
  onError?: (message: string) => void;
  onClose?: () => void;
};

interface ChatMessage {
  id: string;
  user_id: string;
  session_id: string;
  role: string;
  content: string;
  created_at: string;
}

interface MonitorStat {
  event_type: string;
  count: number;
  success_count: number;
  fail_count: number;
  avg_duration_ms: number;
  p95_duration_ms?: number;
  max_duration_ms?: number;
  last_event_at?: string;
  last_error?: string;
}

interface MonitorSummary {
  total: number;
  success: number;
  fail: number;
  success_rate: number;
  avg_duration_ms: number;
  window_hours: number;
  alerting_enabled: boolean;
}

interface MonitorTrendPoint {
  bucket: string;
  total_count: number;
  success_count: number;
  fail_count: number;
  avg_duration_ms: number;
}

interface MonitorEvent {
  id: string;
  event_type: string;
  duration_ms: number;
  success: boolean;
  created_at: string;
  extra?: Record<string, any>;
}

interface MonitorDashboard {
  summary: MonitorSummary;
  stats: MonitorStat[];
  trend: MonitorTrendPoint[];
  recent: MonitorEvent[];
}

interface ChatMessageOptions {
  keyword?: string;
  limit?: number;
}

class APIClient {
  private client: AxiosInstance;
  private token: string | null = null;
  private currentUser: LoginResponse['user'] | null = null;
  private wsUrl: string;

  constructor() {
    this.client = axios.create({
      baseURL: API_BASE_URL,
      timeout: 120000,
    });
    this.wsUrl = WS_BASE_URL;

    // Load token from localStorage if available
    this.token = localStorage.getItem('auth_token');
    if (this.token) {
      this.setAuthHeader(this.token);
    }

    const storedUser = localStorage.getItem('auth_user');
    if (storedUser) {
      try {
        this.currentUser = JSON.parse(storedUser);
      } catch {
        this.currentUser = null;
      }
    }

    // Add response interceptor to handle 401
    this.client.interceptors.response.use(
      (response) => response,
      (error) => {
        if (error.response?.status === 401) {
          this.logout();
        }
        return Promise.reject(error);
      }
    );
  }

  private setAuthHeader(token: string) {
    this.client.defaults.headers.common['Authorization'] = `Bearer ${token}`;
  }

  async register(username: string, email: string, password: string) {
    const response = await this.client.post('/auth/register', {
      username,
      email,
      password,
    });
    return response.data.data;
  }

  async login(username: string, password: string): Promise<LoginResponse> {
    const response = await this.client.post('/auth/login', {
      username,
      password,
    });

    const data = response.data.data;
    this.token = data.token;
    localStorage.setItem('auth_token', data.token);
    this.setAuthHeader(data.token);
    this.currentUser = data.user;
    localStorage.setItem('auth_user', JSON.stringify(data.user));

    return data;
  }

  logout() {
    this.token = null;
    this.currentUser = null;
    localStorage.removeItem('auth_token');
    localStorage.removeItem('auth_user');
    delete this.client.defaults.headers.common['Authorization'];
    window.dispatchEvent(new CustomEvent('auth:logout'));
  }

  isAuthenticated(): boolean {
    return !!this.token;
  }

  getCurrentUser() {
    return this.currentUser;
  }

  async generateSQL(request: SQLGenerateRequest): Promise<SQLGenerateResponse> {
    const response = await this.client.post('/sql/generate', request);
    return response.data.data;
  }

  async executeSQL(request: SQLExecuteRequest): Promise<SQLExecuteResponse> {
    const response = await this.client.post('/sql/execute', request);
    return response.data.data;
  }

  async debugSQL(request: SQLDebugRequest): Promise<SQLDebugResponse> {
    const response = await this.client.post('/sql/debug', request);
    return response.data.data;
  }

  async saveSQL(payload: SaveSQLRequest): Promise<SQLHistoryRecord> {
    const response = await this.client.post('/sql/save', payload);
    return response.data.data;
  }

  async getHistory(): Promise<SQLHistoryRecord[]> {
    const response = await this.client.get('/sql/history');
    return response.data.data || [];
  }

  async deleteHistory(id: string): Promise<void> {
    await this.client.delete(`/sql/history/${id}`);
  }

  async getTemplates(): Promise<TemplateInfo[]> {
    const response = await this.client.get('/templates');
    return response.data.data || [];
  }

  async createTemplate(payload: TemplatePayload): Promise<TemplateInfo[]> {
    const response = await this.client.post('/templates', payload);
    return response.data.data || [];
  }

  async updateTemplate(id: string, payload: TemplatePayload): Promise<TemplateInfo[]> {
    const response = await this.client.put(`/templates/${id}`, payload);
    return response.data.data || [];
  }

  async deleteTemplate(id: string): Promise<TemplateInfo[]> {
    const response = await this.client.delete(`/templates/${id}`);
    return response.data.data || [];
  }

  async getSessions(): Promise<SessionMap> {
    const response = await this.client.get('/sql/sessions');
    return response.data.data || {};
  }

  async getSessionHistory(sessionId: string) {
    const response = await this.client.get(`/sql/sessions/${sessionId}/history`);
    return response.data.data || [];
  }

  async getGenerateStatus(requestId: string): Promise<GenerationStatus | null> {
    try {
      const response = await this.client.get(`/sql/generate/status/${requestId}`);
      return response.data.data;
    } catch (error: any) {
      if (error?.response?.status === 404) {
        return null;
      }
      throw error;
    }
  }

  async getDatabaseInfo() {
    const response = await this.client.get('/database/info');
    return response.data.data;
  }

  openSQLStream(request: SQLGenerateRequest, handlers: SQLStreamHandlers) {
    if (!this.token) {
      throw new Error('Not authenticated');
    }
    const base = `${this.wsUrl.replace(/\/$/, '')}/sql`;
    const url = new URL(base);
    url.searchParams.set('token', this.token);
    const ws = new WebSocket(url.toString());

    ws.onopen = () => {
      ws.send(JSON.stringify(request));
    };

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        switch (msg.type) {
          case 'progress':
            handlers.onProgress?.(msg.stage, msg.message);
            break;
          case 'chunk':
            handlers.onChunk?.(msg.chunk);
            break;
          case 'complete':
            handlers.onComplete?.({
              sql: msg.sql,
              reasoning: msg.reasoning,
              template_id: msg.template_id,
              used_memory: msg.used_memory,
            } as SQLGenerateResponse);
            break;
          case 'error':
            handlers.onError?.(msg.error || '生成失败');
            break;
          default:
            break;
        }
      } catch (err) {
        handlers.onError?.('解析服务端消息失败');
      }
    };

    ws.onerror = () => {
      handlers.onError?.('WebSocket 连接异常');
    };

    ws.onclose = () => {
      handlers.onClose?.();
    };

    return ws;
  }

  async getChatSessions(): Promise<SessionMap> {
    const response = await this.client.get('/chat/sessions');
    return response.data.data || {};
  }

  async getChatMessages(sessionId: string, options?: ChatMessageOptions): Promise<ChatMessage[]> {
    const response = await this.client.get(`/chat/${sessionId}/messages`, {
      params: options,
    });
    return response.data.data || [];
  }

  async exportChat(sessionId: string): Promise<string> {
    const response = await this.client.get<string>(`/chat/${sessionId}/export`, {
      responseType: 'text',
    });
    return response.data;
  }

  async getMonitorStats(): Promise<MonitorDashboard> {
    const response = await this.client.get('/monitor/stats');
    return response.data.data;
  }
}

const apiClient = new APIClient();

export default apiClient;
export { apiClient as services };
export type {
  LoginResponse,
  SQLGenerateRequest,
  SQLGenerateResponse,
  SQLExecuteRequest,
  SQLExecuteResponse,
  SQLDebugRequest,
  SQLDebugResponse,
  SQLHistoryRecord,
  TemplateInfo,
  TemplatePayload,
  SessionMap,
  SaveSQLRequest,
  GenerationStatus,
  ChatMessage,
  MonitorStat,
  MonitorSummary,
  MonitorTrendPoint,
  MonitorEvent,
  MonitorDashboard,
};
