package agent

import (
	"strings"
	"testing"
)

func newTestSession() *Session {
	return &Session{ID: "test"}
}

func TestContextManager_NoCompression_UnderLimit(t *testing.T) {
	cm := NewContextManager()
	cm.MaxTurns = 10

	s := newTestSession()
	for i := 0; i < 5; i++ {
		s.AddUserMessage("user msg")
		s.AddAssistantMessage("assistant msg")
	}

	result := cm.CheckAndCompress(s)
	if result != "" {
		t.Fatalf("Expected no compression for 5 turns (max 10), got: %s", result)
	}
}

func TestContextManager_CompressWhenExceeded(t *testing.T) {
	cm := NewContextManager()
	cm.MaxTurns = 3

	s := newTestSession()
	for i := 0; i < 5; i++ {
		s.AddUserMessage("question " + string(rune('A'+i)))
		s.AddAssistantMessage("answer " + string(rune('A'+i)))
	}

	// CheckAndCompress just logs, doesn't mutate.
	result := cm.CheckAndCompress(s)
	if result == "" {
		t.Fatal("Expected compression warning")
	}

	// Original messages should be untouched.
	if len(s.Messages) != 10 {
		t.Fatalf("Expected 10 original messages preserved, got %d", len(s.Messages))
	}

	// BuildLLMMessages should return compressed version.
	compressed := cm.BuildLLMMessages(s.Messages)
	if len(compressed) >= len(s.Messages) {
		t.Fatalf("Expected compressed messages < original, got %d >= %d", len(compressed), len(s.Messages))
	}

	// Should have a summary system message.
	hasSummary := false
	for _, m := range compressed {
		if m.Role == "system" && strings.Contains(m.Content, "Earlier conversation") {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		t.Fatal("Expected a summary system message in compressed output")
	}
}

func TestContextManager_ToolCallsDontInflateTurnCount(t *testing.T) {
	cm := NewContextManager()
	cm.MaxTurns = 5

	s := newTestSession()
	for i := 0; i < 6; i++ {
		s.AddUserMessage("user msg")
		s.AddAssistantToolCallMessage([]ToolCallMessage{
			{ID: "call_1", Type: "function", Function: struct {
				Name      string "json:\"name\""
				Arguments string "json:\"arguments\""
			}{Name: "get_time", Arguments: "{}"}},
		})
		s.AddToolResult("call_1", "get_time", "2026-07-21", false)
		s.AddAssistantMessage("assistant response")
	}

	// 6 user turns > max 5, should compress.
	compressed := cm.BuildLLMMessages(s.Messages)
	if len(compressed) >= len(s.Messages) {
		t.Fatalf("Expected compression, got %d >= %d", len(compressed), len(s.Messages))
	}

	// Original should still be full.
	if len(s.Messages) != 24 {
		t.Fatalf("Original messages should be 24, got %d", len(s.Messages))
	}
}

func TestContextManager_FollowUpQuestions(t *testing.T) {
	cm := NewContextManager()
	cm.MaxTurns = 100

	s := newTestSession()
	s.AddUserMessage("北京的天气怎么样？")
	s.AddAssistantMessage("北京今天25°C，晴天。")
	s.AddUserMessage("那明天呢？")
	s.AddAssistantMessage("明天28°C，多云。")

	// Should not compress at 2 turns.
	compressed := cm.BuildLLMMessages(s.Messages)
	if len(compressed) != len(s.Messages) {
		t.Fatalf("Expected no compression for 2 turns, got %d messages", len(compressed))
	}

	// Verify both questions are preserved.
	userCount := 0
	for _, m := range compressed {
		if m.Role == RoleUser {
			userCount++
		}
	}
	if userCount != 2 {
		t.Fatalf("Expected 2 user messages preserved, got %d", userCount)
	}
}
