package tools

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestCalculator_Basic(t *testing.T) {
	c := &Calculator{}
	tests := []struct {
		name   string
		expr   string
		expect string
	}{
		{"addition", "2+2", "4"},
		{"subtraction", "10-3", "7"},
		{"multiplication", "4*5", "20"},
		{"division", "20/4", "5"},
		{"sqrt", "sqrt(16)", "4"},
		{"power", "2**10", "1024"},
		{"complex", "3+4*2", "11"},
		{"parentheses", "(3+4)*2", "14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := c.Execute(context.Background(), map[string]any{
				"expression": tt.expr,
			})
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if !strings.Contains(result, tt.expect) {
				t.Fatalf("Expected %q to contain %q", result, tt.expect)
			}
		})
	}
}

func TestCalculator_EdgeCases(t *testing.T) {
	c := &Calculator{}

	// Division by zero.
	_, err := c.Execute(context.Background(), map[string]any{
		"expression": "1/0",
	})
	if err == nil {
		t.Fatal("Expected error for division by zero")
	}

	// Empty expression.
	_, err = c.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("Expected error for missing expression")
	}
}

func TestCalculator_Scientific(t *testing.T) {
	c := &Calculator{}
	result, err := c.Execute(context.Background(), map[string]any{
		"expression": "sin(0)",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, "0") {
		t.Fatalf("Expected sin(0) to be 0, got %s", result)
	}
}

func TestReadDocs_LocalFile(t *testing.T) {
	// Create a temporary file to read.
	tmpFile, err := os.CreateTemp("", "test_read_docs_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	testContent := "Hello, this is a test file for ReadDocs."
	if _, err := tmpFile.WriteString(testContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	d := NewReadDocs()
	result, err := d.Execute(context.Background(), map[string]any{
		"path": tmpFile.Name(),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, testContent) {
		t.Fatalf("Expected result to contain file content, got %q", result)
	}
}

func TestReadDocs_FileNotFound(t *testing.T) {
	d := NewReadDocs()
	_, err := d.Execute(context.Background(), map[string]any{
		"path": "/nonexistent/path/to/file.txt",
	})
	if err == nil {
		t.Fatal("Expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Expected 'not found' error, got %v", err)
	}
}

func TestReadDocs_MissingPath(t *testing.T) {
	d := NewReadDocs()
	_, err := d.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("Expected error for missing path")
	}
}

func TestReadDocs_DirectoryListing(t *testing.T) {
	d := NewReadDocs()
	result, err := d.Execute(context.Background(), map[string]any{
		"path": ".",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, ".go") {
		t.Fatalf("Expected directory listing to contain .go files, got %q", result)
	}
}

func TestReadDocs_AbsolutePath(t *testing.T) {
	// Create a temp file with an absolute path.
	tmpFile, err := os.CreateTemp("", "test_abs_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	testContent := "Absolute path test."
	if _, err := tmpFile.WriteString(testContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	d := NewReadDocs()
	result, err := d.Execute(context.Background(), map[string]any{
		"path": tmpFile.Name(),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, testContent) {
		t.Fatalf("Expected file content, got %q", result)
	}
}

func TestWeather_CityRequired(t *testing.T) {
	w := NewWeather()
	_, err := w.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("Expected error for missing city")
	}
}
