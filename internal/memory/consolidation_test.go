package memory

import (
	"context"
	"errors"
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

type captureConsolidationProvider struct {
	text string
	seen string
}

type retryConsolidationProvider struct{ calls int }

func (p *retryConsolidationProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	p.calls++
	if p.calls == 1 {
		return agent.Response{}, errors.New("temporary consolidation provider failure")
	}
	return agent.Response{Text: `{"episode":{"summary":"recovered","startedAt":"2026-01-01T00:00:00Z","endedAt":"2026-01-01T00:00:01Z"}}`, StopReason: "stop"}, nil
}

type jsonModeConsolidationProvider struct{ used bool }

func (p *jsonModeConsolidationProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{}, errors.New("plain consolidation call used")
}

func (p *jsonModeConsolidationProvider) NextJSON(context.Context, []agent.Message, []string) (agent.Response, error) {
	p.used = true
	return agent.Response{Text: `{"episode":{"summary":"structured","startedAt":"2026-01-01T00:00:00Z","endedAt":"2026-01-01T00:00:01Z"}}`, StopReason: "stop"}, nil
}

func (p *captureConsolidationProvider) Next(_ context.Context, messages []agent.Message, _ []string) (agent.Response, error) {
	p.seen = messages[0].Content
	return agent.Response{Text: p.text, StopReason: "stop"}, nil
}

func TestEngineConsolidateParsesAndAppliesStructuredResult(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	provider := consolidationProvider{text: "```json\n{\"facts\":[{\"id\":\"f1\",\"subject\":\"User prefers short answers\"}],\"edges\":[],\"episode\":{\"summary\":\"Recorded a response preference.\",\"startedAt\":\"2026-01-01T00:00:00Z\",\"endedAt\":\"2026-01-01T00:00:01Z\",\"relatedFactIds\":[\"f1\"]}}\n```"}
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

func TestEngineConsolidateRequiresEpisodeTimestamps(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	err = engine.Consolidate(context.Background(), consolidationProvider{text: `{"episode":{"summary":"missing times"}}`}, "turn-time", "conv-time", "work", "cli", []ConsolidationTurn{{Role: "user", Content: "hello"}})
	if err == nil || !strings.Contains(err.Error(), "timestamps are required") {
		t.Fatalf("err=%v", err)
	}
	if got := engine.Checkpoints.Get("conv-time").LastEntryID; got != "" {
		t.Fatalf("checkpoint advanced to %q", got)
	}
}

func TestEngineConsolidatesOnlyWhenTriggeredAndTracksSeparateState(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	provider := consolidationProvider{text: `{"facts":[{"id":"f1","subject":"User prefers concise replies"}],"episode":{"summary":"Recorded a preference.","startedAt":"2026-01-01T00:00:00Z","endedAt":"2026-01-01T00:00:01Z"}}`}
	triggered, err := engine.ConsolidateIfTriggered(context.Background(), provider, "turn-1", "conv-1", "work-1", "telegram", "Thanks, that is all", []ConsolidationTurn{{Role: "user", Content: "Keep it concise."}})
	if err != nil || !triggered {
		t.Fatalf("triggered=%v err=%v", triggered, err)
	}
	if got := engine.ConsolidationCheckpoints.Get("conv-1").LastEntryID; got != "turn-1" {
		t.Fatalf("consolidation checkpoint=%q", got)
	}
	if got := engine.Checkpoints.Get("conv-1").LastEntryID; got != "turn-1" {
		t.Fatalf("durable checkpoint=%q", got)
	}
	if again, err := engine.ConsolidateIfTriggered(context.Background(), provider, "turn-2", "conv-1", "work-1", "telegram", "keep working", []ConsolidationTurn{{Role: "user", Content: "another turn"}}); err != nil || again {
		t.Fatalf("untriggered follow-up=%v err=%v", again, err)
	}
}

func TestEngineConsolidationCapsTriggeredWindow(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	provider := &captureConsolidationProvider{text: `{"episode":{"summary":"window","startedAt":"2026-01-01T00:00:00Z","endedAt":"2026-01-01T00:00:01Z"}}`}
	turns := make([]ConsolidationTurn, ConsolidationTurnCeiling+5)
	for i := range turns {
		turns[i] = ConsolidationTurn{Role: "user", Content: "turn"}
	}
	triggered, err := engine.ConsolidateIfTriggered(context.Background(), provider, "turn-ceiling", "conv-ceiling", "work", "cli", "keep working", turns)
	if err != nil || !triggered {
		t.Fatalf("triggered=%v err=%v", triggered, err)
	}
	if got := strings.Count(provider.seen, "user: turn"); got != ConsolidationTurnCeiling {
		t.Fatalf("prompt turns=%d, want %d", got, ConsolidationTurnCeiling)
	}
}

func TestConsolidationPromptCapsTranscriptCharacters(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	provider := &captureConsolidationProvider{text: `{"episode":{"summary":"window","startedAt":"2026-01-01T00:00:00Z","endedAt":"2026-01-01T00:00:01Z"}}`}
	turns := []ConsolidationTurn{{Role: "user", Content: strings.Repeat("x", MaxConsolidationTranscriptChars+1000)}}
	if err := engine.Consolidate(context.Background(), provider, "turn-size", "conv-size", "work", "cli", turns); err != nil {
		t.Fatal(err)
	}
	if len(provider.seen) > MaxConsolidationTranscriptChars+2000 || !strings.Contains(provider.seen, strings.Repeat("x", 100)) {
		t.Fatalf("consolidation prompt length=%d", len(provider.seen))
	}
}

func TestConsolidationRetriesProviderFailureOnlyWithinConsolidation(t *testing.T) {
	oldDelay := consolidationRetryDelay
	consolidationRetryDelay = 0
	defer func() { consolidationRetryDelay = oldDelay }()
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	provider := &retryConsolidationProvider{}
	if err := engine.Consolidate(context.Background(), provider, "turn-retry", "conv-retry", "work", "cli", []ConsolidationTurn{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls=%d, want 2", provider.calls)
	}
}

func TestConsolidationUsesOptionalJSONProviderMode(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	provider := &jsonModeConsolidationProvider{}
	if err := engine.Consolidate(context.Background(), provider, "turn-json", "conv-json", "work", "cli", []ConsolidationTurn{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatal(err)
	}
	if !provider.used {
		t.Fatal("structured provider mode was not used")
	}
}
