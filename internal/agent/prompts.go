package agent

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"text/template"
	"time"
)

//go:embed templates/momo.md.tpl
var momoPromptTmpl string

//go:embed templates/title.md
var titlePromptTmpl string

//go:embed templates/summary.md
var summaryPromptTmpl string

// EnvData holds environment information injected into prompts.
type EnvData struct {
	WorkingDir string
	Platform   string
	Date       string
}

// BuildTitlePrompt renders the title generation prompt.
func BuildTitlePrompt() string {
	return titlePromptTmpl
}

// BuildSummaryPrompt renders the summary generation prompt.
func BuildSummaryPrompt() string {
	return summaryPromptTmpl
}

// renderPrompt executes a Go template with the given data.
func renderPrompt(tmplText, name string, data any) string {
	tmpl, err := template.New(name).Parse(tmplText)
	if err != nil {
		// Fallback to a basic prompt if template fails.
		return fmt.Sprintf("You are momo, an AI assistant.\nDate: %s", time.Now().Format("1/2/2006"))
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("You are momo, an AI assistant.\nDate: %s", time.Now().Format("1/2/2006"))
	}
	return buf.String()
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return wd
}
