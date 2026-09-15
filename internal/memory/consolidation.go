package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
)

const ConsolidationTurnCeiling = 70
const MaxConsolidationTranscriptChars = 100000
const consolidationFailureCooldown = 15 * time.Minute
const consolidationMaxRetries = 3

var consolidationRetryDelay = 2 * time.Second

var consolidationTriggerPhrases = []string{"thanks", "thank you", "great job", "good work", "nice work", "perfect", "awesome", "that's all", "all done"}

func ShouldTriggerConsolidation(userMessage string, turnsSinceCheckpoint int) bool {
	lower := strings.ToLower(userMessage)
	for _, phrase := range consolidationTriggerPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return turnsSinceCheckpoint >= ConsolidationTurnCeiling
}

type ConsolidationTurn struct {
	Role      string
	Content   string
	Timestamp string
}

func (e *Engine) Consolidate(ctx context.Context, provider agent.Provider, turnID, conversationID, workspaceID, adapter string, turns []ConsolidationTurn) error {
	if provider == nil {
		return errors.New("consolidation provider is required")
	}
	if len(turns) == 0 {
		return errors.New("consolidation turns are required")
	}
	prompt, err := e.consolidationPrompt(turns, Context{WorkspaceID: workspaceID, ConversationID: conversationID})
	if err != nil {
		return err
	}
	response, err := retryConsolidationCall(ctx, provider, []agent.Message{{Role: "user", Content: prompt}})
	if err != nil {
		return err
	}
	if response.StopReason == "error" || response.StopReason == "aborted" {
		return fmt.Errorf("consolidation stopped: %s", response.StopReason)
	}
	result, err := parseConsolidationResponse(response.Text)
	if err != nil {
		return err
	}
	if result.Episode.StartedAt == "" {
		result.Episode.StartedAt = turns[0].Timestamp
	}
	if result.Episode.EndedAt == "" {
		result.Episode.EndedAt = turns[len(turns)-1].Timestamp
	}
	return e.ApplyConsolidation(turnID, conversationID, workspaceID, adapter, result.Facts, result.Edges, result.Episode)
}

func retryConsolidationCall(ctx context.Context, provider agent.Provider, messages []agent.Message) (agent.Response, error) {
	for attempt := 0; ; attempt++ {
		response, err := provider.Next(ctx, messages, nil)
		if err == nil || attempt >= consolidationMaxRetries {
			return response, err
		}
		delay := consolidationRetryDelay * time.Duration(1<<attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return agent.Response{}, ctx.Err()
		case <-timer.C:
		}
	}
}

// ConsolidateIfTriggered runs the pinned completion/ceiling trigger without
// reusing the durable turn checkpoint. Failed background work is recorded for
// cooldown and returned to the caller for logging; it must not fail the turn.
func (e *Engine) ConsolidateIfTriggered(ctx context.Context, provider agent.Provider, turnID, conversationID, workspaceID, adapter, userMessage string, turns []ConsolidationTurn) (bool, error) {
	if e == nil || e.ConsolidationCheckpoints == nil {
		return false, nil
	}
	checkpoint := e.ConsolidationCheckpoints.Get(conversationID)
	if checkpoint.LastFailureAt != "" {
		if failedAt, err := time.Parse(time.RFC3339Nano, checkpoint.LastFailureAt); err == nil && time.Since(failedAt) < consolidationFailureCooldown {
			return false, nil
		}
	}
	if !ShouldTriggerConsolidation(userMessage, len(turns)) {
		return false, nil
	}
	if len(turns) > ConsolidationTurnCeiling {
		turns = turns[len(turns)-ConsolidationTurnCeiling:]
	}
	if err := e.Consolidate(ctx, provider, turnID, conversationID, workspaceID, adapter, turns); err != nil {
		_ = e.ConsolidationCheckpoints.Set(conversationID, Checkpoint{LastEntryID: checkpoint.LastEntryID, LastFailureAt: time.Now().UTC().Format(time.RFC3339Nano)})
		return true, err
	}
	return true, e.ConsolidationCheckpoints.Set(conversationID, Checkpoint{LastEntryID: turnID})
}

func (e *Engine) consolidationPrompt(turns []ConsolidationTurn, ctx Context) (string, error) {
	if e == nil || e.Semantic == nil {
		return "", errors.New("semantic memory is not configured")
	}
	var transcript strings.Builder
	for _, turn := range turns {
		if strings.TrimSpace(turn.Content) == "" {
			continue
		}
		fmt.Fprintf(&transcript, "%s: %s\n", turn.Role, strings.TrimSpace(turn.Content))
	}
	if transcript.Len() == 0 {
		return "", errors.New("consolidation turns are empty")
	}
	transcriptText := capConsolidationTranscript(transcript.String())
	existingNodes, err := e.Semantic.Remember(transcriptText, ctx)
	if err != nil {
		return "", err
	}
	var existing strings.Builder
	for _, node := range existingNodes {
		fmt.Fprintf(&existing, "- %s: %s\n", node.ID, node.Subject)
	}
	if existing.Len() == 0 {
		existing.WriteString("- none\n")
	}
	return "You are a memory consolidation pass. Extract durable facts from the conversation and return only JSON with this shape: " +
		`{"facts":[{"id":"f1","subject":"...","body":"..."}],"edges":[{"from":"f1","to":"f2","rel":"depends_on"}],"episode":{"summary":"...","startedAt":"ISO-8601","endedAt":"ISO-8601","relatedFactIds":["f1"]}}` +
		". Existing nodes:\n" + existing.String() + "Conversation:\n" + transcriptText, nil
}

func capConsolidationTranscript(text string) string {
	runes := []rune(text)
	if len(runes) <= MaxConsolidationTranscriptChars {
		return text
	}
	return string(runes[len(runes)-MaxConsolidationTranscriptChars:])
}

func parseConsolidationResponse(raw string) (struct {
	Facts   []ConsolidatedFact
	Edges   []ConsolidatedEdge
	Episode ConsolidatedEpisode
}, error) {
	var result struct {
		Facts   []ConsolidatedFact `json:"facts"`
		Edges   []ConsolidatedEdge `json:"edges"`
		Episode struct {
			Summary        string   `json:"summary"`
			StartedAt      string   `json:"startedAt"`
			EndedAt        string   `json:"endedAt"`
			RelatedFactIDs []string `json:"relatedFactIds"`
			RelatedNodeIDs []string `json:"relatedSemanticNodeIds"`
		} `json:"episode"`
	}
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		if newline := strings.IndexByte(cleaned, '\n'); newline >= 0 {
			cleaned = strings.TrimSpace(cleaned[newline+1:])
		}
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
	}
	if start, end := strings.IndexByte(cleaned, '{'), strings.LastIndexByte(cleaned, '}'); start >= 0 && end > start {
		cleaned = cleaned[start : end+1]
	}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return struct {
			Facts   []ConsolidatedFact
			Edges   []ConsolidatedEdge
			Episode ConsolidatedEpisode
		}{}, fmt.Errorf("consolidation JSON: %w", err)
	}
	if strings.TrimSpace(result.Episode.Summary) == "" {
		return struct {
			Facts   []ConsolidatedFact
			Edges   []ConsolidatedEdge
			Episode ConsolidatedEpisode
		}{}, errors.New("consolidation episode summary is required")
	}
	related := result.Episode.RelatedFactIDs
	if len(related) == 0 {
		related = result.Episode.RelatedNodeIDs
	}
	return struct {
		Facts   []ConsolidatedFact
		Edges   []ConsolidatedEdge
		Episode ConsolidatedEpisode
	}{Facts: result.Facts, Edges: result.Edges, Episode: ConsolidatedEpisode{Summary: result.Episode.Summary, StartedAt: result.Episode.StartedAt, EndedAt: result.Episode.EndedAt, RelatedSemanticNodeIDs: related}}, nil
}
