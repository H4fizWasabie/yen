package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	providerpkg "github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/session"
)

type Runner struct {
	Queue                       *conversation.Queue
	Provider                    agent.Provider
	ToolFactory                 func(workspace string) []agent.Tool
	SessionPath                 func(turn conversation.Turn) string
	Checkpoints                 *memory.Checkpoints
	Memory                      *memory.Engine
	SharedMemory                bool
	AutoCompactTurns            int
	AutoCompactKeepRecentTokens int
	AutoCompactContextWindow    int
	AutoCompactReserveTokens    int
	AutoCompactOnOverflow       bool
	AutoConsolidate             bool

	mu     sync.Mutex
	active map[string]context.CancelFunc
	queues map[string]*agent.MessageQueues
}

func New(queue *conversation.Queue, provider agent.Provider, tools func(string) []agent.Tool) *Runner {
	return &Runner{Queue: queue, Provider: provider, ToolFactory: tools, active: make(map[string]context.CancelFunc), queues: make(map[string]*agent.MessageQueues)}
}

func AutoCompactTurnsFromEnv() int {
	value, err := strconv.Atoi(os.Getenv("THEOSES_AUTO_COMPACT_TURNS"))
	if err != nil || value < 1 {
		return 0
	}
	return value
}

func AutoCompactKeepRecentTokensFromEnv() int {
	value, err := strconv.Atoi(os.Getenv("THEOSES_AUTO_COMPACT_KEEP_RECENT_TOKENS"))
	if err != nil || value < 1 {
		return 0
	}
	return value
}

func AutoCompactContextWindowFromEnv() int {
	value, err := strconv.Atoi(os.Getenv("THEOSES_AUTO_COMPACT_CONTEXT_WINDOW"))
	if err != nil || value < 1 {
		return 0
	}
	return value
}

func AutoCompactReserveTokensFromEnv() int {
	value, err := strconv.Atoi(os.Getenv("THEOSES_AUTO_COMPACT_RESERVE_TOKENS"))
	if err != nil || value < 1 {
		return 16384
	}
	return value
}

func AutoCompactOnOverflowFromEnv() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("THEOSES_AUTO_COMPACT_OVERFLOW")))
	return value == "1" || value == "true" || value == "yes"
}

func AutoConsolidateFromEnv() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("THEOSES_AUTO_CONSOLIDATE")))
	return value == "1" || value == "true" || value == "yes"
}

func (r *Runner) Submit(link conversation.Link, prompt string) (conversation.Turn, error) {
	if r.Queue == nil {
		return conversation.Turn{}, errors.New("runtime queue is required")
	}
	return r.Queue.Enqueue(link.ConversationID, link.Adapter, link.AdapterKey, link.WorkspaceID, prompt)
}

func (r *Runner) RunNext(ctx context.Context, conversationID string) (conversation.Turn, agent.Result, error) {
	return r.runNext(ctx, conversationID, nil)
}

func (r *Runner) RunNextWithUpdates(ctx context.Context, conversationID string, onUpdate func(string)) (conversation.Turn, agent.Result, error) {
	return r.runNext(ctx, conversationID, onUpdate)
}

func (r *Runner) RunSubmitted(ctx context.Context, submitted conversation.Turn) (conversation.Turn, agent.Result, error) {
	return r.runSubmitted(ctx, submitted, nil, nil, nil)
}

func (r *Runner) RunSubmittedWithUpdates(ctx context.Context, submitted conversation.Turn, onUpdate func(string)) (conversation.Turn, agent.Result, error) {
	return r.runSubmitted(ctx, submitted, nil, onUpdate, nil)
}

func (r *Runner) RunSubmittedWithEvents(ctx context.Context, submitted conversation.Turn, onUpdate func(string), onEvent agent.EventFunc) (conversation.Turn, agent.Result, error) {
	return r.RunSubmittedWithEventsAndImages(ctx, submitted, nil, onUpdate, onEvent)
}

func (r *Runner) RunSubmittedWithEventsAndImages(ctx context.Context, submitted conversation.Turn, images []string, onUpdate func(string), onEvent agent.EventFunc) (conversation.Turn, agent.Result, error) {
	return r.runSubmitted(ctx, submitted, images, onUpdate, onEvent)
}

func (r *Runner) runSubmitted(ctx context.Context, submitted conversation.Turn, images []string, onUpdate func(string), onEvent agent.EventFunc) (conversation.Turn, agent.Result, error) {
	for {
		turn, ok, err := r.Queue.Claim(submitted.ConversationID)
		if err != nil {
			return conversation.Turn{}, agent.Result{}, err
		}
		if ok {
			turnImages := images
			if turn.ID != submitted.ID {
				turnImages = nil
			}
			result, runErr := r.runClaimed(ctx, turn, turnImages, onUpdate, onEvent)
			if turn.ID == submitted.ID || runErr != nil {
				return turn, result, runErr
			}
			continue
		}
		select {
		case <-ctx.Done():
			return conversation.Turn{}, agent.Result{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (r *Runner) runNext(ctx context.Context, conversationID string, onUpdate func(string)) (conversation.Turn, agent.Result, error) {
	turn, ok, err := r.Queue.Claim(conversationID)
	if err != nil {
		return conversation.Turn{}, agent.Result{}, err
	}
	if !ok {
		return conversation.Turn{}, agent.Result{}, errors.New("no pending turn")
	}
	result, runErr := r.runClaimed(ctx, turn, nil, onUpdate, nil)
	return turn, result, runErr
}

func (r *Runner) runClaimed(ctx context.Context, turn conversation.Turn, images []string, onUpdate func(string), onEvent agent.EventFunc) (agent.Result, error) {
	turnCtx, cancel := context.WithCancel(ctx)
	queues := &agent.MessageQueues{}
	r.mu.Lock()
	r.active[turn.ID] = cancel
	r.queues[turn.ID] = queues
	r.mu.Unlock()
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		renewTicker := time.NewTicker(conversation.DefaultLeaseDuration / 3)
		defer renewTicker.Stop()
		statusTicker := time.NewTicker(100 * time.Millisecond)
		defer statusTicker.Stop()
		for {
			select {
			case <-renewTicker.C:
				if err := r.Queue.Renew(turn.ID, conversation.DefaultLeaseDuration); err != nil {
					cancel()
					return
				}
			case <-statusTicker.C:
				current, ok := r.Queue.Get(turn.ID)
				if !ok || current.Status != "active" || current.LeaseOwner != turn.LeaseOwner {
					cancel()
					return
				}
			case <-turnCtx.Done():
				return
			}
		}
	}()
	defer func() {
		cancel()
		<-renewDone
		r.mu.Lock()
		delete(r.active, turn.ID)
		delete(r.queues, turn.ID)
		r.mu.Unlock()
	}()
	result, runErr := r.runTurn(turnCtx, turn, images, queues, onUpdate, onEvent)
	if turnCtx.Err() != nil || errors.Is(runErr, context.Canceled) {
		_ = r.Queue.Cancel(turn.ID)
	} else {
		_ = r.Queue.Complete(turn.ID)
	}
	return result, runErr
}

func (r *Runner) Steer(turnID, prompt string) error {
	if prompt == "" {
		return errors.New("steering prompt is required")
	}
	r.mu.Lock()
	queues := r.queues[turnID]
	r.mu.Unlock()
	if queues == nil {
		return errors.New("turn is not active")
	}
	queues.Steer(agent.Message{Role: "user", Content: prompt})
	return nil
}

func (r *Runner) FollowUp(turnID, prompt string) error {
	if prompt == "" {
		return errors.New("follow-up prompt is required")
	}
	r.mu.Lock()
	queues := r.queues[turnID]
	r.mu.Unlock()
	if queues == nil {
		return errors.New("turn is not active")
	}
	queues.FollowUp(agent.Message{Role: "user", Content: prompt})
	return nil
}

func (r *Runner) Cancel(turnID string) error {
	r.mu.Lock()
	cancel := r.active[turnID]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return r.Queue.CancelActive(turnID)
}

func (r *Runner) Active(conversationID string) (conversation.Turn, bool) {
	turns := r.Queue.Active(conversationID)
	if len(turns) == 0 {
		return conversation.Turn{}, false
	}
	return turns[0], true
}

func (r *Runner) OpenSession(link conversation.Link) (*session.Session, error) {
	return openOrCreate(r.pathFor(conversation.Turn{ConversationID: link.ConversationID, Adapter: link.Adapter, AdapterKey: link.AdapterKey, WorkspaceID: link.WorkspaceID}), conversation.Turn{ConversationID: link.ConversationID, Adapter: link.Adapter, AdapterKey: link.AdapterKey, WorkspaceID: link.WorkspaceID})
}

func (r *Runner) Compact(ctx context.Context, conversationID string, keepRecentTurns int) error {
	if r.Provider == nil {
		return errors.New("compaction provider is required")
	}
	if _, active := r.Active(conversationID); active {
		return errors.New("cannot compact an active conversation")
	}
	return r.compactConversation(ctx, conversationID, keepRecentTurns)
}

func (r *Runner) compactConversation(ctx context.Context, conversationID string, keepRecentTurns int) error {
	if r.Provider == nil {
		return errors.New("compaction provider is required")
	}
	current, err := openOrCreate(r.pathFor(conversation.Turn{ConversationID: conversationID}), conversation.Turn{ConversationID: conversationID})
	if err != nil {
		return err
	}
	var plan session.CompactionPlan
	keepRecentTokens := r.AutoCompactKeepRecentTokens
	if keepRecentTokens < 1 && r.AutoCompactContextWindow > 0 {
		keepRecentTokens = 20000
	}
	if keepRecentTokens > 0 {
		plan, err = current.PrepareCompactionByTokens(keepRecentTokens)
	} else {
		plan, err = current.PrepareCompaction(keepRecentTurns)
	}
	if err != nil {
		return err
	}
	var transcript strings.Builder
	if plan.PreviousSummary != "" {
		transcript.WriteString("<previous-summary>\n")
		transcript.WriteString(plan.PreviousSummary)
		transcript.WriteString("\n</previous-summary>\n\n")
	}
	transcript.WriteString("<conversation>\n")
	for _, message := range plan.Messages {
		transcript.WriteString(message.Role)
		transcript.WriteString(": ")
		transcript.WriteString(fmt.Sprint(message.Content))
		transcript.WriteByte('\n')
	}
	transcript.WriteString("</conversation>\n\nSummarize the conversation for a later agent. Preserve goals, constraints, decisions, progress, and next steps. Return only the summary.")
	response, err := r.Provider.Next(ctx, []agent.Message{{Role: "user", Content: transcript.String()}}, nil)
	if err != nil {
		return err
	}
	if response.StopReason == "error" || response.StopReason == "aborted" {
		return errors.New("compaction stopped: " + response.StopReason)
	}
	if strings.TrimSpace(response.Text) == "" || len(response.ToolCalls) > 0 {
		return errors.New("compaction returned an invalid summary")
	}
	_, err = current.AppendCompaction(strings.TrimSpace(response.Text), plan.FirstKeptEntryID, plan.TokensBefore, &session.Usage{
		Input: response.Usage.Input, Output: response.Usage.Output, Reasoning: response.Usage.Reasoning,
		CacheRead: response.Usage.CacheRead, CacheWrite: response.Usage.CacheWrite, TotalTokens: response.Usage.TotalTokens,
	})
	return err
}

func (r *Runner) runTurn(ctx context.Context, turn conversation.Turn, images []string, queues *agent.MessageQueues, onUpdate func(string), onEvent agent.EventFunc) (agent.Result, error) {
	path := r.pathFor(turn)
	current, err := openOrCreate(path, turn)
	if err != nil {
		return agent.Result{}, err
	}
	if r.AutoCompactTurns > 0 {
		if err := r.compactConversation(ctx, turn.ConversationID, r.AutoCompactTurns); err != nil && !errors.Is(err, session.ErrNothingToCompact) && !errors.Is(err, session.ErrAlreadyCompacted) {
			return agent.Result{}, err
		}
		current, err = openOrCreate(path, turn)
		if err != nil {
			return agent.Result{}, err
		}
	} else if r.AutoCompactContextWindow > 0 {
		threshold := r.AutoCompactContextWindow - r.AutoCompactReserveTokens
		if session.EstimateContextTokens(current.ContextMessages()) > threshold {
			if err := r.compactConversation(ctx, turn.ConversationID, 0); err != nil && !errors.Is(err, session.ErrNothingToCompact) && !errors.Is(err, session.ErrAlreadyCompacted) {
				return agent.Result{}, err
			}
			current, err = openOrCreate(path, turn)
			if err != nil {
				return agent.Result{}, err
			}
		}
	}
	history := toAgentMessages(current.ContextMessages())
	var tools []agent.Tool
	if r.ToolFactory != nil {
		tools = r.ToolFactory(turn.WorkspaceID)
	}
	if r.Memory != nil {
		r.Memory.ConversationScoped = r.SharedMemory
		ctx := memory.Context{WorkspaceID: turn.WorkspaceID, ConversationID: turn.ConversationID, ConversationScoped: r.SharedMemory}
		tools = append(tools, memory.RememberTool{Engine: r.Memory, Context: ctx}, memory.SaveNoteTool{Engine: r.Memory, Context: ctx})
	}
	tools = append(tools, recallTurnsTool{history: history})
	result, runErr := agent.RunFromWithQueuesAndEventsAndImages(ctx, r.Provider, tools, history, turn.Prompt, images, queues, onUpdate, onEvent)
	if r.AutoCompactOnOverflow && (runErr != nil && providerpkg.IsContextOverflowError(runErr.Error()) || runErr == nil && recoverableLengthStop(result)) {
		keepRecentTurns := r.AutoCompactTurns
		if keepRecentTurns < 1 {
			keepRecentTurns = 2
		}
		if compactErr := r.compactConversation(ctx, turn.ConversationID, keepRecentTurns); compactErr == nil {
			current, err = openOrCreate(path, turn)
			if err != nil {
				return result, err
			}
			history = toAgentMessages(current.ContextMessages())
			result, runErr = agent.RunFromWithQueuesAndEventsAndImages(ctx, r.Provider, tools, history, turn.Prompt, images, queues, onUpdate, onEvent)
		}
	}
	for _, message := range result.Messages[len(history):] {
		if _, err := current.Append(toSessionMessage(message)); err != nil {
			return result, err
		}
	}
	if runErr == nil && r.Memory != nil {
		if err := r.Memory.RecordTurn(turn.ID, turn.ConversationID, turn.WorkspaceID, turn.Adapter, turn.Prompt, result.FinalText); err != nil {
			return result, err
		}
		if r.AutoConsolidate {
			_, _ = r.Memory.ConsolidateIfTriggered(ctx, r.Provider, turn.ID, turn.ConversationID, turn.WorkspaceID, turn.Adapter, turn.Prompt, toConsolidationTurns(current.TimedMessages()))
		}
	}
	if runErr == nil && r.Checkpoints != nil {
		if err := r.Checkpoints.Set(turn.ConversationID, memory.Checkpoint{LastEntryID: turn.ID}); err != nil {
			return result, err
		}
	}
	return result, runErr
}

func recoverableLengthStop(result agent.Result) bool {
	if len(result.Messages) == 0 {
		return false
	}
	last := result.Messages[len(result.Messages)-1]
	return last.Role == "assistant" && last.StopReason == "length"
}

func toConsolidationTurns(messages []session.TimedMessage) []memory.ConsolidationTurn {
	turns := make([]memory.ConsolidationTurn, 0, len(messages))
	for _, timed := range messages {
		message := timed.Message
		role := message.Role
		if role == "toolResult" {
			role = "tool"
		}
		turns = append(turns, memory.ConsolidationTurn{Role: role, Content: consolidationContent(message), Timestamp: timed.Timestamp})
	}
	return turns
}

const (
	consolidationToolArgsChars   = 200
	consolidationToolResultChars = 500
)

func consolidationContent(message session.Message) string {
	switch message.Role {
	case "bashExecution":
		if message.Cancelled {
			return fmt.Sprintf("ran `%s` — cancelled", truncateConsolidation(message.Command, consolidationToolArgsChars))
		}
		exitCode := "?"
		if message.ExitCode != nil {
			exitCode = fmt.Sprint(*message.ExitCode)
		}
		return fmt.Sprintf("ran `%s` — exit %s: %s", truncateConsolidation(message.Command, consolidationToolArgsChars), exitCode, truncateConsolidation(message.Output, consolidationToolResultChars))
	case "branchSummary", "compactionSummary":
		return message.Summary
	}
	if text, ok := message.Content.(string); ok {
		return text
	}
	data, err := json.Marshal(message.Content)
	if err != nil {
		return ""
	}
	var parts []session.ContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return ""
	}
	var content strings.Builder
	for _, part := range parts {
		switch part.Type {
		case "text":
			if message.Role == "toolResult" {
				status := "OK"
				if strings.HasPrefix(part.Text, "Tool error:") || strings.HasPrefix(part.Text, "unknown tool:") || part.Text == "Operation aborted" {
					status = "FAILED"
				}
				fmt.Fprintf(&content, "%s — %s", status, truncateConsolidation(part.Text, consolidationToolResultChars))
			} else {
				content.WriteString(part.Text)
			}
		case "toolCall":
			args, _ := json.Marshal(part.Arguments)
			fmt.Fprintf(&content, "called %s(%s)", part.Name, truncateConsolidation(string(args), consolidationToolArgsChars))
		}
		if content.Len() > 0 {
			content.WriteByte('\n')
		}
	}
	return strings.TrimSpace(content.String())
}

func truncateConsolidation(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}

func (r *Runner) pathFor(turn conversation.Turn) string {
	if r.SessionPath != nil {
		return r.SessionPath(turn)
	}
	return filepath.Join(".theoses-go", "sessions", turn.ConversationID+".jsonl")
}

func openOrCreate(path string, turn conversation.Turn) (*session.Session, error) {
	if _, err := os.Stat(path); err == nil {
		return session.Open(path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return session.New(path, session.Header{ID: turn.ConversationID, ConversationID: turn.ConversationID, WorkspaceID: turn.WorkspaceID, CWD: turn.WorkspaceID, Channel: turn.Adapter, ChannelSessionID: turn.AdapterKey}), nil
}

func toAgentMessages(messages []session.Message) []agent.Message {
	result := make([]agent.Message, 0, len(messages))
	for _, message := range messages {
		converted := agent.Message{Role: message.Role, Images: message.Images, ToolCallID: message.ToolCallID, StopReason: message.StopReason, ErrorMessage: message.ErrorMessage, ResponseID: message.ResponseID, ResponseModel: message.ResponseModel, RawStopReason: message.RawStopReason, Provider: message.Provider, Model: message.Model}
		if message.Usage != nil {
			converted.Usage = &agent.Usage{
				Input: message.Usage.Input, Output: message.Usage.Output, Reasoning: message.Usage.Reasoning,
				CacheRead: message.Usage.CacheRead, CacheWrite: message.Usage.CacheWrite, TotalTokens: message.Usage.TotalTokens,
			}
		}
		switch message.Role {
		case "bashExecution":
			if message.ExcludeFromContext {
				continue
			}
			converted.Role = "user"
			converted.Content = bashExecutionContext(message)
			result = append(result, converted)
			continue
		case "branchSummary":
			converted.Role = "user"
			converted.Content = "The following is a summary of a branch that this conversation came back from:\n\n<summary>\n" + message.Summary + "\n</summary>"
			result = append(result, converted)
			continue
		case "compactionSummary":
			converted.Role = "user"
			converted.Content = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n" + message.Summary + "\n</summary>"
			result = append(result, converted)
			continue
		case "custom":
			converted.Role = "user"
		}
		if message.Role == "toolResult" {
			converted.Role = "tool"
		}
		if text, ok := message.Content.(string); ok {
			converted.Content = text
			result = append(result, converted)
			continue
		}
		data, err := json.Marshal(message.Content)
		if err != nil {
			continue
		}
		var parts []session.ContentPart
		if json.Unmarshal(data, &parts) != nil {
			continue
		}
		for _, part := range parts {
			if part.Type == "text" {
				converted.Content += part.Text
			}
			if part.Type == "thinking" {
				converted.Thinking += part.Text
			}
			if part.Type == "toolCall" {
				args, _ := part.Arguments.(map[string]any)
				converted.ToolCalls = append(converted.ToolCalls, agent.ToolCall{ID: part.ID, Name: part.Name, Args: args})
			}
		}
		result = append(result, converted)
	}
	return result
}

func bashExecutionContext(message session.Message) string {
	text := fmt.Sprintf("Ran `%s`\n", message.Command)
	if message.Output != "" {
		text += "```\n" + message.Output + "\n```"
	} else {
		text += "(no output)"
	}
	if message.Cancelled {
		text += "\n\n(command cancelled)"
	} else if message.ExitCode != nil && *message.ExitCode != 0 {
		text += fmt.Sprintf("\n\nCommand exited with code %d", *message.ExitCode)
	}
	if message.Truncated && message.FullOutputPath != "" {
		text += "\n\n[Output truncated. Full output: " + message.FullOutputPath + "]"
	}
	return text
}

func toSessionMessage(message agent.Message) session.Message {
	var usage *session.Usage
	if message.Usage != nil {
		usage = &session.Usage{
			Input: message.Usage.Input, Output: message.Usage.Output, Reasoning: message.Usage.Reasoning,
			CacheRead: message.Usage.CacheRead, CacheWrite: message.Usage.CacheWrite, TotalTokens: message.Usage.TotalTokens,
		}
	}
	if message.Role == "tool" {
		return session.Message{Role: "toolResult", ToolCallID: message.ToolCallID, Content: []session.ContentPart{{Type: "text", Text: message.Content}}, Usage: usage}
	}
	if len(message.ToolCalls) > 0 || message.Thinking != "" {
		parts := make([]session.ContentPart, 0, len(message.ToolCalls)+2)
		if message.Thinking != "" {
			parts = append(parts, session.ContentPart{Type: "thinking", Text: message.Thinking})
		}
		if message.Content != "" {
			parts = append(parts, session.ContentPart{Type: "text", Text: message.Content})
		}
		for _, call := range message.ToolCalls {
			parts = append(parts, session.ContentPart{Type: "toolCall", ID: call.ID, Name: call.Name, Arguments: call.Args})
		}
		return session.Message{Role: message.Role, Content: parts, StopReason: message.StopReason, ErrorMessage: message.ErrorMessage, ResponseID: message.ResponseID, ResponseModel: message.ResponseModel, RawStopReason: message.RawStopReason, Provider: message.Provider, Model: message.Model, Usage: usage}
	}
	return session.Message{Role: message.Role, Content: message.Content, Images: message.Images, StopReason: message.StopReason, ErrorMessage: message.ErrorMessage, ResponseID: message.ResponseID, ResponseModel: message.ResponseModel, RawStopReason: message.RawStopReason, Provider: message.Provider, Model: message.Model, Usage: usage}
}
