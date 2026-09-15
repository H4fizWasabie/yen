package codingagent

import (
	"context"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type exploreProvider struct {
	tools []string
}

func (p *exploreProvider) Next(_ context.Context, _ []agent.Message, tools []string) (agent.Response, error) {
	p.tools = append([]string(nil), tools...)
	return agent.Response{Text: strings.Repeat("finding\n", 40), StopReason: "stop"}, nil
}

func TestExploreIsReadOnlyAndCapsQuickScan(t *testing.T) {
	provider := &exploreProvider{}
	tool := newExploreTool(t.TempDir(), provider)
	answer, err := tool.Execute(context.Background(), map[string]any{"question": "where is the session loop?"})
	if err != nil {
		t.Fatal(err)
	}
	if lines := len(strings.Split(answer, "\n")); lines > 32 {
		t.Fatalf("lines=%d answer=%q", lines, answer)
	}
	if !strings.Contains(answer, "1/8 turns") {
		t.Fatalf("missing budget footer: %q", answer)
	}
	for _, name := range provider.tools {
		if name != "read" && name != "grep" && name != "find" && name != "ls" {
			t.Fatalf("non-read-only tool exposed: %q", name)
		}
	}
}

type loopingExploreProvider struct{ calls int }

func (p *loopingExploreProvider) Next(_ context.Context, _ []agent.Message, _ []string) (agent.Response, error) {
	p.calls++
	return agent.Response{ToolCalls: []agent.ToolCall{{ID: "scan", Name: "ls"}}, StopReason: "toolUse"}, nil
}

func TestExploreStopsAtQuickScanTurnBudget(t *testing.T) {
	provider := &loopingExploreProvider{}
	answer, err := newExploreTool(t.TempDir(), provider).Execute(context.Background(), map[string]any{"question": "map it"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "INCOMPLETE:") || !strings.Contains(answer, "8/8 turns") {
		t.Fatalf("answer=%q", answer)
	}
	if provider.calls != 8 {
		t.Fatalf("provider calls=%d, want 8", provider.calls)
	}
}
