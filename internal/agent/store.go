package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store persists sessions to disk.
type Store struct {
	mu   sync.RWMutex
	dir  string
}

// NewStore creates a session store rooted at dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Save writes a session to disk.
func (s *Store) Save(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}
	path := filepath.Join(s.dir, session.ID+".json")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(session); err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	return nil
}

// Load reads a session from disk. Returns nil if not found.
func (s *Store) Load(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.dir, id+".json")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()
	var sess Session
	if err := json.NewDecoder(f).Decode(&sess); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return &sess, nil
}

// List returns all session IDs sorted by creation time (newest first).
func (s *Store) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read store dir: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			ids = append(ids, e.Name()[:len(e.Name())-5])
		}
	}
	return ids, nil
}

// Delete removes a session file from disk.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session file: %w", err)
	}
	return nil
}
