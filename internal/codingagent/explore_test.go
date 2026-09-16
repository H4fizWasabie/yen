package codingagent

import (
	"context"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type exploreProvider struct {
	tools []string
	seen  []agent.Message
}

func (p *exploreProvider) Next(_ context.Context, messages []agent.Message, tools []string) (agent.Response, error) {
	p.seen = append([]agent.Message(nil), messages...)
	p.tools = append([]string(nil), tools...)
	return agent.Response{Text: strings.Repeat("finding\n", 40), StopReason: "stop"}, nil
}

func TestExploreAppliesExtensionProviderHook(t *testing.T) {
	provider := &exploreProvider{}
	tool := newExploreToolWithHooks(t.TempDir(), provider, &agent.ToolHooks{
		ProviderBefore: func(_ context.Context, messages []agent.Message, _ []string) ([]agent.Message, error) {
			return append(messages, agent.Message{Role: "system", Content: "intercepted"}), nil
		},
	})
	if _, err := tool.Execute(context.Background(), map[string]any{"question": "map it"}); err != nil {
		t.Fatal(err)
	}
	if len(provider.seen) < 2 || provider.seen[len(provider.seen)-1].Content != "intercepted" {
		t.Fatalf("messages=%#v", provider.seen)
	}
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

type inputBudgetExploreProvider struct{}

func (inputBudgetExploreProvider) Next(_ context.Context, _ []agent.Message, _ []string) (agent.Response, error) {
	return agent.Response{Text: "answer", StopReason: "stop", Usage: agent.Usage{Input: 200001}}, nil
}

func TestExploreStopsWhenQuickScanInputBudgetIsConsumed(t *testing.T) {
	answer, err := newExploreTool(t.TempDir(), inputBudgetExploreProvider{}).Execute(context.Background(), map[string]any{"question": "map it"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "INCOMPLETE:") || !strings.Contains(answer, "~200K in, 1/8 turns") {
		t.Fatalf("answer=%q", answer)
	}
}

type inputBudgetToolExploreProvider struct{ calls int }

func (p *inputBudgetToolExploreProvider) Next(_ context.Context, _ []agent.Message, _ []string) (agent.Response, error) {
	p.calls++
	return agent.Response{ToolCalls: []agent.ToolCall{{ID: "scan", Name: "ls"}}, StopReason: "toolUse", Usage: agent.Usage{Input: 200001}}, nil
}

func TestExploreStopsAfterBudgetedToolTurn(t *testing.T) {
	provider := &inputBudgetToolExploreProvider{}
	answer, err := newExploreTool(t.TempDir(), provider).Execute(context.Background(), map[string]any{"question": "map it"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "INCOMPLETE:") || !strings.Contains(answer, "~200K in, 1/8 turns") {
		t.Fatalf("answer=%q", answer)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls=%d, want 1", provider.calls)
	}
}

func TestExploreAddsFooterWhenBudgetFooterIsMalformed(t *testing.T) {
	provider := malformedFooterExploreProvider{}
	answer, err := newExploreTool(t.TempDir(), provider).Execute(context.Background(), map[string]any{"question": "map it"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "nope/8 turns") || !strings.HasSuffix(answer, "1/8 turns") {
		t.Fatalf("answer=%q", answer)
	}
}

type malformedFooterExploreProvider struct{}

func (malformedFooterExploreProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: "answer\n~1K in, ~nope/8 turns", StopReason: "stop"}, nil
}
