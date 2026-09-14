package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/memory"
)

type recallTurnsTool struct {
	history []agent.Message
}

func (recallTurnsTool) Name() string { return "recall_turns" }

func (t recallTurnsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", errors.New("query is required")
	}
	type hit struct {
		role, text string
		score      int
		index      int
	}
	var hits []hit
	for index, message := range t.history {
		if message.Role != "user" && message.Role != "assistant" || strings.TrimSpace(message.Content) == "" {
			continue
		}
		if score := memory.MatchScore(query, message.Content); score > 0 {
			hits = append(hits, hit{role: message.Role, text: strings.TrimSpace(message.Content), score: score, index: index})
		}
	}
	if len(hits) == 0 {
		return "No matching turns found in this session.", nil
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].index > hits[j].index
	})
	if len(hits) > 5 {
		hits = hits[:5]
	}
	var out strings.Builder
	for _, item := range hits {
		text := strings.Join(strings.Fields(item.text), " ")
		if len(text) > 300 {
			text = text[:297] + "..."
		}
		fmt.Fprintf(&out, "%s: %s\n", item.role, text)
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}
