package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type consolidationProvider struct {
	text string
}

func (p consolidationProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: p.text, StopReason: "stop"}, nil
}

func TestEngineConsolidateParsesAndAppliesStructuredResult(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	provider := consolidationProvider{text: "```json\n{\"facts\":[{\"id\":\"f1\",\"subject\":\"User prefers short answers\"}],\"edges\":[],\"episode\":{\"summary\":\"Recorded a response preference.\",\"relatedFactIds\":[\"f1\"]}}\n```"}
	if err := engine.Consolidate(context.Background(), provider, "turn-c", "conv-c", "work-c", "cli", []ConsolidationTurn{{Role: "user", Content: "Please keep replies short."}}); err != nil {
		t.Fatal(err)
	}
	hits, err := engine.Remember("short answers", Context{WorkspaceID: "work-c", ConversationID: "conv-c"})
	if err != nil || len(hits) != 1 || hits[0].Subject != "User prefers short answers" {
		t.Fatalf("hits=%#v err=%v", hits, err)
	}
	episodes, err := engine.Episodic.Recent("conv-c", 8)
	if err != nil || len(episodes) != 1 || len(episodes[0].RelatedSemanticNodeIDs) != 1 {
		t.Fatalf("episodes=%#v err=%v", episodes, err)
	}
	if got := engine.Checkpoints.Get("conv-c").LastEntryID; got != "turn-c" {
		t.Fatalf("checkpoint=%q", got)
	}
}

func TestEngineConsolidateRejectsInvalidResultWithoutCheckpoint(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	err = engine.Consolidate(context.Background(), consolidationProvider{text: "not json"}, "turn-bad", "conv-bad", "work", "cli", []ConsolidationTurn{{Role: "user", Content: "hello"}})
	if err == nil || !strings.Contains(err.Error(), "consolidation JSON") {
		t.Fatalf("err=%v", err)
	}
	if got := engine.Checkpoints.Get("conv-bad").LastEntryID; got != "" {
		t.Fatalf("checkpoint advanced to %q", got)
	}
	if episodes, err := engine.Episodic.Recent("conv-bad", 8); err != nil || len(episodes) != 0 {
		t.Fatalf("episodes=%#v err=%v", episodes, err)
	}
}
