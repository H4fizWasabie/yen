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

type jsonConsolidationProvider interface {
	NextJSON(context.Context, []agent.Message, []string) (agent.Response, error)
}

func (e *Engine) Consolidate(ctx context.Context, provider agent.Provider, turnID, conversationID, workspaceID, adapter string, turns []ConsolidationTurn) error {
	if !e.beginConsolidation(conversationID) {
		return errors.New("consolidation already active")
	}
	defer e.endConsolidation(conversationID)
	return e.consolidate(ctx, provider, turnID, conversationID, workspaceID, adapter, turns)
}

func (e *Engine) consolidate(ctx context.Context, provider agent.Provider, turnID, conversationID, workspaceID, adapter string, turns []ConsolidationTurn) error {
	return e.consolidateWithCheckpoint(ctx, provider, turnID, conversationID, workspaceID, adapter, turns, true)
}

func (e *Engine) consolidateWithCheckpoint(ctx context.Context, provider agent.Provider, turnID, conversationID, workspaceID, adapter string, turns []ConsolidationTurn, writeCheckpoint bool) error {
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
	return e.applyConsolidationWithCheckpoint(turnID, conversationID, workspaceID, adapter, result.Facts, result.Edges, result.Episode, writeCheckpoint)
}

func (e *Engine) beginConsolidation(conversationID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inFlight == nil {
		e.inFlight = make(map[string]bool)
	}
	if e.inFlight[conversationID] {
		return false
	}
	e.inFlight[conversationID] = true
	return true
}

func (e *Engine) endConsolidation(conversationID string) {
	e.mu.Lock()
	delete(e.inFlight, conversationID)
	e.mu.Unlock()
}

func retryConsolidationCall(ctx context.Context, provider agent.Provider, messages []agent.Message) (agent.Response, error) {
	for attempt := 0; ; attempt++ {
		var response agent.Response
		var err error
		if structured, ok := provider.(jsonConsolidationProvider); ok {
			response, err = structured.NextJSON(ctx, messages, nil)
		} else {
			response, err = provider.Next(ctx, messages, nil)
		}
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
	if !e.beginConsolidation(conversationID) {
		return false, nil
	}
	defer e.endConsolidation(conversationID)
	if len(turns) > ConsolidationTurnCeiling {
		turns = turns[len(turns)-ConsolidationTurnCeiling:]
	}
	if err := e.consolidate(ctx, provider, turnID, conversationID, workspaceID, adapter, turns); err != nil {
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
		if strings.TrimSpace(turn.Timestamp) != "" {
			fmt.Fprintf(&transcript, "[%s] %s: %s\n", turn.Timestamp, turn.Role, strings.TrimSpace(turn.Content))
		} else {
			fmt.Fprintf(&transcript, "%s: %s\n", turn.Role, strings.TrimSpace(turn.Content))
		}
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
	type episodePayload struct {
		Summary        string            `json:"summary"`
		StartedAt      string            `json:"startedAt"`
		EndedAt        string            `json:"endedAt"`
		RelatedFactIDs []json.RawMessage `json:"relatedFactIds"`
		RelatedNodeIDs []json.RawMessage `json:"relatedSemanticNodeIds"`
	}
	var envelope struct {
		Facts   []json.RawMessage `json:"facts"`
		Edges   []json.RawMessage `json:"edges"`
		Episode json.RawMessage   `json:"episode"`
	}
	if err := decodeStructuredJSON(raw, "Memory consolidation", &envelope); err != nil {
		return struct {
			Facts   []ConsolidatedFact
			Edges   []ConsolidatedEdge
			Episode ConsolidatedEpisode
		}{}, fmt.Errorf("consolidation JSON: %w", err)
	}
	if len(envelope.Episode) == 0 || string(envelope.Episode) == "null" {
		return struct {
			Facts   []ConsolidatedFact
			Edges   []ConsolidatedEdge
			Episode ConsolidatedEpisode
		}{}, errors.New("consolidation response is missing an episode")
	}
	var episode episodePayload
	if err := json.Unmarshal(envelope.Episode, &episode); err != nil {
		return struct {
			Facts   []ConsolidatedFact
			Edges   []ConsolidatedEdge
			Episode ConsolidatedEpisode
		}{}, errors.New("consolidation response is missing an episode")
	}
	var episodeFields map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Episode, &episodeFields); err != nil {
		return struct {
			Facts   []ConsolidatedFact
			Edges   []ConsolidatedEdge
			Episode ConsolidatedEpisode
		}{}, errors.New("consolidation response's episode is missing required fields")
	}
	for _, field := range []string{"summary", "startedAt", "endedAt"} {
		value, ok := episodeFields[field]
		var text string
		if !ok || json.Unmarshal(value, &text) != nil {
			return struct {
				Facts   []ConsolidatedFact
				Edges   []ConsolidatedEdge
				Episode ConsolidatedEpisode
			}{}, errors.New("consolidation response's episode is missing required fields")
		}
	}
	var facts []ConsolidatedFact
	for _, rawFact := range envelope.Facts {
		var fact struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
		}
		if json.Unmarshal(rawFact, &fact) == nil {
			facts = append(facts, ConsolidatedFact{ID: fact.ID, Subject: fact.Subject, Body: fact.Body})
		}
	}
	var edges []ConsolidatedEdge
	for _, rawEdge := range envelope.Edges {
		var edge struct {
			From string `json:"from"`
			To   string `json:"to"`
			Rel  string `json:"rel"`
		}
		if json.Unmarshal(rawEdge, &edge) == nil {
			if _, ok := allowedEdgeRelations[edge.Rel]; ok {
				edges = append(edges, ConsolidatedEdge{From: edge.From, To: edge.To, Rel: edge.Rel})
			}
		}
	}
	var related []string
	for _, rawID := range episode.RelatedFactIDs {
		var id string
		if json.Unmarshal(rawID, &id) == nil {
			related = append(related, id)
		}
	}
	if len(related) == 0 {
		for _, rawID := range episode.RelatedNodeIDs {
			var id string
			if json.Unmarshal(rawID, &id) == nil {
				related = append(related, id)
			}
		}
	}
	return struct {
		Facts   []ConsolidatedFact
		Edges   []ConsolidatedEdge
		Episode ConsolidatedEpisode
	}{Facts: facts, Edges: edges, Episode: ConsolidatedEpisode{Summary: episode.Summary, StartedAt: episode.StartedAt, EndedAt: episode.EndedAt, RelatedSemanticNodeIDs: related}}, nil
}

// stripTrailingCommas repairs the near-miss JSON commonly emitted by models
// without changing commas that occur inside string values.
func stripTrailingCommas(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	inString, escaped := false, false
	for i := 0; i < len(text); i++ {
		char := text[i]
		if inString {
			out.WriteByte(char)
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			out.WriteByte(char)
			continue
		}
		if char == ',' {
			j := i + 1
			for j < len(text) && strings.ContainsRune(" \t\r\n", rune(text[j])) {
				j++
			}
			if j < len(text) && (text[j] == '}' || text[j] == ']') {
				continue
			}
		}
		out.WriteByte(char)
	}
	return out.String()
}

// repairJSONStringLiterals matches the pinned TypeScript structured-output
// repair: raw controls are escaped and invalid backslash escapes are preserved
// as literal backslashes.
func repairJSONStringLiterals(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	inString := false
	for i := 0; i < len(text); i++ {
		char := text[i]
		if !inString {
			out.WriteByte(char)
			if char == '"' {
				inString = true
			}
			continue
		}
		if char == '"' {
			out.WriteByte(char)
			inString = false
			continue
		}
		if char == '\\' {
			if i+1 == len(text) {
				out.WriteString(`\\`)
				continue
			}
			next := text[i+1]
			if next == 'u' && i+5 < len(text) && isHex(text[i+2]) && isHex(text[i+3]) && isHex(text[i+4]) && isHex(text[i+5]) {
				out.WriteString(text[i : i+6])
				i += 5
				continue
			}
			if strings.ContainsRune(`"\\/bfnrtu`, rune(next)) {
				out.WriteByte(char)
				out.WriteByte(next)
				i++
				continue
			}
			out.WriteString(`\\`)
			continue
		}
		if char < 0x20 {
			switch char {
			case '\b':
				out.WriteString(`\b`)
			case '\f':
				out.WriteString(`\f`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			case '\t':
				out.WriteString(`\t`)
			default:
				fmt.Fprintf(&out, `\u%04x`, char)
			}
			continue
		}
		out.WriteByte(char)
	}
	return out.String()
}

func isHex(char byte) bool {
	return char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F'
}
