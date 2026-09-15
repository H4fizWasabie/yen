package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
)

type backfillProvider struct{}

func (backfillProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: `{"facts":[],"edges":[],"episode":{"summary":"historical session","startedAt":"2026-01-01T00:00:00Z","endedAt":"2026-01-01T00:01:00Z"}}`, StopReason: "stop"}, nil
}

func TestBackfillFromSessionLogUsesBoundedPipelineWithoutLiveCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "historical.jsonl")
	legacy := session.New(path, session.Header{ID: "session-1", ConversationID: "conv-1", WorkspaceID: "work-1", Channel: "telegram"})
	if _, err := legacy.Append(session.Message{Role: "user", Content: "What changed?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Append(session.Message{Role: "assistant", Content: "The deployment changed."}); err != nil {
		t.Fatal(err)
	}

	engine, err := OpenEngine(filepath.Join(dir, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	count, err := engine.BackfillFromSessionLog(context.Background(), backfillProvider{}, path, "", "")
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if checkpoint := engine.ConsolidationCheckpoints.Get("conv-1"); checkpoint.LastEntryID != "" {
		t.Fatalf("backfill advanced live checkpoint: %#v", checkpoint)
	}
	episodes, err := engine.Episodic.Recent("conv-1", 8)
	if err != nil || len(episodes) != 1 || episodes[0].Summary != "historical session" || episodes[0].Channel != "telegram" {
		t.Fatalf("episodes=%#v err=%v", episodes, err)
	}
}
