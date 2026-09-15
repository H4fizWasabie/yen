package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
)

// BackfillFromSessionLog runs the same bounded consolidation pipeline as live
// turns over a historical session branch. It is explicit and never advances
// the live consolidation checkpoint.
func (e *Engine) BackfillFromSessionLog(ctx context.Context, provider agent.Provider, path, workspaceID, adapter string) (int, error) {
	if e == nil || provider == nil {
		return 0, errors.New("memory engine and provider are required")
	}
	legacy, err := session.Open(path)
	if err != nil {
		return 0, err
	}
	header := legacy.Header()
	conversationID := strings.TrimSpace(header.ConversationID)
	if conversationID == "" {
		conversationID = header.ID
	}
	if conversationID == "" {
		return 0, errors.New("historical session has no conversation ID")
	}
	if workspaceID == "" {
		workspaceID = header.WorkspaceID
	}
	if adapter == "" {
		adapter = header.Channel
	}
	timed := legacy.TimedMessages()
	turns := make([]ConsolidationTurn, 0, len(timed))
	for _, message := range timed {
		content := historicalTurnText(message.Message)
		if strings.TrimSpace(content) == "" {
			continue
		}
		turns = append(turns, ConsolidationTurn{Role: message.Message.Role, Content: content, Timestamp: message.Timestamp})
	}
	if len(turns) == 0 {
		return 0, nil
	}
	if !e.beginConsolidation(conversationID) {
		return 0, errors.New("memory consolidation already active")
	}
	defer e.endConsolidation(conversationID)
	for start := 0; start < len(turns); start += ConsolidationTurnCeiling {
		end := start + ConsolidationTurnCeiling
		if end > len(turns) {
			end = len(turns)
		}
		chunkID := backfillChunkID(path, start)
		if err := e.consolidateWithCheckpoint(ctx, provider, chunkID, conversationID, workspaceID, adapter, turns[start:end], false); err != nil {
			return start, err
		}
	}
	return len(turns), nil
}

func backfillChunkID(path string, start int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", path, start)))
	return "backfill-" + hex.EncodeToString(digest[:8])
}

func historicalTurnText(message session.Message) string {
	if message.Role == "bashExecution" {
		status := fmt.Sprintf("exit %v", message.ExitCode)
		if message.Cancelled {
			status = "cancelled"
		}
		return fmt.Sprintf("ran `%s` — %s: %s", message.Command, status, message.Output)
	}
	if message.Role == "branchSummary" || message.Role == "compactionSummary" {
		return message.Summary
	}
	return historicalMessageText(message.Content)
}

func historicalMessageText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []session.ContentPart:
		var text strings.Builder
		for _, part := range value {
			if part.Type == "toolCall" {
				fmt.Fprintf(&text, "called %s(%v)", part.Name, part.Arguments)
			} else if part.Type != "thinking" {
				text.WriteString(part.Text)
			}
		}
		return text.String()
	case []any:
		var text strings.Builder
		for _, item := range value {
			if part, ok := item.(map[string]any); ok {
				if part["type"] == "toolCall" {
					fmt.Fprintf(&text, "called %v(%v)", part["name"], part["arguments"])
				} else if part["type"] != "thinking" {
					if value, ok := part["text"].(string); ok {
						text.WriteString(value)
					}
				}
			}
		}
		return text.String()
	default:
		return ""
	}
}
