package agent

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleSystem    = "system"
)

// Message represents a single message in the conversation.
type Message struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolName   string            `json:"tool_name,omitempty"`
	ToolCalls  []ToolCallMessage `json:"tool_calls,omitempty"`
}

// ToolCallMessage is the LLM-format tool call inside an assistant message.
type ToolCallMessage struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Session holds the state for one conversation.
type Session struct {
	mu sync.RWMutex

	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Messages     []Message `json:"messages"`
	CreatedAt    time.Time `json:"created_at"`
	TurnCount    int       `json:"turn_count"`
	Status       string    `json:"status,omitempty"`
	StatusDetail string    `json:"status_detail,omitempty"`
}

// SessionManager manages multiple independent sessions with disk persistence.
type SessionManager struct {
	mu           sync.RWMutex
	sessions     map[string]*Session
	store        *Store
	runLocks     sync.Map // map[string]*sync.Mutex — per-session execution mutex
	busySessions sync.Map // map[string]bool — fast busy check without blocking
}

// TryLockRun attempts to acquire the per-session execution lock.
// Returns a release function and true if acquired. If session is already busy,
// returns nil, false without blocking.
func (sm *SessionManager) TryLockRun(id string) (func(), bool) {
	// Mark busy first (atomic check used by frontend).
	if _, loaded := sm.busySessions.LoadOrStore(id, true); loaded {
		return nil, false
	}
	// Acquire the mutex (should always succeed since we set busy first).
	mu, _ := sm.runLocks.LoadOrStore(id, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	return func() {
		mu.(*sync.Mutex).Unlock()
		sm.busySessions.Delete(id)
	}, true
}

// IsBusy returns true if the session is currently processing a request.
func (sm *SessionManager) IsBusy(id string) bool {
	_, ok := sm.busySessions.Load(id)
	return ok
}

// NewSessionManager creates a new session manager backed by a Store.
func NewSessionManager(store *Store) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		store:    store,
	}
	// Preload existing sessions from disk.
	ids, err := store.List()
	if err != nil {
		log.Printf("Warning: failed to list sessions: %v", err)
		return sm
	}
	for _, id := range ids {
		sess, err := store.Load(id)
		if err != nil {
			log.Printf("Warning: failed to load session %s: %v", id, err)
			continue
		}
		if sess != nil {
			sm.sessions[id] = sess
		}
	}
	return sm
}

// GetOrCreate returns an existing session or creates a new one (persisted).
func (sm *SessionManager) GetOrCreate(id string) *Session {
	// Try read-only lookup first to avoid blocking concurrent sessions.
	sm.mu.RLock()
	s, ok := sm.sessions[id]
	sm.mu.RUnlock()
	if ok {
		return s
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()
	// Double-check after acquiring write lock.
	if s, ok := sm.sessions[id]; ok {
		return s
	}
	// Try loading from disk.
	s, err := sm.store.Load(id)
	if err != nil {
		log.Printf("Warning: failed to load session %s: %v", id, err)
	}
	if s != nil {
		sm.sessions[id] = s
		return s
	}
	// Create new.
	s = &Session{
		ID:        id,
		Messages:  []Message{},
		CreatedAt: time.Now(),
	}
	sm.sessions[id] = s
	if err := sm.store.Save(s); err != nil {
		log.Printf("Warning: failed to persist new session %s: %v", id, err)
	}
	return s
}

// Get returns a session by ID, or nil if not found.
func (sm *SessionManager) Get(id string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[id]
}

// List returns all session IDs sorted by creation time (newest first).
func (sm *SessionManager) List() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	ids := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		ids = append(ids, id)
	}
	// Sort by creation time descending (newest first) for stable order.
	sort.SliceStable(ids, func(i, j int) bool {
		return sm.sessions[ids[i]].CreatedAt.After(sm.sessions[ids[j]].CreatedAt)
	})
	return ids
}

// Save persists a session to disk immediately.
func (sm *SessionManager) Save(id string) error {
	sm.mu.RLock()
	sess, ok := sm.sessions[id]
	sm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	return sm.store.Save(sess)
}

// Delete removes a session.
func (sm *SessionManager) Delete(id string) {
	sm.mu.Lock()
	delete(sm.sessions, id)
	sm.mu.Unlock()
	if err := sm.store.Delete(id); err != nil {
		log.Printf("Warning: failed to delete session file %s: %v", id, err)
	}
}

// AddUserMessage adds a user message to a session and persists.
func (s *Session) AddUserMessage(content string) {
	s.mu.Lock()
	s.Messages = append(s.Messages, Message{Role: RoleUser, Content: content})
	s.TurnCount++
	s.mu.Unlock()
}

// AddAssistantMessage adds an assistant text message.
func (s *Session) AddAssistantMessage(content string) {
	s.mu.Lock()
	s.Messages = append(s.Messages, Message{Role: RoleAssistant, Content: content})
	s.mu.Unlock()
}

// AddAssistantToolCallMessage adds an assistant message containing tool calls.
func (s *Session) AddAssistantToolCallMessage(tcs []ToolCallMessage) {
	s.mu.Lock()
	s.Messages = append(s.Messages, Message{Role: RoleAssistant, Content: "", ToolCalls: tcs})
	s.mu.Unlock()
}

// AddToolResult adds a tool result message.
func (s *Session) AddToolResult(toolCallID, toolName, result string, isError bool) {
	content := result
	if isError {
		content = fmt.Sprintf("Error: %s", result)
	}
	s.mu.Lock()
	s.Messages = append(s.Messages, Message{
		Role:       RoleTool,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Content:    content,
	})
	s.mu.Unlock()
}

// DisplayMsg is a message formatted for the frontend.
type DisplayMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Summary string `json:"summary,omitempty"`
	Type    string `json:"type,omitempty"` // "thinking", "message"
}

// MessagesForDisplay returns messages for the frontend.
// Each turn's tool calls+results are grouped into a single "thinking"
// block whose Content is pre-built HTML with individual collapsible
// <details> elements for each tool step.
func (s *Session) MessagesForDisplay() []DisplayMsg {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []DisplayMsg

	// Accumulate tool step HTML fragments for the current "thinking" block.
	var steps []string

	flushThinking := func() {
		if len(steps) == 0 {
			return
		}
		content := ""
		for _, s := range steps {
			content += s
		}
		out = append(out, DisplayMsg{
			Role:    "assistant",
			Content: content,
			Summary: fmt.Sprintf("思考过程 (%d步)", len(steps)),
			Type:    "thinking",
		})
		steps = nil
	}

	for _, m := range s.Messages {
		switch m.Role {
		case RoleUser:
			flushThinking()
			out = append(out, DisplayMsg{Role: "user", Content: m.Content, Type: "message"})
		case RoleAssistant:
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					steps = append(steps, fmt.Sprintf(
						"<details><summary>🔧 %s</summary><div class=\"step-detail\">参数: %s</div></details>",
						escHTML(tc.Function.Name), escHTML(tc.Function.Arguments)))
				}
			}
			if m.Content != "" {
				flushThinking()
				out = append(out, DisplayMsg{Role: "assistant", Content: m.Content, Type: "message"})
			}
		case RoleTool:
			content := m.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			steps = append(steps, fmt.Sprintf(
				"<details><summary>📎 %s</summary><div class=\"step-detail\">%s</div></details>",
				escHTML(m.ToolName), escHTML(content)))
		}
	}
	flushThinking()
	return out
}

// MessagesToOpenAI converts []Message to OpenAI API format.
func MessagesToOpenAI(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			out = append(out, map[string]any{
				"role":    RoleUser,
				"content": m.Content,
			})
		case RoleAssistant:
			msg := map[string]any{"role": RoleAssistant}
			if len(m.ToolCalls) > 0 {
				if m.Content != "" {
					msg["content"] = m.Content
				}
				tcs := make([]map[string]any, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					tcs[i] = map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Function.Name,
							"arguments": tc.Function.Arguments,
						},
					}
				}
				msg["tool_calls"] = tcs
			} else {
				msg["content"] = m.Content
			}
			out = append(out, msg)
		case RoleTool:
			out = append(out, map[string]any{
				"role":          RoleTool,
				"tool_call_id":  m.ToolCallID,
				"content":       m.Content,
			})
		}
	}
	return out
}

// ToOpenAIMessages converts session messages to OpenAI API format.
func (s *Session) ToOpenAIMessages() []map[string]any {
	return MessagesToOpenAI(s.Messages)
}

// SetStatus safely updates the session status (concurrent-safe).
func (s *Session) SetStatus(status, detail string) {
	s.mu.Lock()
	s.Status = status
	s.StatusDetail = detail
	s.mu.Unlock()
}

// escHTML escapes special HTML characters to prevent injection.
func escHTML(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			out = append(out, "&amp;"...)
		case '<':
			out = append(out, "&lt;"...)
		case '>':
			out = append(out, "&gt;"...)
		case '"':
			out = append(out, "&quot;"...)
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}
