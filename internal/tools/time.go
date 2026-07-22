package tools

import (
	"context"
	"fmt"
	"time"
)

// GetTime returns the current date and time.
type GetTime struct{}

func NewGetTime() *GetTime { return &GetTime{} }

func (g *GetTime) Name() string { return "get_time" }

func (g *GetTime) Description() string {
	return "Get the current date and time. Returns the current system time with timezone information."
}

func (g *GetTime) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (g *GetTime) Execute(_ context.Context, _ map[string]any) (string, error) {
	now := time.Now()
	return fmt.Sprintf("🕐 当前时间: %s\n时区: %s\nUnix时间戳: %d",
		now.Format("2006-01-02 15:04:05"),
		now.Location().String(),
		now.Unix()), nil
}
