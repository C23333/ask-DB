import { useEffect, useState } from 'react';
import { LoginPage } from './pages/LoginPage';
import { EditorPage } from './pages/EditorPage';
import apiClient from './services/api';
import './App.css';

function App() {
  const [isAuthenticated, setIsAuthenticated] = useState(apiClient.isAuthenticated());

  useEffect(() => {
    const handleAuthLogout = () => setIsAuthenticated(false);
    window.addEventListener('auth:logout' as any, handleAuthLogout as EventListener);
    return () =>
      window.removeEventListener('auth:logout' as any, handleAuthLogout as EventListener);
  }, []);

  const handleLoginSuccess = (query?: string) => {
    if (query) {
      sessionStorage.setItem('initialQuery', query);
    }
    setIsAuthenticated(true);
  };

  const handleLogout = () => {
    apiClient.logout();
    setIsAuthenticated(false);
  };

  return (
    <div className="app">
      {isAuthenticated ? (
        <EditorPage onLogout={handleLogout} />
      ) : (
        <LoginPage onSuccess={handleLoginSuccess} />
      )}
    </div>
  );
}

export default App;
