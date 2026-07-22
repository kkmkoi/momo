package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// newHTTPClient creates a properly configured HTTP client for LLM API calls.
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: transport,
	}
}

// buildSystemPrompt renders the system prompt template with env info.
func buildSystemPrompt() string {
	return renderPrompt(momoPromptTmpl, "momo", EnvData{
		WorkingDir: mustGetwd(),
		Platform:   runtime.GOOS,
		Date:       time.Now().Format("1/2/2006"),
	})
}


// AgentEvent is a structured event emitted during agent execution for real-time UI.
type AgentEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

const (
	EventToolCall   = "tool_call"
	EventToolResult = "tool_result"
	EventLLMStart   = "llm_start"
	EventLLMEnd     = "llm_end"
	EventResponse   = "response"
	EventError      = "server_error"
	EventDone       = "done"
)

// Agent is the core agent runtime that orchestrates the LLM loop.
type Agent struct {
	client    *openai.Client
	apiKey    string
	model     string
	registry  *Registry
	sessions  *SessionManager
	ctxMgr    *ContextManager
	maxTurns  int
	LLMApiURL string
	LastTrace string
	httpCli   *http.Client

	OnTrace func(string)           // raw trace text
	OnEvent func(AgentEvent)       // structured events for real-time UI
}

// AgentOption configures the agent.
type AgentOption func(*Agent)

func WithModel(model string) AgentOption {
	return func(a *Agent) { a.model = model }
}

func WithMaxTurns(n int) AgentOption {
	return func(a *Agent) { a.maxTurns = n }
}

func WithLLMApiURL(url string) AgentOption {
	return func(a *Agent) { a.LLMApiURL = url }
}

// NewAgent creates a new agent with disk-backed session persistence.
func NewAgent(apiKey string, registry *Registry, dataDir string, opts ...AgentOption) *Agent {
	client := openai.NewClient(apiKey)
	store := NewStore(dataDir)
	a := &Agent{
		client:    client,
		apiKey:    apiKey,
		model:     "gpt-4o-mini",
		registry:  registry,
		sessions:  NewSessionManager(store),
		ctxMgr:    NewContextManager(),
		maxTurns:  999,
		LastTrace: "",
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.LLMApiURL != "" {
		cfg := openai.DefaultConfig(apiKey)
		cfg.BaseURL = a.LLMApiURL
		a.client = openai.NewClientWithConfig(cfg)
	}
	a.httpCli = newHTTPClient()
	return a
}

// RunResult contains the full result of a single agent run.
type RunResult struct {
	Content   string     `json:"content"`
	Turns     int        `json:"turns"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Trace     string     `json:"trace"`
	Error     string     `json:"error,omitempty"`
}

// Run executes one user input through the full agent loop.
func (a *Agent) Run(ctx context.Context, sessionID, userInput string) (*RunResult, error) {
	release, ok := a.sessions.TryLockRun(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s is busy", sessionID)
	}
	defer release()

	sess := a.sessions.GetOrCreate(sessionID)
	defer a.sessions.Save(sessionID)

	// Add user message.
	sess.AddUserMessage(userInput)

	// Auto-generate title from first message if not set.
	if sess.Title == "" {
		title := userInput
		if len(title) > 30 {
			title = title[:30] + "..."
		}
		sess.Title = title
	}

	// Build trace.
	var traceBuilder strings.Builder
	traceBuilder.WriteString(fmt.Sprintf("[%s] Session %s: %s\n", time.Now().Format(time.RFC3339), sessionID, userInput))
	a.emitTrace(traceBuilder.String())
	var allToolCalls []ToolCall

	for turn := 0; ; turn++ {
		// Check if context was canceled (e.g. client disconnect, 120s timeout).
		if ctx.Err() != nil {
			sess.SetStatus("error", ctx.Err().Error())
			traceBuilder.WriteString(fmt.Sprintf("Context canceled: %v\n", ctx.Err()))
			a.LastTrace = traceBuilder.String()
			a.emitTrace(traceBuilder.String())
			a.emitEvent(ctx, AgentEvent{Type: EventError, Data: map[string]string{"message": ctx.Err().Error()}})
			a.emitEvent(ctx, AgentEvent{Type: EventDone, Data: map[string]string{}})
			return &RunResult{
				Trace: traceBuilder.String(),
				Error: fmt.Sprintf("Request canceled: %v", ctx.Err()),
			}, nil
		}

		// Check context limits.
		if cMsg := a.ctxMgr.CheckAndCompress(sess); cMsg != "" {
			traceBuilder.WriteString(cMsg + "\n")
			a.emitTrace(traceBuilder.String())
		}

		// Call LLM — emit start event.
		sess.SetStatus("thinking", "")
		a.emitEvent(ctx, AgentEvent{Type: EventLLMStart, Data: map[string]string{"model": a.model, "turn": fmt.Sprintf("%d", turn+1)}})

		llmTrace, toolCalls, response, err := a.callLLM(ctx, sess)
		traceBuilder.WriteString(llmTrace)
		a.emitTrace(traceBuilder.String())

		a.emitEvent(ctx, AgentEvent{Type: EventLLMEnd, Data: map[string]any{
			"tool_calls": len(toolCalls),
		}})

		if err != nil {
			sess.SetStatus("error", err.Error())
			traceBuilder.WriteString(fmt.Sprintf("LLM error: %v\n", err))
			a.LastTrace = traceBuilder.String()
			a.emitTrace(traceBuilder.String())
			a.emitEvent(ctx, AgentEvent{Type: EventError, Data: map[string]string{"message": err.Error()}})
			return &RunResult{
				Trace: traceBuilder.String(),
				Error: fmt.Sprintf("LLM call failed: %v", err),
			}, nil
		}

		// If no tool calls, the LLM responded directly.
		if len(toolCalls) == 0 {
			sess.AddAssistantMessage(response)
			sess.SetStatus("done", "")
			traceBuilder.WriteString(fmt.Sprintf("  → %s\n", truncate(response, 200)))
			a.LastTrace = traceBuilder.String()
			a.emitTrace(traceBuilder.String())
			a.emitEvent(ctx, AgentEvent{Type: EventResponse, Data: map[string]string{"content": response}})
			a.emitEvent(ctx, AgentEvent{Type: EventDone, Data: map[string]string{}})
			return &RunResult{
				Content:   response,
				Turns:     turn + 1,
				ToolCalls: allToolCalls,
				Trace:     traceBuilder.String(),
			}, nil
		}

		// LLM wants to call tools — emit tool_call events immediately.
		var tcms []ToolCallMessage
		for _, tc := range toolCalls {
			tcms = append(tcms, tc.ToolCallMessage)
			sess.SetStatus("tool_call", tc.Name+"("+truncate(tc.Args, 100)+")")
			a.emitEvent(ctx, AgentEvent{Type: EventToolCall, Data: map[string]any{
				"id":   tc.ID,
				"name": tc.Name,
				"args": tc.Args,
			}})
		}
		sess.AddAssistantToolCallMessage(tcms)

		// Execute each tool — emit result events as they complete.
		var turnToolCalls []ToolCall
		for _, tc := range toolCalls {
			startTime := time.Now()
			tc.Result, tc.IsError = a.executeToolCall(ctx, &traceBuilder, tc)
			sess.AddToolResult(tc.ID, tc.Name, tc.Result, tc.IsError)
			turnToolCalls = append(turnToolCalls, tc)
			if tc.IsError {
				sess.SetStatus("error", tc.Name+": "+truncate(tc.Result, 100))
			} else {
				sess.SetStatus("tool_result", tc.Name+": "+truncate(tc.Result, 100))
			}
			a.emitEvent(ctx, AgentEvent{Type: EventToolResult, Data: map[string]any{
				"id":       tc.ID,
				"name":     tc.Name,
				"result":   truncate(tc.Result, 200),
				"error":    tc.IsError,
				"duration": time.Since(startTime).String(),
			}})
		}
		allToolCalls = append(allToolCalls, turnToolCalls...)
		a.emitTrace(traceBuilder.String())
	}
}

// safeMsg is our own message struct that omits empty content for tool_calls.
type safeMsg struct {
	Role         string              `json:"role"`
	Content      *string             `json:"content,omitempty"`
	ToolCalls    []openai.ToolCall   `json:"tool_calls,omitempty"`
	ToolCallID   string              `json:"tool_call_id,omitempty"`
}

// buildSafeMessages converts internal messages to safeMsg (control JSON output).
func buildSafeMessages(msgs []map[string]any) []safeMsg {
	var out []safeMsg
	for _, m := range msgs {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)

		sm := safeMsg{Role: role}

		switch role {
		case "assistant":
			if tcs, ok := m["tool_calls"].([]map[string]any); ok && len(tcs) > 0 {
				// Tool-calls message: content must be omitted/null, not "".
				var tools []openai.ToolCall
				for _, tc := range tcs {
					id, _ := tc["id"].(string)
					tcType, _ := tc["type"].(string)
					fn, _ := tc["function"].(map[string]any)
					fnName, _ := fn["name"].(string)
					fnArgs, _ := fn["arguments"].(string)
					tools = append(tools, openai.ToolCall{
						ID:   id,
						Type: openai.ToolType(tcType),
						Function: openai.FunctionCall{
							Name:      fnName,
							Arguments: fnArgs,
						},
					})
				}
				sm.ToolCalls = tools
				// Content stays nil → omitted from JSON.
			} else if content != "" {
				sm.Content = &content
			}
		case "tool":
			sm.Content = &content
			if tid, ok := m["tool_call_id"].(string); ok {
				sm.ToolCallID = tid
			}
		default:
			// user, system
			if content != "" {
				sm.Content = &content
			}
		}
		out = append(out, sm)
	}
	return out
}

// openaiResponse is the subset of the OpenAI response we need.
type openaiResponse struct {
	Choices []struct {
		Message struct {
			Role      string            `json:"role"`
			Content   string            `json:"content"`
			ToolCalls []openai.ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// callLLM sends the session messages to the LLM using a raw HTTP request
// so we can control exactly how tool_calls messages are serialized.
func (a *Agent) callLLM(ctx context.Context, sess *Session) (string, []ToolCall, string, error) {
	// Build compressed message list for LLM (preserves full history on disk).
	compressed := a.ctxMgr.BuildLLMMessages(sess.Messages)
	msgs := MessagesToOpenAI(compressed)
	safeMsgs := buildSafeMessages(msgs)

	// Prepend system prompt with env info.
	sysContent := buildSystemPrompt()
	safeMsgs = append([]safeMsg{{Role: "system", Content: &sysContent}}, safeMsgs...)

	// Build the request body manually.
	reqBody := map[string]any{
		"model":       a.model,
		"messages":    safeMsgs,
		"tools":       toRequestTools(a.registry.Schemas()),
		"temperature": 0.7,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Determine API URL.
	apiURL := a.LLMApiURL
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1"
	}
	apiURL = strings.TrimRight(apiURL, "/")

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL+"/chat/completions", bytes.NewReader(jsonBytes))
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.httpCli.Do(httpReq)
	if err != nil {
		return "", nil, "", fmt.Errorf("API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil, "", fmt.Errorf("error, status code: %d, message: %s", resp.StatusCode, string(respBody))
	}

	var result openaiResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", nil, "", fmt.Errorf("empty response from LLM")
	}

	choice := result.Choices[0]
	msg := choice.Message

	trace := fmt.Sprintf("  [LLM] %s (tokens: %d+%d, finish: %s)\n",
		a.model, result.Usage.PromptTokens, result.Usage.CompletionTokens, choice.FinishReason)

	// Parse tool calls.
	if len(msg.ToolCalls) > 0 {
		var calls []ToolCall
		for _, tc := range msg.ToolCalls {
			call := ToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: tc.Function.Arguments,
				ToolCallMessage: ToolCallMessage{
					ID:   tc.ID,
					Type: string(tc.Type),
					Function: struct {
						Name      string "json:\"name\""
						Arguments string "json:\"arguments\""
					}{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				},
			}
			calls = append(calls, call)
			trace += fmt.Sprintf("  [tool call] %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
		}
		return trace, calls, msg.Content, nil
	}

	// Text response.
	return trace, nil, msg.Content, nil
}

func (a *Agent) emitTrace(msg string) {
	if a.OnTrace != nil {
		a.OnTrace(msg)
	}
}

// eventCallbackKey is used to pass a per-request event callback through context.
type eventCallbackKey struct{}

// WithEventCallback returns a context with a per-request event callback.
func WithEventCallback(ctx context.Context, cb func(AgentEvent)) context.Context {
	return context.WithValue(ctx, eventCallbackKey{}, cb)
}

func (a *Agent) emitEvent(ctx context.Context, evt AgentEvent) {
	if cb, _ := ctx.Value(eventCallbackKey{}).(func(AgentEvent)); cb != nil {
		cb(evt)
		return
	}
	if a.OnEvent != nil {
		a.OnEvent(evt)
	}
}

// executeToolCall runs a single tool and updates the trace.
func (a *Agent) executeToolCall(ctx context.Context, trace *strings.Builder, tc ToolCall) (string, bool) {
	start := time.Now()
	result, err := a.registry.Execute(ctx, tc.Name, tc.Args)
	duration := time.Since(start)

	if err != nil {
		trace.WriteString(fmt.Sprintf("  [tool error] %s(%s): %v (%v)\n", tc.Name, tc.Args, err, duration))
		return fmt.Sprintf("Error: %v", err), true
	}

	trace.WriteString(fmt.Sprintf("  [tool ok] %s → %s (%v)\n", tc.Name, truncate(result, 150), duration))
	return result, false
}

// GetSessionManager returns the session manager.
func (a *Agent) GetSessionManager() *SessionManager {
	return a.sessions
}

// toRequestTools converts internal tool schemas to OpenAI format.
func toRequestTools(schemas []map[string]any) []openai.Tool {
	out := make([]openai.Tool, len(schemas))
	for i, s := range schemas {
		fn, _ := s["function"].(map[string]any)
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params, _ := fn["parameters"].(map[string]any)

		out[i] = openai.Tool{
			Type: "function",
			Function: &openai.FunctionDefinition{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// GenerateTitle creates a session title using the LLM.
func (a *Agent) GenerateTitle(ctx context.Context, sessionID string) string {
	sess := a.sessions.Get(sessionID)
	if sess == nil || len(sess.Messages) == 0 {
		return "New Session"
	}

	firstMsg := sess.Messages[0].Content
	if len(firstMsg) > 200 {
		firstMsg = firstMsg[:200]
	}

	sysPrompt := BuildTitlePrompt()
	reqBody := map[string]any{
		"model": a.model,
		"messages": []map[string]any{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": fmt.Sprintf("Generate a title for a conversation starting with: %s", firstMsg)},
		},
		"temperature": 0.3,
		"max_tokens":  50,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "New Session"
	}

	apiURL := strings.TrimRight(a.LLMApiURL, "/")
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1"
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL+"/chat/completions", bytes.NewReader(jsonBytes))
	if err != nil {
		return "New Session"
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.httpCli.Do(httpReq)
	if err != nil {
		return "New Session"
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "New Session"
	}

	var result openaiResponse
	if err := json.Unmarshal(body, &result); err != nil || len(result.Choices) == 0 {
		return "New Session"
	}

	title := strings.TrimSpace(result.Choices[0].Message.Content)
	if title == "" {
		return "New Session"
	}
	return title
}
