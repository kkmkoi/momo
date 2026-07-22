package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool is the interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	// Parameters returns a JSON Schema map describing the tool's parameters.
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) (string, error)
}

// ToolSchema converts a Tool into the OpenAI tool definition format.
func ToolSchema(t Tool) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"parameters":  t.Parameters(),
		},
	}
}

// Registry holds all registered tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) error {
	if _, exists := r.tools[t.Name()]; exists {
		return fmt.Errorf("tool %q already registered", t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tool names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// Schemas returns all tool definitions in OpenAI-compatible format.
func (r *Registry) Schemas() []map[string]any {
	out := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, ToolSchema(t))
	}
	return out
}

// Execute runs a tool by name with the given JSON arguments.
func (r *Registry) Execute(ctx context.Context, name, argsJSON string) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
		return "", fmt.Errorf("tool %q: invalid args: %w", name, err)
	}

	return t.Execute(ctx, params)
}

// ToolCall represents a tool call from the LLM.
type ToolCall struct {
	ID              string
	Name            string
	Args            string // raw JSON arguments
	Result          string
	IsError         bool
	ToolCallMessage       // embedded for conversion back to API format
}
