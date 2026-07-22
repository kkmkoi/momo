package agent

import (
	"context"
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&mockTool{name: "test_tool"}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := r.Register(&mockTool{name: "test_tool"}); err == nil {
		t.Fatal("Expected error registering duplicate tool")
	}

	tool, ok := r.Get("test_tool")
	if !ok {
		t.Fatal("Expected to find tool")
	}
	if tool.Name() != "test_tool" {
		t.Fatalf("Expected name 'test_tool', got %q", tool.Name())
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Fatal("Expected not to find nonexistent tool")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "a"})
	r.Register(&mockTool{name: "b"})
	r.Register(&mockTool{name: "c"})

	names := r.List()
	if len(names) != 3 {
		t.Fatalf("Expected 3 tools, got %d", len(names))
	}

	seen := make(map[string]bool)
	for _, n := range names {
		seen[n] = true
	}
	if !seen["a"] || !seen["b"] || !seen["c"] {
		t.Fatal("Missing expected tool names")
	}
}

func TestRegistry_Schemas(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "calc"})
	schemas := r.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("Expected 1 schema, got %d", len(schemas))
	}
	fn, ok := schemas[0]["function"].(map[string]any)
	if !ok {
		t.Fatal("Expected function field in schema")
	}
	if fn["name"] != "calc" {
		t.Fatalf("Expected name 'calc', got %v", fn["name"])
	}
}

func TestRegistry_Execute(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "echo"})

	result, err := r.Execute(context.Background(), "echo", `{"msg":"hello"}`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result != "hello" {
		t.Fatalf("Expected 'hello', got %q", result)
	}

	_, err = r.Execute(context.Background(), "unknown", "{}")
	if err == nil {
		t.Fatal("Expected error for unknown tool")
	}
}

type mockTool struct {
	name string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return "mock tool for testing" }
func (m *mockTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
	}
}
func (m *mockTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if msg, ok := params["msg"]; ok {
		return msg.(string), nil
	}
	return "ok", nil
}
