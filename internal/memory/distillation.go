package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	providerpkg "github.com/H4fizWasabie/yen/internal/provider"
)

type DistilledFact struct {
	Fact       string
	Confidence float64
}

type DistilledMemory struct {
	Facts   []DistilledFact
	Episode string
}

var distillationRetryDelay = 2 * time.Second

// DistillMemory extracts durable facts from messages that are leaving context.
// It mirrors Theoses' best-effort compaction safety net: provider failures return
// an error to the caller, while a valid response keeps only high-confidence facts.
//
// reserveTokens mirrors the oracle's distillMemory (compaction.ts:816-848),
// which bounds the call's output tokens to 80% of reserveTokens so a
// best-effort background extraction can't itself blow past the context
// budget it's meant to protect. Pass 0 to leave the provider unbounded.
func DistillMemory(ctx context.Context, provider agent.Provider, turns []ConsolidationTurn, reserveTokens int) (DistilledMemory, error) {
	if provider == nil {
		return DistilledMemory{}, errors.New("distillation provider is required")
	}
	if len(turns) == 0 {
		return DistilledMemory{}, errors.New("distillation turns are required")
	}
	var transcript strings.Builder
	for _, turn := range turns {
		if text := strings.TrimSpace(turn.Content); text != "" {
			fmt.Fprintf(&transcript, "%s: %s\n", turn.Role, text)
		}
	}
	if transcript.Len() == 0 {
		return DistilledMemory{}, errors.New("distillation turns are empty")
	}
	if reserveTokens > 0 {
		if bounded, err := providerpkg.SetMaxTokens(provider, int(0.8*float64(reserveTokens))); err == nil {
			provider = bounded
		}
	}
	prompt := "Extract durable memory from this conversation. Return only JSON: " +
		`[{"fact":"one durable fact","confidence":0.0},{"episode":"one sentence describing this batch"}]` +
		" Keep only facts worth remembering in a month; confidence must be at least 0.85.\n<conversation>\n" +
		transcript.String() + "</conversation>"
	response, err := retryDistillationCall(ctx, provider, []agent.Message{{Role: "user", Content: prompt}})
	if err != nil {
		return DistilledMemory{}, err
	}
	if response.StopReason == "error" || response.StopReason == "aborted" {
		return DistilledMemory{}, fmt.Errorf("distillation stopped: %s", response.StopReason)
	}
	return ParseDistillationResponse(response.Text)
}

func retryDistillationCall(ctx context.Context, provider agent.Provider, messages []agent.Message) (agent.Response, error) {
	for attempt := 0; ; attempt++ {
		response, err := provider.Next(ctx, messages, nil)
		if err == nil || attempt >= consolidationMaxRetries || !providerpkg.IsRetryableProviderError(err.Error()) {
			return response, err
		}
		timer := time.NewTimer(distillationRetryDelay * time.Duration(1<<attempt))
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return agent.Response{}, ctx.Err()
		}
	}
}

func ParseDistillationResponse(raw string) (DistilledMemory, error) {
	var value any
	if parsed, err := parseStructuredJSON(raw, "Memory distillation"); err != nil {
		return DistilledMemory{}, fmt.Errorf("distillation JSON: %w", err)
	} else {
		value = parsed
	}
	result := DistilledMemory{}
	var values []any
	switch object := value.(type) {
	case []any:
		values = object
	case map[string]any:
		if facts, ok := object["facts"].([]any); ok {
			values = facts
		}
		if episode, ok := object["episode"].(string); ok {
			result.Episode = strings.TrimSpace(episode)
		}
	default:
		return DistilledMemory{}, errors.New("distillation response was not an array or object")
	}
	for _, item := range values {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if episode, ok := object["episode"].(string); ok && result.Episode == "" {
			result.Episode = strings.TrimSpace(episode)
		}
		fact, factOK := object["fact"].(string)
		confidence, confidenceOK := object["confidence"].(float64)
		if factOK && confidenceOK && confidence >= 0.85 && strings.TrimSpace(fact) != "" {
			result.Facts = append(result.Facts, DistilledFact{Fact: strings.TrimSpace(fact), Confidence: confidence})
		}
	}
	return result, nil
}
