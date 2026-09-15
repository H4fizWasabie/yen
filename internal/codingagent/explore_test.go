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
	if lines := len(strings.Split(answer, "\n")); lines > 31 {
		t.Fatalf("lines=%d answer=%q", lines, answer)
	}
	for _, name := range provider.tools {
		if name != "read" && name != "grep" && name != "find" && name != "ls" {
			t.Fatalf("non-read-only tool exposed: %q", name)
		}
	}
}
