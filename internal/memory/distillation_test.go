package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	providerpkg "github.com/H4fizWasabie/yen/internal/provider"
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
	result, err := DistillMemory(context.Background(), provider, []ConsolidationTurn{{Role: "user", Content: "I prefer Go"}}, 0)
	if err != nil || provider.calls != 2 || len(result.Facts) != 1 {
		t.Fatalf("calls=%d result=%#v err=%v", provider.calls, result, err)
	}
}

// TestDistillMemoryBoundsOutputTokensForSupportedProviders matches the
// oracle's distillMemory (compaction.ts:816-848), which bounds the
// distillation call's output tokens to 80% of reserveTokens. A provider
// that supports max-tokens control (OpenAICompletions) must receive that
// bound before the call.
func TestDistillMemoryBoundsOutputTokensForSupportedProviders(t *testing.T) {
	var seenMaxTokens int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			MaxTokens int `json:"max_completion_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		seenMaxTokens = payload.MaxTokens
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":`+`"[{\"fact\":\"prefers Go\",\"confidence\":0.9}]"`+`},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client := providerpkg.NewOpenAICompletions(server.URL, "", "test-model")
	if _, err := DistillMemory(context.Background(), client, []ConsolidationTurn{{Role: "user", Content: "I prefer Go"}}, 16384); err != nil {
		t.Fatal(err)
	}
	if seenMaxTokens != 13107 {
		t.Fatalf("seenMaxTokens = %d, want floor(0.8 * 16384) = 13107", seenMaxTokens)
	}
}

// TestDistillMemoryLeavesProviderUnboundedWhenReserveTokensIsUnset preserves
// existing behavior for deployments that never configure a compaction
// context-window budget (AutoCompactReserveTokens stays at its Go zero
// value): distillation must still work, uncapped.
func TestDistillMemoryLeavesProviderUnboundedWhenReserveTokensIsUnset(t *testing.T) {
	var seenMaxTokens int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			MaxTokens int `json:"max_completion_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		seenMaxTokens = payload.MaxTokens
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":`+`"[{\"fact\":\"prefers Go\",\"confidence\":0.9}]"`+`},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client := providerpkg.NewOpenAICompletions(server.URL, "", "test-model")
	result, err := DistillMemory(context.Background(), client, []ConsolidationTurn{{Role: "user", Content: "I prefer Go"}}, 0)
	if err != nil || len(result.Facts) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if seenMaxTokens != 0 {
		t.Fatalf("seenMaxTokens = %d, want 0 (uncapped)", seenMaxTokens)
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
	result, err := DistillMemory(context.Background(), distillationProvider{text: `[{"fact":"prefers Telegram","confidence":0.95}]`}, []ConsolidationTurn{{Role: "user", Content: "I prefer Telegram"}}, 0)
	if err != nil || len(result.Facts) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
