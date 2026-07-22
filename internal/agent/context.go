package agent

import (
	"fmt"
	"log"
)

// ContextManager handles context window limits and compression for LLM input.
// Original messages in the session are never deleted — compression only affects
// what gets sent to the LLM, computed on-the-fly in BuildLLMMessages.
type ContextManager struct {
	MaxTurns int // max user turns to include in LLM context
}

// NewContextManager creates a context manager with defaults.
func NewContextManager() *ContextManager {
	return &ContextManager{
		MaxTurns: 100,
	}
}

// CheckAndCompress checks if the session exceeds MaxTurns and logs it.
// It no longer mutates the session — that's done in BuildLLMMessages.
func (cm *ContextManager) CheckAndCompress(sess *Session) string {
	userTurns := countUserMessages(sess.Messages)
	if userTurns <= cm.MaxTurns {
		return ""
	}
	msg := fmt.Sprintf("Context: %d user turns, will compress to %d for LLM", userTurns, cm.MaxTurns)
	log.Println(msg)
	return msg
}

// BuildLLMMessages returns a compressed message slice for LLM consumption.
// Original session messages are untouched.
func (cm *ContextManager) BuildLLMMessages(msgs []Message) []Message {
	userTurns := countUserMessages(msgs)
	if userTurns <= cm.MaxTurns {
		return msgs
	}

	turns := groupTurns(msgs)
	excess := len(turns) - cm.MaxTurns

	summarizeEnd := excess
	if summarizeEnd < 1 {
		summarizeEnd = 1
	}
	if summarizeEnd > len(turns) {
		summarizeEnd = len(turns)
	}

	summary := compactSummary(turns[:summarizeEnd])

	var result []Message
	result = append(result, Message{
		Role:    RoleSystem,
		Content: fmt.Sprintf("[Earlier conversation: %s]", summary),
	})
	for _, t := range turns[summarizeEnd:] {
		result = append(result, t.messages...)
	}
	return result
}

// turn represents one user question and all associated activity.
type turn struct {
	messages []Message
}

// countUserMessages counts how many RoleUser messages are in the list.
func countUserMessages(msgs []Message) int {
	count := 0
	for _, m := range msgs {
		if m.Role == RoleUser {
			count++
		}
	}
	return count
}

// groupTurns splits messages into turns. Each turn starts at a user message
// and continues until the next user message (or end).
func groupTurns(msgs []Message) []turn {
	var turns []turn
	var current []Message
	for _, m := range msgs {
		if m.Role == RoleUser && len(current) > 0 {
			turns = append(turns, turn{messages: current})
			current = nil
		}
		current = append(current, m)
	}
	if len(current) > 0 {
		turns = append(turns, turn{messages: current})
	}
	return turns
}

// compactSummary merges multiple old turns into a concise text summary.
func compactSummary(turns []turn) string {
	var parts []string
	for _, t := range turns {
		var userText, answerText string
		var toolNames []string
		for _, m := range t.messages {
			switch m.Role {
			case RoleUser:
				userText = truncateStr(m.Content, 60)
			case RoleAssistant:
				if len(m.ToolCalls) > 0 {
					for _, tc := range m.ToolCalls {
						toolNames = append(toolNames, tc.Function.Name)
					}
				} else if m.Content != "" {
					answerText = truncateStr(m.Content, 80)
				}
			}
		}
		line := fmt.Sprintf("Q:%s", userText)
		if len(toolNames) > 0 {
			line += fmt.Sprintf(" [tools: %s]", joinUnique(toolNames))
		}
		if answerText != "" {
			line += fmt.Sprintf(" A:%s", answerText)
		}
		parts = append(parts, line)
	}
	return joinStrings(parts, " | ")
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func joinUnique(items []string) string {
	seen := make(map[string]bool)
	var result string
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		if result != "" {
			result += ","
		}
		result += item
	}
	return result
}

func joinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	result := items[0]
	for _, item := range items[1:] {
		result += sep + item
	}
	return result
}
