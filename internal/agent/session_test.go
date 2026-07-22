package agent

import (
	"testing"
)

func newTestSessionManager(t *testing.T) *SessionManager {
	t.Helper()
	return NewSessionManager(NewStore(t.TempDir()))
}

func TestSessionManager_CreateAndGet(t *testing.T) {
	sm := newTestSessionManager(t)

	s := sm.GetOrCreate("session1")
	if s.ID != "session1" {
		t.Fatalf("Expected session1, got %s", s.ID)
	}

	// Same ID returns existing session.
	s2 := sm.GetOrCreate("session1")
	if s2 != s {
		t.Fatal("GetOrCreate should return same session for same ID")
	}

	// Different ID returns new session.
	s3 := sm.GetOrCreate("session2")
	if s3 == s {
		t.Fatal("GetOrCreate should return different session for different ID")
	}
}

func TestSessionManager_List(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.GetOrCreate("a")
	sm.GetOrCreate("b")

	ids := sm.List()
	if len(ids) != 2 {
		t.Fatalf("Expected 2 sessions, got %d", len(ids))
	}
}

func TestSessionManager_Delete(t *testing.T) {
	sm := newTestSessionManager(t)
	sm.GetOrCreate("session1")
	sm.Delete("session1")

	if s := sm.Get("session1"); s != nil {
		t.Fatal("Session should be nil after delete")
	}
}

func TestSession_AddAndRetrieveMessages(t *testing.T) {
	s := &Session{ID: "test"}
	s.AddUserMessage("Hello")
	s.AddAssistantMessage("Hi there!")

	if len(s.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(s.Messages))
	}
	if s.Messages[0].Role != RoleUser || s.Messages[0].Content != "Hello" {
		t.Fatalf("First message mismatch")
	}
	if s.Messages[1].Role != RoleAssistant || s.Messages[1].Content != "Hi there!" {
		t.Fatalf("Second message mismatch")
	}
}

func TestSession_ToolMessages(t *testing.T) {
	s := &Session{ID: "test"}

	tcs := []ToolCallMessage{
		{
			ID:   "call_1",
			Type: "function",
			Function: struct {
				Name      string "json:\"name\""
				Arguments string "json:\"arguments\""
			}{Name: "calculator", Arguments: `{"expression":"2+2"}`},
		},
	}
	s.AddAssistantToolCallMessage(tcs)

	s.AddToolResult("call_1", "calculator", "4", false)

	if len(s.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(s.Messages))
	}
	if len(s.Messages[0].ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call in assistant message")
	}
	if s.Messages[1].Role != RoleTool || s.Messages[1].ToolCallID != "call_1" {
		t.Fatalf("Tool result message mismatch")
	}
}

func TestSession_TurnCount(t *testing.T) {
	s := &Session{ID: "test"}
	if s.TurnCount != 0 {
		t.Fatalf("Initial turn count should be 0")
	}

	s.AddUserMessage("msg1")
	if s.TurnCount != 1 {
		t.Fatalf("Turn count should be 1 after first message")
	}

	s.AddUserMessage("msg2")
	if s.TurnCount != 2 {
		t.Fatalf("Turn count should be 2 after second message")
	}
}

func TestSession_ToOpenAIMessages(t *testing.T) {
	s := &Session{ID: "test"}
	s.AddUserMessage("Hello")
	s.AddAssistantMessage("World")

	msgs := s.ToOpenAIMessages()
	if len(msgs) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(msgs))
	}
	if msgs[0]["role"] != RoleUser || msgs[0]["content"] != "Hello" {
		t.Fatalf("First message mismatch")
	}
	if msgs[1]["role"] != RoleAssistant || msgs[1]["content"] != "World" {
		t.Fatalf("Second message mismatch")
	}
}

func TestSession_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Create session manager and add data.
	store1 := NewStore(dir)
	sm1 := NewSessionManager(store1)
	s := sm1.GetOrCreate("persist-test")
	s.AddUserMessage("hello")
	s.AddAssistantMessage("world")
	sm1.Save("persist-test")

	// Create new session manager pointing at same dir.
	store2 := NewStore(dir)
	sm2 := NewSessionManager(store2)
	s2 := sm2.GetOrCreate("persist-test")

	if len(s2.Messages) != 2 {
		t.Fatalf("Expected 2 persisted messages, got %d", len(s2.Messages))
	}
	if s2.Messages[0].Content != "hello" {
		t.Fatalf("Expected 'hello', got %q", s2.Messages[0].Content)
	}
}
