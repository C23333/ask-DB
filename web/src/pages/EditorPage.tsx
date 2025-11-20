import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ChatMessage,
  MonitorDashboard,
  MonitorEvent,
  MonitorStat,
  MonitorTrendPoint,
  SQLExecuteResponse,
  SQLHistoryRecord,
  TemplateInfo,
} from '../services/api';
import apiClient from '../services/api';
import './EditorPage.css';

interface EditorPageProps {
  onLogout?: () => void;
}

interface SessionEntry {
  id: string;
  updatedAt: string;
}

export const EditorPage: React.FC<EditorPageProps> = ({ onLogout }) => {
  const user = apiClient.getCurrentUser();
  const [dbInfo, setDbInfo] = useState<Record<string, any> | null>(null);
  const [query, setQuery] = useState('');
  const [sql, setSQL] = useState('');
  const [reasoning, setReasoning] = useState('');
  const [result, setResult] = useState<SQLExecuteResponse | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [executePage, setExecutePage] = useState(1);
  const [executePageSize, setExecutePageSize] = useState(50);
  const [history, setHistory] = useState<SQLHistoryRecord[]>([]);
  const [templates, setTemplates] = useState<TemplateInfo[]>([]);
  const [sessions, setSessions] = useState<SessionEntry[]>([]);
  const [activeSession, setActiveSession] = useState<string>(
    () => localStorage.getItem('chat_session') || user?.id || 'default'
  );
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);
  const [progressModalOpen, setProgressModalOpen] = useState(false);
  const [progressLogs, setProgressLogs] = useState<string[]>([]);
  const [chatKeyword, setChatKeyword] = useState('');
  const [monitorOverview, setMonitorOverview] = useState<MonitorDashboard | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const streamingMessageIdRef = useRef<string | null>(null);
  const chatLogRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const init = async () => {
      try {
        const info = await apiClient.getDatabaseInfo();
        setDbInfo(info);
      } catch (err) {
        console.warn('Failed to load DB info', err);
      }
      loadChatSessions();
      loadTemplates();
      loadHistory();
    };
    init();
  }, []);

  useEffect(() => {
    loadMonitorStats();
    const timer = window.setInterval(() => {
      loadMonitorStats();
    }, 30000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (activeSession) {
      loadChatMessages(activeSession, chatKeyword);
      localStorage.setItem('chat_session', activeSession);
    }
  }, [activeSession]);

  useEffect(() => {
    if (chatLogRef.current) {
      chatLogRef.current.scrollTop = chatLogRef.current.scrollHeight;
    }
  }, [messages]);

  useEffect(() => {
    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  const stageLabel = (stage: string) => {
    const map: Record<string, string> = {
      received: '已接收请求',
      template_matched: '命中模版',
      prepare_context: '准备上下文',
      memory_loaded: '加载记忆',
      llm_call: 'LLM 生成中',
      completed: '完成',
      failed: '失败',
    };
    return map[stage] || stage;
  };

  const handlePageChange = async (direction: number) => {
    const nextPage = Math.max(1, executePage + direction);
    await executeSQLWithPagination(nextPage, executePageSize);
  };

  const handleRefreshPage = async () => {
    await executeSQLWithPagination(executePage, executePageSize);
  };

  const handlePageSizeChange = async (size: number) => {
    setExecutePageSize(size);
    if (result) {
      await executeSQLWithPagination(1, size);
    }
  };

  const appendProgressLog = (stage: string, message: string) => {
    const entry = `[${new Date().toLocaleTimeString()}] ${stage} - ${message}`;
    setProgressLogs((prev) => [...prev, entry]);
  };

  const loadChatSessions = async () => {
    try {
      const data = await apiClient.getChatSessions();
      const entries = Object.entries(data || {}).map(([id, ts]) => ({
        id,
        updatedAt: ts,
      }));
      entries.sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : -1));
      setSessions(entries);
      if (entries.length > 0 && !entries.find((s) => s.id === activeSession)) {
        setActiveSession(entries[0].id);
      }
    } catch (err) {
      console.warn('Failed to load chat sessions', err);
    }
  };

  const loadChatMessages = async (sessionId: string, keywordParam?: string) => {
    try {
      const params = keywordParam?.trim()
        ? {
            keyword: keywordParam.trim(),
          }
        : undefined;
      const data = await apiClient.getChatMessages(sessionId, params);
      setMessages(data || []);
    } catch (err) {
      console.warn('Failed to load chat messages', err);
    }
  };

  const loadHistory = async () => {
    try {
      const records = await apiClient.getHistory();
      setHistory(records);
    } catch (err) {
      console.warn('Failed to load history', err);
    }
  };

  const loadTemplates = async () => {
    try {
      const list = await apiClient.getTemplates();
      setTemplates(list);
    } catch (err) {
      console.warn('Failed to load templates', err);
    }
  };

  const loadMonitorStats = async () => {
    try {
      const data = await apiClient.getMonitorStats();
      setMonitorOverview(data || null);
    } catch (err) {
      console.warn('Failed to load monitor stats', err);
    }
  };

  const scrollToBottom = () => {
    if (chatLogRef.current) {
      chatLogRef.current.scrollTop = chatLogRef.current.scrollHeight;
    }
  };

  const appendLocalMessage = (role: string, content: string) => {
    const message: ChatMessage = {
      id: crypto.randomUUID(),
      user_id: user?.id || 'local',
      session_id: activeSession,
      role,
      content,
      created_at: new Date().toISOString(),
    };
    setMessages((prev) => [...prev, message]);
    scrollToBottom();
    return message.id;
  };

  const handleGenerateSQL = async () => {
    const trimmed = query.trim();
    if (!trimmed) return;
    setError('');
    setResult(null);
    setReasoning('');

    appendLocalMessage('user', trimmed);
    setQuery('');
    setLoading(true);
    setIsStreaming(true);
    setProgressModalOpen(true);
    setProgressLogs([`[${new Date().toLocaleTimeString()}] 等待服务器响应...`]);

    const requestId = crypto.randomUUID();
    try {
      const ws = apiClient.openSQLStream(
        {
          query: trimmed,
          session_id: activeSession,
          request_id: requestId,
        },
        {
          onProgress: (stage, message) => appendProgressLog(stageLabel(stage), message),
          onChunk: (chunk) => {
            setMessages((prev) => {
              let msgId = streamingMessageIdRef.current;
              if (!msgId) {
                msgId = crypto.randomUUID();
                streamingMessageIdRef.current = msgId;
                return [
                  ...prev,
                  {
                    id: msgId,
                    user_id: user?.id || 'assistant',
                    session_id: activeSession,
                    role: 'assistant',
                    content: chunk,
                    created_at: new Date().toISOString(),
                  },
                ];
              }
              return prev.map((msg) =>
                msg.id === msgId ? { ...msg, content: msg.content + chunk } : msg
              );
            });
          },
          onComplete: (payload) => {
            const finalText = `${payload.sql}\n\n说明:\n${payload.reasoning || ''}`;
            setMessages((prev) => {
              let msgId = streamingMessageIdRef.current;
              if (!msgId) {
                msgId = crypto.randomUUID();
                streamingMessageIdRef.current = msgId;
                return [
                  ...prev,
                  {
                    id: msgId,
                    user_id: user?.id || 'assistant',
                    session_id: activeSession,
                    role: 'assistant',
                    content: finalText,
                    created_at: new Date().toISOString(),
                  },
                ];
              }
              return prev.map((msg) =>
                msg.id === msgId ? { ...msg, content: finalText } : msg
              );
            });
            setSQL(payload.sql);
            setReasoning(payload.reasoning || '');
            setExecutePage(1);
            setResult(null);
            setProgressModalOpen(false);
            setIsStreaming(false);
            setLoading(false);
            streamingMessageIdRef.current = null;
            loadChatSessions();
            loadHistory();
          },
          onError: (msg) => {
            setError(msg);
            setProgressModalOpen(false);
            setIsStreaming(false);
            setLoading(false);
            streamingMessageIdRef.current = null;
          },
          onClose: () => {
            setIsStreaming(false);
            setProgressModalOpen(false);
          },
        }
      );
      wsRef.current = ws;
    } catch (err: any) {
      setError(err?.message || '生成失败');
      setIsStreaming(false);
      setLoading(false);
      setProgressModalOpen(false);
    }
  };

  const executeSQLWithPagination = async (targetPage: number, targetSize: number) => {
    if (!sql.trim()) return;
    setError('');
    setLoading(true);
    try {
      const data = await apiClient.executeSQL({
        sql: sql.trim(),
        timeout: 60,
        page: targetPage,
        page_size: targetSize,
      });
      setResult(data);
      setExecutePage(targetPage);
    } catch (err: any) {
      setError(err?.response?.data?.message || '执行SQL失败');
    } finally {
      setLoading(false);
    }
  };

  const handleExecuteSQL = async () => {
    setExecutePage(1);
    await executeSQLWithPagination(1, executePageSize);
  };

  const handleQuerySubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!isStreaming) {
      handleGenerateSQL();
    }
  };

  const handleCreateSession = () => {
    const name = window.prompt('请输入新的会话 ID（建议使用英文/数字）', 'session_' + Date.now());
    if (!name) return;
    setSessions((prev) => [{ id: name, updatedAt: new Date().toISOString() }, ...prev]);
    setActiveSession(name);
    setMessages([]);
  };

  const handleSaveReport = async () => {
    if (!sql.trim()) {
      setError('当前没有可保存的 SQL');
      return;
    }
    const title = window.prompt('报表标题', `报表-${new Date().toLocaleString()}`);
    if (!title) return;
    const description = window.prompt('报表描述（可选）', '');
    try {
      await apiClient.saveSQL({
        sql: sql.trim(),
        title: title.trim(),
        description: description?.trim() || undefined,
        session_id: activeSession,
      });
      loadHistory();
    } catch (err: any) {
      setError(err?.response?.data?.message || '保存失败');
    }
  };

  const handleLoadReport = (record: SQLHistoryRecord) => {
    setSQL(record.sql);
    setReasoning(record.description || '');
    setExecutePage(1);
    setResult(null);
  };

  const handleDeleteReport = async (id: string) => {
    if (!window.confirm('确认删除该报表？')) return;
    try {
      await apiClient.deleteHistory(id);
      setHistory((prev) => prev.filter((item) => item.id !== id));
    } catch (err: any) {
      setError(err?.response?.data?.message || '删除失败');
    }
  };

  const handleApplyTemplate = (tpl: TemplateInfo) => {
    setQuery(tpl.description || tpl.name);
    setSQL(tpl.sql);
    setReasoning(`模版：${tpl.name}`);
    setExecutePage(1);
    setResult(null);
  };

  const handleSaveTemplateFromSQL = async () => {
    if (!sql.trim()) {
      setError('当前没有可保存的 SQL');
      return;
    }
    const name = window.prompt('模版名称', `模版-${new Date().toLocaleString()}`);
    if (!name) return;
    const desc = window.prompt('模版描述（可选）', reasoning || '');
    const keywordsInput = window.prompt('关键词（逗号分隔）', '门店,报表');
    const keywords = keywordsInput
      ? keywordsInput.split(',').map((k) => k.trim()).filter(Boolean)
      : [];
    try {
      const updated = await apiClient.createTemplate({
        name,
        description: desc || '',
        keywords,
        sql,
        parameters: {},
      });
      setTemplates(updated);
    } catch (err: any) {
      setError(err?.response?.data?.message || '保存模版失败');
    }
  };

  const handleEditTemplate = async (tpl: TemplateInfo) => {
    const name = window.prompt('模版名称', tpl.name);
    if (!name) return;
    const desc = window.prompt('模版描述', tpl.description || '') || '';
    const keywordsInput = window.prompt(
      '关键词（逗号分隔）',
      tpl.keywords?.join(',') || ''
    );
    const keywords = keywordsInput
      ? keywordsInput.split(',').map((k) => k.trim()).filter(Boolean)
      : [];
    const sqlText = window.prompt('模版 SQL', tpl.sql);
    if (!sqlText) return;
    try {
      const updated = await apiClient.updateTemplate(tpl.id, {
        name,
        description: desc,
        keywords,
        sql: sqlText,
      });
      setTemplates(updated);
    } catch (err: any) {
      setError(err?.response?.data?.message || '更新模版失败');
    }
  };

  const handleDeleteTemplate = async (id: string) => {
    if (!window.confirm('确认删除该模版？')) return;
    try {
      const updated = await apiClient.deleteTemplate(id);
      setTemplates(updated);
    } catch (err: any) {
      setError(err?.response?.data?.message || '删除模版失败');
    }
  };

  const handleChatSearch = () => {
    if (activeSession) {
      loadChatMessages(activeSession, chatKeyword);
    }
  };

  const handleExportChat = async () => {
    if (!activeSession) return;
    try {
      const content = await apiClient.exportChat(activeSession);
      const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `chat_${activeSession}_${Date.now()}.txt`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);
    } catch (err: any) {
      setError(err?.response?.data?.message || '导出失败');
    }
  };

  const handleMonitorRefresh = () => {
    loadMonitorStats();
  };

  const sessionEntries = useMemo(() => sessions, [sessions]);
  const rows = result?.rows ?? [];
  const columns = result?.columns ?? [];
  const currentResultPage = result?.page ?? executePage;
  const currentPageSize = result?.page_size ?? executePageSize;
  const currentHasMore = !!result?.has_more;
  const maskedColumns = result?.masked_columns ?? [];
  const monitorTrend: MonitorTrendPoint[] = monitorOverview?.trend ?? [];
  const monitorStatsData = monitorOverview?.stats ?? [];
  const monitorRecent: MonitorEvent[] = monitorOverview?.recent ?? [];
  const monitorWindowHours = monitorOverview?.summary?.window_hours ?? 0;
  const monitorSuccessRate = ((monitorOverview?.summary?.success_rate ?? 0) * 100).toFixed(1);
  const monitorAvgDuration = Math.round(monitorOverview?.summary?.avg_duration_ms ?? 0);
  const monitorAlerting = monitorOverview?.summary?.alerting_enabled ?? false;
  const monitorTotal = monitorOverview?.summary?.total ?? 0;
  const monitorSuccess = monitorOverview?.summary?.success ?? 0;
  const monitorFail = monitorOverview?.summary?.fail ?? 0;
  const monitorSparkline = useMemo<{
    path: string;
    max: number;
    last: number;
    startLabel: string;
    endLabel: string;
  }>(() => {
    if (!monitorTrend.length) {
      return {
        path: '',
        max: 0,
        last: 0,
        startLabel: '',
        endLabel: '',
      };
    }
    const totals = monitorTrend.map((item) => item.total_count || 0);
    const max = Math.max(...totals, 1);
    const last = totals[totals.length - 1] || 0;
    const denominator = Math.max(monitorTrend.length - 1, 1);
    const pointStrings = monitorTrend.map((_, idx) => {
      const x = (idx / denominator) * 100;
      const y = 100 - (totals[idx] / max) * 100;
      return `${x},${Number.isFinite(y) ? y : 100}`;
    });
    if (pointStrings.length === 1) {
      pointStrings.push('100,100');
    }
    const startDate = new Date(monitorTrend[0].bucket);
    const endDate = new Date(monitorTrend[monitorTrend.length - 1].bucket);
    const startLabel = Number.isNaN(startDate.getTime()) ? '' : startDate.toLocaleTimeString();
    const endLabel = Number.isNaN(endDate.getTime()) ? '' : endDate.toLocaleTimeString();
    return {
      path: pointStrings.join(' '),
      max,
      last,
      startLabel,
      endLabel,
    };
  }, [monitorTrend]);

  return (
    <div className="chat-shell">
      <header className="zen-header">
        <div>
          <h1>DB 聊天助手</h1>
          <p>与数据库对话，实时生成 SQL</p>
        </div>
        <div className="zen-header-meta">
          {dbInfo?.database_version && <span className="zen-chip">{dbInfo.database_version}</span>}
          {dbInfo?.current_user && <span className="zen-chip">Schema: {dbInfo.current_user}</span>}
          <div className="zen-user-info">
            <span>{user?.username || '未登录'}</span>
            <button className="zen-link" onClick={() => onLogout?.()}>
              退出
            </button>
          </div>
        </div>
      </header>

      <div className="chat-layout">
        <aside className="chat-sessions">
          <div className="chat-sessions__header">
            <h3>会话</h3>
            <button className="zen-btn zen-btn-secondary" onClick={handleCreateSession}>
              新建
            </button>
          </div>
          <div className="chat-sessions__list">
            {sessionEntries.length === 0 && <p className="zen-note">暂无会话，开始一条对话吧。</p>}
            {sessionEntries.map((session) => (
              <button
                key={session.id}
                className={`chat-session-item ${session.id === activeSession ? 'active' : ''}`}
                onClick={() => setActiveSession(session.id)}
              >
                <span>{session.id}</span>
                <small>{new Date(session.updatedAt).toLocaleString()}</small>
              </button>
            ))}
          </div>
        </aside>

        <main className="chat-main">
          {monitorOverview && (
            <section className="zen-section monitor-panel">
              <div className="monitor-panel__header">
                <div className="zen-section-title">
                  <h2>运行监控</h2>
                  <p>
                    最近 {monitorWindowHours.toFixed(1)} 小时 · 成功率 {monitorSuccessRate}%
                  </p>
                </div>
                <button
                  type="button"
                  className="zen-btn zen-btn-secondary"
                  onClick={handleMonitorRefresh}
                >
                  刷新监控
                </button>
              </div>
              <div className="monitor-summary">
                <div className="monitor-card">
                  <span>请求总量</span>
                  <strong>{monitorTotal}</strong>
                  <small>成功 {monitorSuccess}</small>
                </div>
                <div className="monitor-card">
                  <span>失败次数</span>
                  <strong>{monitorFail}</strong>
                  <small>
                    失败率{' '}
                    {monitorTotal ? ((monitorFail / monitorTotal) * 100).toFixed(1) : '0.0'}%
                  </small>
                </div>
                <div className="monitor-card">
                  <span>平均耗时</span>
                  <strong>{monitorAvgDuration}ms</strong>
                  <small>最近 {monitorSparkline.last} 次请求</small>
                </div>
                <div
                  className={`monitor-card ${
                    monitorAlerting ? 'monitor-card--ok' : 'monitor-card--warn'
                  }`}
                >
                  <span>邮件报警</span>
                  <strong>{monitorAlerting ? '已开启' : '未开启'}</strong>
                  <small>{monitorAlerting ? '失败自动通知' : '请配置 EMAIL_SMTP_*'}</small>
                </div>
              </div>
              <div className="monitor-chart">
                <div className="monitor-chart__title">
                  <strong>调用趋势</strong>
                  <span>
                    {monitorSparkline.startLabel} {monitorSparkline.startLabel && '→'}{' '}
                    {monitorSparkline.endLabel}
                  </span>
                </div>
                {monitorSparkline.path ? (
                  <div className="monitor-chart__sparkline">
                    <svg viewBox="0 0 100 100" preserveAspectRatio="none">
                      <polyline
                        points={monitorSparkline.path}
                        fill="none"
                        stroke="#2563eb"
                        strokeWidth={3}
                        strokeLinejoin="round"
                        strokeLinecap="round"
                      />
                    </svg>
                    <div className="monitor-chart__legend">
                      <span>峰值 {monitorSparkline.max}</span>
                      <span>当前 {monitorSparkline.last}</span>
                    </div>
                  </div>
                ) : (
                  <p className="zen-note">暂无趋势数据</p>
                )}
              </div>
              <div className="monitor-split">
                <div className="monitor-table">
                  <table>
                    <thead>
                      <tr>
                        <th>事件</th>
                        <th>次数</th>
                        <th>成功率</th>
                        <th>耗时</th>
                        <th>最后错误</th>
                      </tr>
                    </thead>
                    <tbody>
                      {monitorStatsData.length === 0 && (
                        <tr>
                          <td colSpan={5} className="zen-note">
                            暂无监控数据
                          </td>
                        </tr>
                      )}
                      {monitorStatsData.map((stat: MonitorStat) => {
                        const rate =
                          stat.count > 0
                            ? ((stat.success_count / stat.count) * 100).toFixed(1)
                            : '0.0';
                        return (
                          <tr key={stat.event_type}>
                            <td>{stat.event_type}</td>
                            <td>{stat.count}</td>
                            <td>{rate}%</td>
                            <td>
                              {Math.round(stat.avg_duration_ms)}ms · P95{' '}
                              {Math.round(stat.p95_duration_ms || 0)}ms
                            </td>
                            <td>
                              {stat.last_error ? (
                                <span className="monitor-table__error">{stat.last_error}</span>
                              ) : (
                                '—'
                              )}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                <div className="monitor-recent">
                  <div className="monitor-recent__header">
                    <h3>最新事件</h3>
                  </div>
                  {monitorRecent.length === 0 ? (
                    <p className="zen-note">暂无记录</p>
                  ) : (
                    <ul>
                      {monitorRecent.slice(0, 5).map((event) => (
                        <li
                          key={event.id}
                          className={`monitor-recent__item ${event.success ? 'ok' : 'fail'}`}
                        >
                          <div>
                            <strong>{event.event_type}</strong>
                            <p>
                              {new Date(event.created_at).toLocaleTimeString()} · {event.duration_ms}
                              ms
                            </p>
                          </div>
                          <span>{event.success ? '成功' : '失败'}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              </div>
            </section>
          )}

          <div className="chat-toolbar">
            <input
              type="text"
              value={chatKeyword}
              placeholder="按关键字搜索当前会话"
              onChange={(e) => setChatKeyword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  handleChatSearch();
                }
              }}
            />
            <button
              type="button"
              className="zen-btn zen-btn-secondary"
              onClick={handleChatSearch}
              disabled={!activeSession}
            >
              搜索
            </button>
            <button
              type="button"
              className="zen-btn zen-btn-secondary"
              onClick={handleExportChat}
              disabled={!activeSession}
            >
              导出
            </button>
          </div>

          <div className="chat-log" ref={chatLogRef}>
            {messages.length === 0 && <p className="zen-note">暂无历史记录，试着提问 “最近30天门店...”</p>}
            {messages.map((msg) => (
              <div
                key={msg.id}
                className={`chat-message chat-message--${msg.role === 'user' ? 'user' : 'assistant'}`}
              >
                <div className="chat-message__role">
                  {msg.role === 'user' ? '我' : 'DB 助理'} · {new Date(msg.created_at).toLocaleTimeString()}
                </div>
                <pre className="chat-message__content">{msg.content}</pre>
              </div>
            ))}
          </div>

          <form className="chat-input" onSubmit={handleQuerySubmit}>
            <textarea
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="向数据库提问，例如：列出最近30天新增门店..."
              rows={3}
            />
            <div className="chat-input__actions">
              <button type="submit" className="zen-btn" disabled={!query.trim() || loading || isStreaming}>
                {loading || isStreaming ? '生成中...' : '发送'}
              </button>
              <button
                type="button"
                className="zen-btn zen-btn-secondary"
                onClick={() => setQuery('')}
                disabled={loading || isStreaming}
              >
                清空
              </button>
            </div>
          </form>

          {error && <div className="zen-error">{error}</div>}

          <section className="zen-section">
            <div className="zen-section-title">
              <h2>SQL 草稿</h2>
              {reasoning && <p className="zen-note">{reasoning}</p>}
            </div>
            <textarea
              className="zen-textarea zen-textarea-sql"
              value={sql}
              onChange={(e) => setSQL(e.target.value)}
              rows={10}
            />
            <div className="zen-actions">
              <button className="zen-btn" onClick={handleExecuteSQL} disabled={!sql.trim() || loading}>
                执行 SQL
              </button>
              <button className="zen-btn zen-btn-secondary" onClick={handleSaveReport}>
                保存报表
              </button>
              <button className="zen-btn zen-btn-secondary" onClick={handleSaveTemplateFromSQL}>
                保存为模版
              </button>
            </div>
          </section>

          {result && (
            <section className="zen-section">
              <div className="zen-section-title">
                <h2>查询结果</h2>
                {result.success && (
                  <span className="zen-chip">
                    本页 {result.row_count} 行 · {result.exec_time_ms}ms
                  </span>
                )}
              </div>
              {result.success && (
                <div className="pagination-bar">
                  <div className="pagination-info">
                    <span>
                      第 {currentResultPage} 页 · 每页 {currentPageSize} 行
                    </span>
                    {maskedColumns.length > 0 && (
                      <span className="masked-note">敏感列已脱敏：{maskedColumns.join(', ')}</span>
                    )}
                  </div>
                  <div className="pagination-actions">
                    <label>每页</label>
                    <select
                      value={currentPageSize}
                      onChange={(e) => handlePageSizeChange(Number(e.target.value))}
                      disabled={loading}
                    >
                      {[20, 50, 100, 200].map((size) => (
                        <option key={size} value={size}>
                          {size}
                        </option>
                      ))}
                    </select>
                    <button
                      type="button"
                      className="zen-btn zen-btn-secondary"
                      onClick={() => handlePageChange(-1)}
                      disabled={loading || currentResultPage <= 1}
                    >
                      上一页
                    </button>
                    <button
                      type="button"
                      className="zen-btn zen-btn-secondary"
                      onClick={handleRefreshPage}
                      disabled={loading}
                    >
                      刷新
                    </button>
                    <button
                      type="button"
                      className="zen-btn zen-btn-secondary"
                      onClick={() => handlePageChange(1)}
                      disabled={loading || !currentHasMore}
                    >
                      下一页
                    </button>
                  </div>
                </div>
              )}
              <div className="zen-result">
                {result.success ? (
                  <table className="zen-table">
                    <thead>
                      <tr>
                        {columns.map((col) => (
                          <th key={col}>{col}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {rows.map((row, idx) => (
                        <tr key={idx}>
                          {columns.map((col, j) => (
                            <td key={col + j}>{row[j] !== undefined ? String(row[j]) : ''}</td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <div className="zen-error">{result.error || '执行失败'}</div>
                )}
              </div>
            </section>
          )}

          <section className="zen-section">
            <div className="zen-section-title">
              <h2>模版库</h2>
            </div>
            {templates.length === 0 ? (
              <p className="zen-note">暂无模版</p>
            ) : (
              <ul className="zen-list">
                {templates.map((tpl) => (
                  <li key={tpl.id} className="zen-list-item">
                    <div>
                      <strong>{tpl.name}</strong>
                      <p className="zen-note">{tpl.description}</p>
                      {tpl.is_system && <p className="zen-note">系统模版</p>}
                    </div>
                    <button className="zen-btn zen-btn-secondary" onClick={() => handleApplyTemplate(tpl)}>
                      应用
                    </button>
                    {tpl.editable ? (
                      <div className="zen-list-actions">
                        <button className="zen-btn zen-btn-secondary" onClick={() => handleEditTemplate(tpl)}>
                          编辑
                        </button>
                        <button className="zen-btn zen-btn-text" onClick={() => handleDeleteTemplate(tpl.id)}>
                          删除
                        </button>
                      </div>
                    ) : (
                      <span className="zen-note">只读</span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="zen-section">
            <div className="zen-section-title">
              <h2>个人报表</h2>
            </div>
            {history.length === 0 ? (
              <p className="zen-note">暂无保存记录</p>
            ) : (
              <ul className="zen-list">
                {history.map((item) => (
                  <li key={item.id} className="zen-list-item">
                    <div>
                      <strong>{item.title}</strong>
                      <p className="zen-note">
                        {new Date(item.updated_at).toLocaleString()} · {item.description || '无描述'}
                      </p>
                    </div>
                    <div className="zen-list-actions">
                      <button className="zen-btn zen-btn-secondary" onClick={() => handleLoadReport(item)}>
                        载入
                      </button>
                      <button className="zen-btn zen-btn-text" onClick={() => handleDeleteReport(item.id)}>
                        删除
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </main>
      </div>

      {progressModalOpen && (
        <div className="zen-modal">
          <div className="zen-modal-content">
            <div className="zen-spinner" />
            <h3>正在生成 SQL</h3>
            <div className="zen-progress-log">
              {progressLogs.map((log, idx) => (
                <div key={idx}>{log}</div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
