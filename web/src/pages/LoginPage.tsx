import React, { useState } from 'react';
import apiClient from '../services/api';
import './LoginPage.css';

interface LoginPageProps {
  onSuccess?: (initialQuery?: string) => void;
}

type Mode = 'login' | 'register';

export const LoginPage: React.FC<LoginPageProps> = ({ onSuccess }) => {
  const [mode, setMode] = useState<Mode>('login');
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [query, setQuery] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    if (!username.trim() || !password.trim() || (mode === 'register' && !email.trim())) {
      setError('请完整填写信息');
      return;
    }

    setIsLoading(true);
    try {
      if (mode === 'register') {
        await apiClient.register(username.trim(), email.trim(), password);
      }
      await apiClient.login(username.trim(), password);
      onSuccess?.(query.trim() ? query.trim() : undefined);
    } catch (err: any) {
      const message =
        err?.response?.data?.message ||
        (mode === 'register' ? '注册失败，请稍后再试' : '登录失败，请检查账号密码');
      setError(message);
    } finally {
      setIsLoading(false);
    }
  };

  const disableSubmit =
    isLoading ||
    !username.trim() ||
    !password.trim() ||
    (mode === 'register' && !email.trim());

  return (
    <div className="zen-container">
      <div className="zen-card">
        <div className="zen-card-header">
          <h1 className="zen-title">问数据库</h1>
          <p className="zen-subtitle">自然语言 → SQL → 即刻查询</p>
        </div>

        <div className="zen-tabs">
          <button
            type="button"
            className={`zen-tab ${mode === 'login' ? 'active' : ''}`}
            onClick={() => {
              setMode('login');
              setError('');
            }}
          >
            登录
          </button>
          <button
            type="button"
            className={`zen-tab ${mode === 'register' ? 'active' : ''}`}
            onClick={() => {
              setMode('register');
              setError('');
            }}
          >
            注册
          </button>
        </div>

        {error && <div className="zen-error">{error}</div>}

        <form onSubmit={handleSubmit} className="zen-form">
          <label className="zen-label">用户名</label>
          <input
            className="zen-input"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="如：data_reader"
            autoComplete="username"
          />

          {mode === 'register' && (
            <>
              <label className="zen-label">邮箱</label>
              <input
                className="zen-input"
                value={email}
                type="email"
                onChange={(e) => setEmail(e.target.value)}
                placeholder="用于接收通知"
                autoComplete="email"
              />
            </>
          )}

          <label className="zen-label">密码</label>
          <input
            className="zen-input"
            value={password}
            type="password"
            onChange={(e) => setPassword(e.target.value)}
            placeholder="请输入密码"
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          />

          <label className="zen-label zen-label-inline">
            想问什么？
            <span>可选，登录后自动生成SQL</span>
          </label>
          <textarea
            className="zen-textarea"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="例如：统计最近7天每个产品的销售额..."
            rows={4}
          />

          <button type="submit" className="zen-button" disabled={disableSubmit}>
            {isLoading ? '处理中...' : mode === 'login' ? '登录并开始' : '注册并登录'}
          </button>
        </form>

        <div className="zen-hint">
          当前为演示环境，账户数据保存在内存里，重启后需要重新注册。
        </div>
      </div>
    </div>
  );
};
