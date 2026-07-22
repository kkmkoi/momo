package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadDocs provides direct local file reading (no permission checks).
type ReadDocs struct{}

// NewReadDocs creates a read_docs tool that reads local files.
func NewReadDocs() *ReadDocs {
	return &ReadDocs{}
}

func (d *ReadDocs) Name() string { return "read_docs" }

func (d *ReadDocs) Description() string {
	return "Read any file on the local computer. Provide an absolute or relative path. Supports Windows paths (C:\\...), Unix paths (/...), and relative paths."
}

func (d *ReadDocs) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path to read. Examples: 'README.md', 'C:\\Users\\name\\doc.txt', '/home/user/file.go', './internal/agent/agent.go'",
			},
		},
		"required": []string{"path"},
	}
}

func (d *ReadDocs) Execute(_ context.Context, params map[string]any) (string, error) {
	rawPath, _ := params["path"].(string)
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("path is required")
	}

	// Resolve the path.
	resolved, err := resolvePath(rawPath)
	if err != nil {
		return "", err
	}

	// Check if path exists and is a file.
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s\n\nResolved path: %s\nWorking dir: %s",
				rawPath, resolved, mustGetwd())
		}
		return "", fmt.Errorf("cannot access %q: %w", rawPath, err)
	}

	if info.IsDir() {
		// List directory contents instead.
		return listDir(resolved)
	}

	// Read file contents.
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("failed to read %q: %w", rawPath, err)
	}

	content := string(data)

	// Truncate very large files (>100KB).
	const maxSize = 100 * 1024
	if len(content) > maxSize {
		content = content[:maxSize]
		content += "\n\n... [file truncated at 100KB] ..."
	}

	return fmt.Sprintf("📄 %s (%d bytes)\n\n%s", resolved, info.Size(), content), nil
}

// resolvePath expands ~, normalizes slashes, and converts to absolute path.
func resolvePath(rawPath string) (string, error) {
	// Expand ~ to home directory.
	if strings.HasPrefix(rawPath, "~/") || rawPath == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand ~: %w", err)
		}
		if rawPath == "~" {
			rawPath = home
		} else {
			rawPath = filepath.Join(home, rawPath[2:])
		}
	}

	// Normalize path separators (for Windows compatibility).
	cleaned := filepath.Clean(rawPath)

	// Convert to absolute path if relative.
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %q: %w", rawPath, err)
	}

	return abs, nil
}

// listDir returns a listing of the given directory.
func listDir(dirPath string) (string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", fmt.Errorf("cannot read directory %q: %w", dirPath, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📁 %s\n\n", dirPath)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		info, err := entry.Info()
		size := ""
		if err == nil && !entry.IsDir() {
			size = fmt.Sprintf(" (%d bytes)", info.Size())
		}
		fmt.Fprintf(&b, "  %s%s\n", name, size)
	}
	fmt.Fprintf(&b, "\n%d entries", len(entries))
	return b.String(), nil
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return wd
}
