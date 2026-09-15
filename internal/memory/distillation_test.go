package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type distillationProvider struct{ text string }

func (p distillationProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: p.text, StopReason: "stop"}, nil
}

type retryDistillationProvider struct{ calls int }

func (p *retryDistillationProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	p.calls++
	if p.calls == 1 {
		return agent.Response{}, errors.New("temporary overloaded_error")
	}
	return agent.Response{Text: `[{"fact":"prefers Go","confidence":0.9}]`, StopReason: "stop"}, nil
}

func TestDistillMemoryRetriesTransientProviderFailure(t *testing.T) {
	previous := distillationRetryDelay
	distillationRetryDelay = 0
	t.Cleanup(func() { distillationRetryDelay = previous })
	provider := &retryDistillationProvider{}
	result, err := DistillMemory(context.Background(), provider, []ConsolidationTurn{{Role: "user", Content: "I prefer Go"}})
	if err != nil || provider.calls != 2 || len(result.Facts) != 1 {
		t.Fatalf("calls=%d result=%#v err=%v", provider.calls, result, err)
	}
}

func TestParseDistillationResponseFiltersConfidenceAndAcceptsEpisodeArray(t *testing.T) {
	result, err := ParseDistillationResponse("```json\n[{\"fact\":\"likes Go\",\"confidence\":0.9},{\"fact\":\"guess\",\"confidence\":0.4},{\"episode\":\"Discussed Go\"}]\n```")
	if err != nil || len(result.Facts) != 1 || result.Facts[0].Fact != "likes Go" || result.Episode != "Discussed Go" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestParseDistillationResponseAcceptsObjectFactsAndRepairsJSON(t *testing.T) {
	result, err := ParseDistillationResponse(`{"facts":[{"fact":"uses Yen","confidence":0.85,}],"episode":"Built Yen"}`)
	if err != nil || len(result.Facts) != 1 || result.Episode != "Built Yen" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDistillMemoryUsesProvider(t *testing.T) {
	result, err := DistillMemory(context.Background(), distillationProvider{text: `[{"fact":"prefers Telegram","confidence":0.95}]`}, []ConsolidationTurn{{Role: "user", Content: "I prefer Telegram"}})
	if err != nil || len(result.Facts) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
