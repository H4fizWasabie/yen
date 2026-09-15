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
	limited := &turnLimitedProvider{provider: t.provider, limit: turns, tier: tier}
	result, err := agent.RunFrom(ctx, limited, []agent.Tool{
		tools.NewReadTool(t.cwd),
		tools.NewGrepTool(t.cwd),
		tools.NewFindTool(t.cwd),
		tools.NewListTool(t.cwd),
	}, history, question)
	if err != nil {
		return "", err
	}
	answer := strings.TrimSpace(result.FinalText)
	if answer == "" {
		answer = "INCOMPLETE: budget exhausted before a final answer was produced."
	}
	if strings.HasPrefix(answer, "INCOMPLETE:") {
		return answer + fmt.Sprintf("\n~%dK in, %d/%d turns", (limited.inputTokens+500)/1000, limited.calls, turns), nil
	}
	answer = capExplorerAnswer(answer, lines)
	if !strings.HasSuffix(answer, " turns") || !strings.Contains(answer[strings.LastIndex(answer, "\n")+1:], "/") {
		answer += fmt.Sprintf("\n~%dK in, %d/%d turns", (limited.inputTokens+500)/1000, limited.calls, turns)
	}
	return answer, nil
}

func explorerPrompt(tier string, lines, turns int) string {
	structural := ""
	if tier == "deep-map" {
		structural = " Start with a 5–15 line structural overview of components, entry points, and data flow before findings."
	}
	return fmt.Sprintf("You are Theoses's background explorer: a cheap, isolated scouting agent. Answer ONE question about a codebase with a distilled answer; never return raw tool dumps. Use only read, grep, find, and ls. Cite file paths and line locations when possible. Keep the answer to at most %d lines and stop within %d turns.%s End with a budget footer in the form ~<K> in, <turns>/%d turns. If the turn budget is reached before you can answer, say INCOMPLETE: <what is missing> instead of guessing.", lines, turns, structural, turns)
}

type turnLimitedProvider struct {
	provider     agent.Provider
	limit        int
	calls        int
	tier         string
	inputTokens  int
	outputTokens int
}

func (p *turnLimitedProvider) Next(ctx context.Context, messages []agent.Message, tools []string) (agent.Response, error) {
	if p.calls >= p.limit {
		return agent.Response{
			Text:       fmt.Sprintf("INCOMPLETE: explorer reached the %s turn budget.\n~0K in, %d/%d turns", p.tier, p.limit, p.limit),
			StopReason: "stop",
		}, nil
	}
	p.calls++
	response, err := p.provider.Next(ctx, messages, tools)
	if err == nil {
		p.inputTokens += response.Usage.Input
		p.outputTokens += response.Usage.Output
	}
	return response, err
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
