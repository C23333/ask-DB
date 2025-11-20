package progress

import (
	"sync"
	"time"
)

type Entry struct {
	ID        string    `json:"id"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Done      bool      `json:"done"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store 管理生成 SQL 请求的进度
type Store struct {
	mu    sync.RWMutex
	items map[string]*Entry
}

func NewStore() *Store {
	return &Store{items: make(map[string]*Entry)}
}

func (s *Store) Init(id, stage, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = &Entry{
		ID:        id,
		Stage:     stage,
		Message:   message,
		UpdatedAt: time.Now(),
		Done:      false,
		Success:   false,
	}
}

func (s *Store) Update(id, stage, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.items[id]; ok {
		entry.Stage = stage
		entry.Message = message
		entry.UpdatedAt = time.Now()
	}
}

func (s *Store) Complete(id, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.items[id]; ok {
		entry.Stage = "completed"
		entry.Message = message
		entry.Done = true
		entry.Success = true
		entry.UpdatedAt = time.Now()
	}
}

func (s *Store) Fail(id, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.items[id]; ok {
		entry.Stage = "failed"
		entry.Message = "请求失败"
		entry.Error = message
		entry.Done = true
		entry.Success = false
		entry.UpdatedAt = time.Now()
	}
}

func (s *Store) Get(id string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.items[id]
	if !ok {
		return nil, false
	}
	copy := *entry
	return &copy, true
}

func (s *Store) Cleanup(olderThan time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	for id, entry := range s.items {
		if entry.UpdatedAt.Before(cutoff) {
			delete(s.items, id)
		}
	}
}
