package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// WebFetch fetches a URL and returns readable text content.
type WebFetch struct {
	client *http.Client
}

// NewWebFetch creates a web_fetch tool.
func NewWebFetch() *WebFetch {
	return &WebFetch{
		client: newHTTPClient(),
	}
}

func (f *WebFetch) Name() string { return "web" }

func (f *WebFetch) Description() string {
	return "Search the web or fetch any URL. Returns readable text from HTML pages, JSON APIs, or plain text. Follow up with additional fetches to get full page content."
}

func (f *WebFetch) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The full URL to fetch, including protocol (e.g. https://www.bing.com/search?q=latest+news)",
			},
		},
		"required": []string{"url"},
	}
}

func (f *WebFetch) Execute(ctx context.Context, params map[string]any) (string, error) {
	urlStr, _ := params["url"].(string)
	urlStr = strings.TrimSpace(urlStr)
	if urlStr == "" {
		return "", fmt.Errorf("url is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.5")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	maxSize := int64(512 * 1024) // 512KB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}

	if !utf8.Valid(body) {
		body = []byte(strings.ToValidUTF8(string(body), ""))
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "text/html") {
		content = extractText(content)
	}

	// Truncate at 50KB for LLM consumption.
	const maxLen = 50 * 1024
	if len(content) > maxLen {
		content = content[:maxLen] + "\n\n...[truncated]..."
	}

	return fmt.Sprintf("Content from %s:\n\n%s", urlStr, strings.TrimSpace(content)), nil
}

// extractText extracts readable text from HTML.
func extractText(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}

	var buf strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if buf.Len() > 0 {
					buf.WriteByte(' ')
				}
				buf.WriteString(text)
			}
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "nav", "footer", "header", "aside", "noscript", "iframe", "svg":
				return
			case "br", "p", "div", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "li":
				if buf.Len() > 0 {
					buf.WriteByte('\n')
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "li":
				buf.WriteByte('\n')
			}
		}
	}
	f(doc)

	result := buf.String()
	// Collapse multiple newlines.
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}
