import { create } from 'zustand';
import api, { SQLHistoryRecord, SQLExecuteResponse } from '../services/api';

interface SQLStore {
  // Editor state
  sqlQuery: string;
  setSQLQuery: (query: string) => void;

  // Execution state
  isExecuting: boolean;
  executionResult: SQLExecuteResponse | null;
  executionError: string | null;
  executeSQL: (timeout?: number) => Promise<void>;

  // Generation state
  isGenerating: boolean;
  generatedSQL: string | null;
  generationError: string | null;
  generateSQL: (naturalLanguageQuery: string, context?: string) => Promise<void>;

  // Debugging state
  isDebugging: boolean;
  debugSuggestions: any | null;
  debugError: string | null;
  debugSQL: (error: string) => Promise<void>;

  // History
  history: SQLHistoryRecord[];
  loadHistory: () => Promise<void>;
  saveSQL: (title: string) => Promise<void>;

  // UI state
  activeTab: 'editor' | 'results' | 'history';
  setActiveTab: (tab: 'editor' | 'results' | 'history') => void;

  // Clear state
  clearResults: () => void;
  clearError: () => void;
}

export const useSQLStore = create<SQLStore>((set, get) => ({
  sqlQuery: '',
  setSQLQuery: (query: string) => set({ sqlQuery: query }),

  isExecuting: false,
  executionResult: null,
  executionError: null,
  executeSQL: async (timeout?: number) => {
    const { sqlQuery } = get();
    if (!sqlQuery.trim()) {
      set({ executionError: 'Please enter a SQL query' });
      return;
    }

    set({ isExecuting: true, executionError: null, executionResult: null });
    try {
      const result = await api.executeSQL({
        sql: sqlQuery,
        timeout: timeout || 30,
      });
      set({ executionResult: result, activeTab: 'results' });
    } catch (error: any) {
      set({
        executionError: error.response?.data?.message || 'Execution failed',
      });
    } finally {
      set({ isExecuting: false });
    }
  },

  isGenerating: false,
  generatedSQL: null,
  generationError: null,
  generateSQL: async (naturalLanguageQuery: string, context?: string) => {
    if (!naturalLanguageQuery.trim()) {
      set({ generationError: 'Please enter a query' });
      return;
    }

    set({ isGenerating: true, generationError: null, generatedSQL: null });
    try {
      const result = await api.generateSQL({
        query: naturalLanguageQuery,
        context,
      });
      set({ generatedSQL: result.sql, sqlQuery: result.sql });
    } catch (error: any) {
      set({
        generationError: error.response?.data?.message || 'Generation failed',
      });
    } finally {
      set({ isGenerating: false });
    }
  },

  isDebugging: false,
  debugSuggestions: null,
  debugError: null,
  debugSQL: async (error: string) => {
    const { sqlQuery } = get();
    if (!sqlQuery.trim()) {
      set({ debugError: 'Please enter a SQL query' });
      return;
    }

    set({ isDebugging: true, debugError: null, debugSuggestions: null });
    try {
      const result = await api.debugSQL({
        sql: sqlQuery,
        error,
      });
      set({ debugSuggestions: result });
      if (result.suggested_sql) {
        set({ generatedSQL: result.suggested_sql });
      }
    } catch (error: any) {
      set({
        debugError: error.response?.data?.message || 'Debug failed',
      });
    } finally {
      set({ isDebugging: false });
    }
  },

  history: [],
  loadHistory: async () => {
    try {
      const records = await api.getHistory();
      set({ history: records });
    } catch (error) {
      console.error('Failed to load history', error);
    }
  },

  saveSQL: async (title: string) => {
    const { sqlQuery } = get();
    if (!sqlQuery.trim()) {
      set({ executionError: 'Please enter a SQL query' });
      return;
    }

    try {
      const record = await api.saveSQL({ sql: sqlQuery, title });
      set((state) => ({
        history: [record, ...state.history],
      }));
    } catch (error: any) {
      set({
        executionError: error.response?.data?.message || 'Save failed',
      });
    }
  },

  activeTab: 'editor',
  setActiveTab: (tab: 'editor' | 'results' | 'history') => {
    set({ activeTab: tab });
  },

  clearResults: () => {
    set({
      executionResult: null,
      executionError: null,
      generatedSQL: null,
      generationError: null,
      debugSuggestions: null,
      debugError: null,
    });
  },

  clearError: () => {
    set({
      executionError: null,
      generationError: null,
      debugError: null,
    });
  },
}));
