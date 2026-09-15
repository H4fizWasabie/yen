package codingagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/tools"
)

var explorerSlots = make(chan struct{}, 3)

type exploreTool struct {
	cwd      string
	provider agent.Provider
}

func (exploreTool) Name() string { return "explore" }

func (t exploreTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	question, ok := args["question"].(string)
	if !ok || strings.TrimSpace(question) == "" {
		return "", fmt.Errorf("question is required")
	}
	tier := "quick-scan"
	if value, ok := args["tier"].(string); ok && value != "" {
		tier = value
	}
	lines, turns := 30, 8
	if tier == "deep-map" {
		lines, turns = 80, 15
	} else if tier != "quick-scan" {
		return "", fmt.Errorf("tier must be quick-scan or deep-map")
	}
	select {
	case explorerSlots <- struct{}{}:
		defer func() { <-explorerSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	history := []agent.Message{{Role: "system", Content: explorerPrompt(tier, lines, turns)}}
	result, err := agent.RunFrom(ctx, t.provider, []agent.Tool{
		tools.NewReadTool(t.cwd),
		tools.NewGrepTool(t.cwd),
		tools.NewFindTool(t.cwd),
		tools.NewListTool(t.cwd),
	}, history, question)
	if err != nil {
		return "", err
	}
	return capExplorerAnswer(result.FinalText, lines), nil
}

func explorerPrompt(tier string, lines, turns int) string {
	structural := ""
	if tier == "deep-map" {
		structural = " Start with a concise structural overview before findings."
	}
	return fmt.Sprintf("You are Theoses's background explorer: a read-only codebase scouting agent. Answer one question with evidence, never dump raw tool output. Use only read, grep, find, and ls. Cite file paths and line locations when possible.%s Keep the answer to at most %d lines and stop within %d turns.", structural, lines, turns)
}

func capExplorerAnswer(answer string, limit int) string {
	lines := strings.Split(strings.TrimSpace(answer), "\n")
	if len(lines) <= limit {
		return strings.TrimSpace(answer)
	}
	return strings.Join(lines[:limit], "\n") + fmt.Sprintf("\n[truncated by harness: exceeded cap of %d lines]", limit)
}

func newExploreTool(workspace string, provider agent.Provider) agent.Tool {
	return exploreTool{cwd: workspace, provider: provider}
}

type codingToolOptions struct {
	current  *session.Session
	provider agent.Provider
}

var _ agent.Tool = exploreTool{}
