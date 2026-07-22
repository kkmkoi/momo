package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/charmbracelet/crush-agent/internal/agent"
	"github.com/charmbracelet/crush-agent/internal/tools"
	"github.com/charmbracelet/crush-agent/internal/web"
)

func main() {
	var (
		apiKey   = flag.String("api-key", os.Getenv("OPENAI_API_KEY"), "OpenAI API key (or set OPENAI_API_KEY)")
		model    = flag.String("model", "gpt-4o-mini", "LLM model to use")
		port     = flag.Int("port", 8080, "HTTP server port")
		apiURL   = flag.String("api-url", os.Getenv("OPENAI_API_BASE"), "OpenAI-compatible API base URL (optional)")
		maxTurns = flag.Int("max-turns", 999, "Max conversation turns per request")
		dataDir  = flag.String("data-dir", "./data", "Directory for session persistence")
	)
	flag.Parse()

	if *apiKey == "" {
		log.Fatal("API key required. Set OPENAI_API_KEY env var or pass --api-key")
	}

	// Create tool registry and register tools.
	reg := agent.NewRegistry()
	for _, t := range []agent.Tool{
		&tools.Calculator{},
		tools.NewWebFetch(),
		tools.NewGetTime(),
		tools.NewReadDocs(),
		tools.NewWeather(),
	} {
		if err := reg.Register(t); err != nil {
			log.Fatalf("Failed to register tool %q: %v", t.Name(), err)
		}
	}

	log.Printf("Registered tools: %v", reg.List())

	// Configure agent options.
	opts := []agent.AgentOption{
		agent.WithModel(*model),
		agent.WithMaxTurns(*maxTurns),
	}
	if *apiURL != "" {
		opts = append(opts, agent.WithLLMApiURL(*apiURL))
	}

	// Create agent.
	a := agent.NewAgent(*apiKey, reg, *dataDir, opts...)

	// Create web handler and start server.
	h := web.NewHandler(a)
	addr := fmt.Sprintf(":%d", *port)

	log.Printf("🤖 Agent runtime initialized")
	log.Printf("   Model: %s", *model)
	log.Printf("   Tools: %v", reg.List())

	if err := web.RunServer(addr, h); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
