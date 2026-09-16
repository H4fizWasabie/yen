package agent

import (
	"context"
	"errors"
	"maps"
	"sync"
)

type Message struct {
	Role                string
	Content             string
	TextSignature       string
	Thinking            string
	ThinkingSignature   string
	ReasoningDetails    []any
	ThinkingRedacted    bool
	Images              []string
	ToolCalls           []ToolCall
	ToolCallID          string
	ToolName            string
	StopReason          string
	ErrorMessage        string
	ResponseID          string
	ResponseModel       string
	RawStopReason       string
	Provider            string
	Model               string
	Usage               *Usage
	ToolResultDetails   any
	ToolResultUsage     *Usage
	AddedToolNames      []string
	ToolResultTerminate bool
}

type ToolCall struct {
	ID               string
	Name             string
	Args             map[string]any
	ThoughtSignature string
	Namespace        string
}

type Response struct {
	Text              string
	TextSignature     string
	Thinking          string
	ThinkingSignature string
	ReasoningDetails  []any
	ThinkingRedacted  bool
	ToolCalls         []ToolCall
	StopReason        string
	RawStopReason     string
	ErrorMessage      string
	ResponseID        string
	ResponseModel     string
	Provider          string
	Model             string
	Usage             Usage
}

type Usage struct {
	Input       int
	Output      int
	Reasoning   int
	CacheRead   int
	CacheWrite  int
	TotalTokens int
}

type Provider interface {
	Next(ctx context.Context, messages []Message, tools []string) (Response, error)
}

// ToolDefinitionProvider is an additive seam for providers that can transmit
// the complete tool contract. The legacy Provider contract remains intact.
type ToolDefinitionProvider interface {
	NextWithToolDefinitions(context.Context, []Message, []ToolDefinition) (Response, error)
}
type ToolDefinitionStreamingProvider interface {
	NextWithToolDefinitionsAndEvents(context.Context, []Message, []ToolDefinition, func(StreamEvent)) (Response, error)
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
	Label       string
}
type ProviderIdentity interface{ ProviderID() string }

// TurnStopper lets a provider-specific budget stop the loop after the current
// assistant/tool turn, matching runtimes that evaluate budgets at the turn
// boundary rather than starting another provider call.
type TurnStopper interface {
	StopAfterTurn(response Response, toolResults []Message) bool
}

type StreamingProvider interface {
	NextWithUpdates(ctx context.Context, messages []Message, tools []string, update func(string)) (Response, error)
}

type StreamEvent struct {
	Type         string
	ContentIndex int
	Delta        string
	ToolCall     *ToolCall
	Partial      Message
}

// StreamingProviderWithEvents is optional so existing providers can keep the
// smaller text-update contract while richer providers expose partial messages.
type StreamingProviderWithEvents interface {
	NextWithEvents(ctx context.Context, messages []Message, tools []string, emit func(StreamEvent)) (Response, error)
}

type Tool interface {
	Name() string
	Execute(ctx context.Context, args map[string]any) (string, error)
}

type ToolResult struct {
	Text           string
	Images         []string
	Details        any
	Usage          *Usage
	AddedToolNames []string
	Terminate      bool
}

type ToolDefiner interface{ ToolDefinition() ToolDefinition }

// ToolHooks are the execution interception seam used by extensions. Hooks run
// after tool lookup and before the corresponding tool result events.
type ToolHooks struct {
	Before func(context.Context, Message, ToolCall) (block bool, reason string, err error)
	After  func(context.Context, Message, ToolCall, ToolResult, bool) (ToolResult, bool, error)

	// BeforeAgentStart may replace the initial context before the first turn.
	BeforeAgentStart func(context.Context, []Message) ([]Message, error)
	// Context runs before every provider call and may replace the context
	// messages, matching the extension context event boundary.
	Context func(context.Context, []Message) ([]Message, error)
	// ProviderBefore can replace the messages and tool names sent to a provider.
	ProviderBefore func(context.Context, []Message, []string) ([]Message, error)
	// ProviderAfter runs after a provider response is received.
	ProviderAfter func(context.Context, Response) error
	// ProviderHeaders lets an extension mutate request headers before transport.
	ProviderHeaders ProviderHeaderHook
	// ProviderResponse runs after each HTTP provider response is received.
	ProviderResponse    ProviderResponseHook
	TransformContext    func(context.Context, []Message) ([]Message, error)
	GetAPIKey           func(context.Context, string) string
	PrepareNextTurn     func(context.Context, Response, []Message, []Message) error
	ShouldStopAfterTurn func(context.Context, Response, []Message, []Message) bool
}

type ProviderHeaderHook func(context.Context, map[string][]string)

type providerHeaderHookKey struct{}

func WithProviderHeaderHook(ctx context.Context, hook ProviderHeaderHook) context.Context {
	return context.WithValue(ctx, providerHeaderHookKey{}, hook)
}

func ProviderHeaderHookFromContext(ctx context.Context) ProviderHeaderHook {
	hook, _ := ctx.Value(providerHeaderHookKey{}).(ProviderHeaderHook)
	return hook
}

type ProviderResponseHook func(context.Context, int, map[string][]string)

type providerResponseHookKey struct{}

func WithProviderResponseHook(ctx context.Context, hook ProviderResponseHook) context.Context {
	return context.WithValue(ctx, providerResponseHookKey{}, hook)
}

type apiKeyContextKey struct{}

func WithAPIKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, apiKeyContextKey{}, key)
}
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyContextKey{}).(string)
	return key, ok
}
func providerName(provider Provider) string {
	if named, ok := provider.(ProviderIdentity); ok {
		return named.ProviderID()
	}
	return ""
}

func ProviderResponseHookFromContext(ctx context.Context) ProviderResponseHook {
	hook, _ := ctx.Value(providerResponseHookKey{}).(ProviderResponseHook)
	return hook
}

type RichTool interface {
	ExecuteRich(ctx context.Context, args map[string]any) (ToolResult, error)
}

type Result struct {
	Messages  []Message
	Events    []string
	FinalText string
}

type Event struct {
	Type           string
	ID             string
	Name           string
	Args           map[string]any
	Result         string
	IsError        bool
	Usage          Usage
	Message        *Message
	Delta          string
	StopReason     string
	AssistantEvent string
	Attempt        int
	MaxAttempts    int
	DelayMs        int
	ErrorMessage   string
	Success        bool
	FinalError     string
	ToolResults    []Message
	Messages       []Message
	Details        any
	AddedToolNames []string
	Terminate      bool
}

type EventFunc func(Event)

// MessageQueues holds messages injected while an agent turn is running.
// Steering is consumed before the next assistant response; follow-up is
// consumed after an assistant would otherwise settle.
type MessageQueues struct {
	mu                         sync.Mutex
	steering                   []Message
	followUp                   []Message
	steeringMode, followUpMode string
}

func (q *MessageQueues) SetModes(steering, followUp string) {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.steeringMode, q.followUpMode = steering, followUp
	q.mu.Unlock()
}

func (q *MessageQueues) Steer(message Message) {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.steering = append(q.steering, message)
	q.mu.Unlock()
}

func (q *MessageQueues) FollowUp(message Message) {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.followUp = append(q.followUp, message)
	q.mu.Unlock()
}

func (q *MessageQueues) drainSteering() []Message {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.steeringMode == "all" {
		messages := q.steering
		q.steering = nil
		return messages
	}
	if len(q.steering) == 0 {
		return nil
	}
	messages := q.steering[:1]
	q.steering = q.steering[1:]
	return messages
}

func (q *MessageQueues) drainFollowUp() []Message {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.followUpMode == "all" {
		messages := q.followUp
		q.followUp = nil
		return messages
	}
	if len(q.followUp) == 0 {
		return nil
	}
	messages := q.followUp[:1]
	q.followUp = q.followUp[1:]
	return messages
}

func Run(ctx context.Context, provider Provider, tools []Tool, prompt string) (Result, error) {
	return RunFrom(ctx, provider, tools, nil, prompt)
}

func RunFrom(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string) (Result, error) {
	return RunFromWithUpdates(ctx, provider, tools, history, prompt, nil)
}

func RunFromWithUpdates(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string, onUpdate func(string)) (Result, error) {
	return RunFromWithQueues(ctx, provider, tools, history, prompt, nil, onUpdate)
}

func RunFromWithQueues(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string, queues *MessageQueues, onUpdate func(string)) (Result, error) {
	return runFromWithQueues(ctx, provider, tools, history, prompt, queues, onUpdate, nil)
}

func RunFromWithQueuesAndEvents(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string, queues *MessageQueues, onUpdate func(string), onEvent EventFunc) (Result, error) {
	return RunFromWithQueuesAndEventsAndImages(ctx, provider, tools, history, prompt, nil, queues, onUpdate, onEvent)
}

func RunFromWithQueuesAndEventsAndImages(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string, images []string, queues *MessageQueues, onUpdate func(string), onEvent EventFunc) (Result, error) {
	return runFromWithQueuesAndImages(ctx, provider, tools, history, prompt, images, queues, onUpdate, onEvent, nil)
}

// RunFromWithQueuesAndEventsAndImagesAndHooks is the opt-in hook-enabled form
// of RunFromWithQueuesAndEventsAndImages. Existing callers retain unchanged
// behavior through the nil-hooks wrapper above.
func RunFromWithQueuesAndEventsAndImagesAndHooks(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string, images []string, queues *MessageQueues, onUpdate func(string), onEvent EventFunc, hooks *ToolHooks) (Result, error) {
	return runFromWithQueuesAndImages(ctx, provider, tools, history, prompt, images, queues, onUpdate, onEvent, hooks)
}

// ContinueFrom resumes from an existing context without adding a user prompt.
func ContinueFrom(ctx context.Context, provider Provider, tools []Tool, history []Message, queues *MessageQueues, onUpdate func(string), onEvent EventFunc, hooks *ToolHooks) (Result, error) {
	return runFromWithQueuesAndImages(ctx, provider, tools, history, "", nil, queues, onUpdate, onEvent, hooks, true)
}

func runFromWithQueues(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string, queues *MessageQueues, onUpdate func(string), onEvent EventFunc) (Result, error) {
	return runFromWithQueuesAndImages(ctx, provider, tools, history, prompt, nil, queues, onUpdate, onEvent, nil)
}

func runFromWithQueuesAndImages(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string, images []string, queues *MessageQueues, onUpdate func(string), onEvent EventFunc, hooks *ToolHooks, continuation ...bool) (Result, error) {
	if hooks != nil && hooks.BeforeAgentStart != nil {
		var err error
		history, err = hooks.BeforeAgentStart(ctx, cloneMessages(history))
		if err != nil {
			return Result{}, err
		}
	}
	result := Result{Messages: append([]Message(nil), history...), Events: []string{"agent_start"}}
	emitEvent(onEvent, Event{Type: "agent_start"})
	if len(continuation) == 0 || !continuation[0] {
		result.Messages = append(result.Messages, Message{Role: "user", Content: prompt, Images: images})
		result.Events = append(result.Events, "turn_start", "message_start:user", "message_end:user")
		emitEvent(onEvent, Event{Type: "turn_start"})
		userMessage := result.Messages[len(result.Messages)-1]
		emitEvent(onEvent, Event{Type: "message_start", Message: &userMessage})
		emitEvent(onEvent, Event{Type: "message_end", Message: &userMessage})
	} else if len(result.Messages) == 0 || result.Messages[len(result.Messages)-1].Role == "assistant" {
		return Result{}, errors.New("cannot continue from assistant or empty context")
	}

	toolMap := make(map[string]Tool, len(tools))
	toolNames := make([]string, 0, len(tools))
	toolDefinitions := make([]ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name()] = tool
		toolNames = append(toolNames, tool.Name())
		definition := ToolDefinition{Name: tool.Name()}
		if definer, ok := tool.(ToolDefiner); ok {
			definition = definer.ToolDefinition()
			if definition.Name == "" {
				definition.Name = tool.Name()
			}
		}
		toolDefinitions = append(toolDefinitions, definition)
	}

	firstTurn := true
	for {
		if firstTurn {
			firstTurn = false
		} else {
			emitEvent(onEvent, Event{Type: "turn_start"})
		}
		appendQueuedMessages(&result, queues.drainSteering(), onEvent)
		result.Events = append(result.Events, "message_start:assistant")
		emitEvent(onEvent, Event{Type: "message_start", Message: &Message{Role: "assistant"}})
		var response Response
		var err error
		providerContext := ctx
		if hooks != nil && hooks.ProviderHeaders != nil {
			providerContext = WithProviderHeaderHook(providerContext, hooks.ProviderHeaders)
		}
		if hooks != nil && hooks.ProviderResponse != nil {
			providerContext = WithProviderResponseHook(providerContext, hooks.ProviderResponse)
		}
		if hooks != nil && hooks.GetAPIKey != nil {
			providerContext = WithAPIKey(providerContext, hooks.GetAPIKey(ctx, providerName(provider)))
		}
		providerMessages := result.Messages
		if hooks != nil && hooks.TransformContext != nil {
			providerMessages, err = hooks.TransformContext(ctx, cloneMessages(providerMessages))
		}
		if hooks != nil && hooks.Context != nil {
			if err == nil {
				providerMessages, err = hooks.Context(ctx, cloneMessages(providerMessages))
			}
		}
		if hooks != nil && hooks.ProviderBefore != nil {
			if err == nil {
				providerMessages, err = hooks.ProviderBefore(ctx, providerMessages, toolNames)
			}
		}
		if err == nil {
			if streaming, ok := provider.(ToolDefinitionStreamingProvider); ok {
				response, err = streaming.NextWithToolDefinitionsAndEvents(providerContext, providerMessages, toolDefinitions, func(event StreamEvent) {
					if event.Type == "text_delta" && event.Delta != "" && onUpdate != nil {
						onUpdate(event.Delta)
					}
					if onEvent != nil {
						onEvent(Event{Type: "message_update", AssistantEvent: event.Type, Delta: event.Delta, Message: &event.Partial})
					}
				})
			} else if streaming, ok := provider.(StreamingProviderWithEvents); ok {
				response, err = streaming.NextWithEvents(providerContext, providerMessages, toolNames, func(event StreamEvent) {
					if event.Type == "text_delta" && event.Delta != "" {
						result.Events = append(result.Events, "message_update")
						if onUpdate != nil {
							onUpdate(event.Delta)
						}
					}
					if onEvent != nil {
						onEvent(Event{Type: "message_update", AssistantEvent: event.Type, Delta: event.Delta, Message: &event.Partial})
					}
				})
			} else if streaming, ok := provider.(StreamingProvider); ok {
				response, err = streaming.NextWithUpdates(providerContext, providerMessages, toolNames, func(text string) {
					if text != "" {
						result.Events = append(result.Events, "message_update")
						if onUpdate != nil {
							onUpdate(text)
						}
					}
				})
			} else if defined, ok := provider.(ToolDefinitionProvider); ok {
				response, err = defined.NextWithToolDefinitions(providerContext, providerMessages, toolDefinitions)
			} else {
				response, err = provider.Next(providerContext, providerMessages, toolNames)
			}
		}
		if err == nil && hooks != nil && hooks.ProviderAfter != nil {
			err = hooks.ProviderAfter(ctx, response)
		}
		if err != nil {
			stopReason := "error"
			if ctx.Err() != nil {
				stopReason = "aborted"
			}
			assistant := Message{Role: "assistant", Content: err.Error(), StopReason: stopReason, ErrorMessage: err.Error()}
			result.Messages = append(result.Messages, assistant)
			emitEvent(onEvent, Event{Type: "message_end", Message: &assistant, StopReason: stopReason})
			result.Events = append(result.Events, "message_end:assistant:"+stopReason, "turn_end", "agent_end", "agent_settled")
			emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant, StopReason: stopReason})
			emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
			emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
			return result, err
		}
		assistant := Message{Role: "assistant", Content: response.Text, TextSignature: response.TextSignature, Thinking: response.Thinking, ThinkingSignature: response.ThinkingSignature, ReasoningDetails: response.ReasoningDetails, ThinkingRedacted: response.ThinkingRedacted, ToolCalls: response.ToolCalls, StopReason: response.StopReason, ErrorMessage: response.ErrorMessage, ResponseID: response.ResponseID, ResponseModel: response.ResponseModel, RawStopReason: response.RawStopReason, Provider: response.Provider, Model: response.Model, Usage: &response.Usage}
		result.Messages = append(result.Messages, assistant)
		if onEvent != nil {
			onEvent(Event{Type: "usage", Usage: response.Usage, Message: &assistant, StopReason: response.StopReason})
		}
		if len(response.ToolCalls) > 0 {
			result.Events = append(result.Events, "message_end:assistant:toolUse")
		} else {
			result.Events = append(result.Events, "message_end:assistant")
		}
		emitEvent(onEvent, Event{Type: "message_end", Message: &assistant, StopReason: response.StopReason})
		if response.StopReason == "error" {
			message := response.ErrorMessage
			if message == "" {
				message = response.Text
			}
			if message == "" {
				message = "provider returned an error"
			}
			result.Events = append(result.Events, "turn_end", "agent_end", "agent_settled")
			emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant, StopReason: "error"})
			emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
			emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
			return result, errors.New(message)
		}

		if len(response.ToolCalls) == 0 {
			if hooks != nil && hooks.ShouldStopAfterTurn != nil && hooks.ShouldStopAfterTurn(ctx, response, nil, result.Messages) {
				result.FinalText = response.Text
				return result, nil
			}
			if stopper, ok := provider.(TurnStopper); ok && stopper.StopAfterTurn(response, nil) {
				result.FinalText = response.Text
				result.Events = append(result.Events, "turn_end", "agent_end", "agent_settled")
				emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant})
				emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
				emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
				return result, nil
			}
			queued := queues.drainSteering()
			if len(queued) == 0 {
				queued = queues.drainFollowUp()
			}
			if len(queued) > 0 {
				appendQueuedMessages(&result, queued, onEvent)
				result.Events = append(result.Events, "turn_end", "turn_start")
				emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant})
				continue
			}
			result.FinalText = response.Text
			result.Events = append(result.Events, "turn_end", "agent_end", "agent_settled")
			emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant})
			emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
			emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
			return result, nil
		}
		if len(response.ToolCalls) > 1 && response.StopReason != "length" {
			toolResults, err := runParallelToolCalls(ctx, &result, response.ToolCalls, toolMap, onEvent, hooks)
			if err != nil {
				result.Events = append(result.Events, "turn_end", "agent_end", "agent_settled")
				emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant, ToolResults: toolResults})
				emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
				emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
				return result, err
			}
			result.Events = append(result.Events, "turn_end")
			emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant, ToolResults: toolResults})
			if allToolResultsTerminate(toolResults) {
				result.Events = append(result.Events, "agent_end", "agent_settled")
				emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
				emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
				return result, nil
			}
			if stopper, ok := provider.(TurnStopper); ok && stopper.StopAfterTurn(response, toolResults) {
				result.Events = append(result.Events, "agent_end", "agent_settled")
				emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
				emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
				return result, nil
			}
			result.Events = append(result.Events, "turn_start")
			continue
		}

		toolResults := make([]Message, 0, len(response.ToolCalls))
		for _, call := range response.ToolCalls {
			if onEvent != nil {
				onEvent(Event{Type: "tool_call", ID: call.ID, Name: call.Name, Args: call.Args, Message: &assistant})
				onEvent(Event{Type: "tool_execution_start", ID: call.ID, Name: call.Name, Args: call.Args, Message: &assistant})
			}
			if response.StopReason == "length" {
				result.Events = append(result.Events, "tool_execution_start:"+call.ID)
				content := `Tool call "` + call.Name + `" was not executed: the response hit the output token limit, so its arguments may be truncated. Re-issue the tool call with complete arguments.`
				result.Events = append(result.Events, "tool_execution_end:"+call.ID, "message_start:toolResult")
				toolMessage := Message{Role: "tool", Content: content, ToolCallID: call.ID, ToolName: call.Name}
				result.Messages = append(result.Messages, toolMessage)
				toolResults = append(toolResults, toolMessage)
				emitEvent(onEvent, Event{Type: "message_start", Message: &toolMessage})
				if onEvent != nil {
					onEvent(Event{Type: "tool_execution_end", ID: call.ID, Name: call.Name, Result: content, IsError: true, Message: &toolMessage})
					onEvent(Event{Type: "tool_result", ID: call.ID, Name: call.Name, Result: content, IsError: true, Message: &toolMessage})
				}
				result.Events = append(result.Events, "message_end:toolResult")
				emitEvent(onEvent, Event{Type: "message_end", Message: &toolMessage})
				continue
			}
			tool, ok := toolMap[call.Name]
			if !ok {
				result.Events = append(result.Events, "tool_execution_start:"+call.ID, "tool_execution_end:"+call.ID)
				result.Events = append(result.Events, "message_start:toolResult")
				content := (&UnknownToolError{Name: call.Name}).Error()
				toolMessage := Message{Role: "tool", Content: content, ToolCallID: call.ID, ToolName: call.Name}
				result.Messages = append(result.Messages, toolMessage)
				toolResults = append(toolResults, toolMessage)
				emitEvent(onEvent, Event{Type: "message_start", Message: &toolMessage})
				if onEvent != nil {
					onEvent(Event{Type: "tool_execution_end", ID: call.ID, Name: call.Name, Result: content, IsError: true, Message: &toolMessage})
					onEvent(Event{Type: "tool_result", ID: call.ID, Name: call.Name, Result: content, IsError: true, Message: &toolMessage})
				}
				result.Events = append(result.Events, "message_end:toolResult")
				emitEvent(onEvent, Event{Type: "message_end", Message: &toolMessage})
				continue
			}
			if hooks != nil && hooks.Before != nil {
				block, reason, hookErr := hooks.Before(ctx, assistant, call)
				if hookErr != nil {
					content := "Tool error: " + hookErr.Error()
					result.Events = append(result.Events, "tool_execution_end:"+call.ID, "message_start:toolResult")
					toolMessage := Message{Role: "tool", Content: content, ToolCallID: call.ID, ToolName: call.Name}
					result.Messages = append(result.Messages, toolMessage)
					toolResults = append(toolResults, toolMessage)
					emitEvent(onEvent, Event{Type: "message_start", Message: &toolMessage})
					emitEvent(onEvent, Event{Type: "tool_execution_end", ID: call.ID, Name: call.Name, Result: content, IsError: true, Message: &toolMessage})
					emitEvent(onEvent, Event{Type: "tool_result", ID: call.ID, Name: call.Name, Result: content, IsError: true, Message: &toolMessage})
					result.Events = append(result.Events, "message_end:toolResult")
					emitEvent(onEvent, Event{Type: "message_end", Message: &toolMessage})
					continue
				}
				if block {
					if reason == "" {
						reason = "Tool execution was blocked"
					}
					content := reason
					result.Events = append(result.Events, "tool_execution_end:"+call.ID, "message_start:toolResult")
					toolMessage := Message{Role: "tool", Content: content, ToolCallID: call.ID, ToolName: call.Name}
					result.Messages = append(result.Messages, toolMessage)
					toolResults = append(toolResults, toolMessage)
					emitEvent(onEvent, Event{Type: "message_start", Message: &toolMessage})
					emitEvent(onEvent, Event{Type: "tool_execution_end", ID: call.ID, Name: call.Name, Result: content, IsError: true, Message: &toolMessage})
					emitEvent(onEvent, Event{Type: "tool_result", ID: call.ID, Name: call.Name, Result: content, IsError: true, Message: &toolMessage})
					result.Events = append(result.Events, "message_end:toolResult")
					emitEvent(onEvent, Event{Type: "message_end", Message: &toolMessage})
					continue
				}
			}
			result.Events = append(result.Events, "tool_execution_start:"+call.ID)
			toolResult, err := executeTool(ctx, tool, call.Args)
			content, images := toolResult.Text, toolResult.Images
			if err != nil {
				result.Events = append(result.Events, "tool_execution_end:"+call.ID)
				if ctx.Err() != nil {
					result.Events = append(result.Events, "message_start:toolResult")
					toolMessage := Message{Role: "tool", Content: "Operation aborted", ToolCallID: call.ID, ToolName: call.Name}
					result.Messages = append(result.Messages, toolMessage)
					toolResults = append(toolResults, toolMessage)
					emitEvent(onEvent, Event{Type: "message_start", Message: &toolMessage})
					if onEvent != nil {
						onEvent(Event{Type: "tool_execution_end", ID: call.ID, Name: call.Name, Result: "Operation aborted", IsError: true, Message: &toolMessage})
						onEvent(Event{Type: "tool_result", ID: call.ID, Name: call.Name, Result: "Operation aborted", IsError: true, Message: &toolMessage})
					}
					result.Events = append(result.Events, "message_end:toolResult", "turn_end", "agent_end", "agent_settled")
					emitEvent(onEvent, Event{Type: "message_end", Message: &toolMessage})
					emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant, ToolResults: []Message{toolMessage}, StopReason: "aborted"})
					emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
					emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
					return result, ctx.Err()
				}
				content = "Tool error: " + err.Error()
			} else {
				result.Events = append(result.Events, "tool_execution_end:"+call.ID)
			}
			if hooks != nil && hooks.After != nil {
				var isError bool
				var hookErr error
				toolResult, isError, hookErr = hooks.After(ctx, assistant, call, toolResult, err != nil)
				if hookErr != nil {
					err = hookErr
				}
				if isError {
					err = errors.New("tool result overridden as error")
				}
				content, images = toolResult.Text, toolResult.Images
			}
			result.Events = append(result.Events, "message_start:toolResult")
			toolMessage := Message{Role: "tool", Content: content, Images: images, ToolCallID: call.ID, ToolName: call.Name, ToolResultDetails: toolResult.Details, ToolResultUsage: toolResult.Usage, AddedToolNames: toolResult.AddedToolNames, ToolResultTerminate: toolResult.Terminate}
			result.Messages = append(result.Messages, toolMessage)
			toolResults = append(toolResults, toolMessage)
			emitEvent(onEvent, Event{Type: "message_start", Message: &toolMessage})
			if onEvent != nil {
				onEvent(Event{Type: "tool_execution_end", ID: call.ID, Name: call.Name, Result: content, IsError: err != nil, Message: &toolMessage, Details: toolResult.Details, Usage: usageValue(toolResult.Usage), AddedToolNames: toolResult.AddedToolNames, Terminate: toolResult.Terminate})
				onEvent(Event{Type: "tool_result", ID: call.ID, Name: call.Name, Result: content, IsError: err != nil, Message: &toolMessage, Details: toolResult.Details, Usage: usageValue(toolResult.Usage), AddedToolNames: toolResult.AddedToolNames, Terminate: toolResult.Terminate})
			}
			result.Events = append(result.Events, "message_end:toolResult")
			emitEvent(onEvent, Event{Type: "message_end", Message: &toolMessage})
		}
		result.Events = append(result.Events, "turn_end")
		emitEvent(onEvent, Event{Type: "turn_end", Message: &assistant, ToolResults: toolResults})
		if allToolResultsTerminate(toolResults) {
			result.Events = append(result.Events, "agent_end", "agent_settled")
			emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
			emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
			return result, nil
		}
		if hooks != nil && hooks.PrepareNextTurn != nil {
			if err := hooks.PrepareNextTurn(ctx, response, toolResults, result.Messages); err != nil {
				return result, err
			}
		}
		if stopper, ok := provider.(TurnStopper); ok && stopper.StopAfterTurn(response, toolResults) {
			result.Events = append(result.Events, "agent_end", "agent_settled")
			emitEvent(onEvent, Event{Type: "agent_end", Messages: append([]Message(nil), result.Messages...)})
			emitEvent(onEvent, Event{Type: "agent_settled", Messages: append([]Message(nil), result.Messages...)})
			return result, nil
		}
		if hooks != nil && hooks.ShouldStopAfterTurn != nil && hooks.ShouldStopAfterTurn(ctx, response, toolResults, result.Messages) {
			return result, nil
		}
		result.Events = append(result.Events, "turn_start")
	}
}

func cloneMessages(messages []Message) []Message {
	result := append([]Message(nil), messages...)
	for i := range result {
		result[i].Images = append([]string(nil), result[i].Images...)
		result[i].ToolCalls = append([]ToolCall(nil), result[i].ToolCalls...)
		for j := range result[i].ToolCalls {
			result[i].ToolCalls[j].Args = cloneArgs(result[i].ToolCalls[j].Args)
		}
	}
	return result
}

func cloneArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	return maps.Clone(args)
}

type parallelToolResult struct {
	call           ToolCall
	content        string
	images         []string
	details        any
	usage          *Usage
	addedToolNames []string
	terminate      bool
	err            error
	blocked        bool
}

func executeTool(ctx context.Context, tool Tool, args map[string]any) (ToolResult, error) {
	if rich, ok := tool.(RichTool); ok {
		return rich.ExecuteRich(ctx, args)
	}
	text, err := tool.Execute(ctx, args)
	return ToolResult{Text: text}, err
}

func usageValue(usage *Usage) Usage {
	if usage == nil {
		return Usage{}
	}
	return *usage
}

func allToolResultsTerminate(messages []Message) bool {
	if len(messages) == 0 {
		return false
	}
	for _, message := range messages {
		if !message.ToolResultTerminate {
			return false
		}
	}
	return true
}

func runParallelToolCalls(ctx context.Context, result *Result, calls []ToolCall, toolMap map[string]Tool, onEvent EventFunc, hooks *ToolHooks) ([]Message, error) {
	outcomes := make([]parallelToolResult, len(calls))
	var wait sync.WaitGroup
	for i, call := range calls {
		outcomes[i].call = call
		result.Events = append(result.Events, "tool_execution_start:"+call.ID)
		if onEvent != nil {
			assistant := result.Messages[len(result.Messages)-1]
			onEvent(Event{Type: "tool_call", ID: call.ID, Name: call.Name, Args: call.Args, Message: &assistant})
			onEvent(Event{Type: "tool_execution_start", ID: call.ID, Name: call.Name, Args: call.Args, Message: &assistant})
		}
		wait.Add(1)
		go func(i int, call ToolCall) {
			defer wait.Done()
			tool, ok := toolMap[call.Name]
			if !ok {
				outcomes[i].content = (&UnknownToolError{Name: call.Name}).Error()
				outcomes[i].err = errors.New(outcomes[i].content)
				return
			}
			assistant := result.Messages[len(result.Messages)-1]
			if hooks != nil && hooks.Before != nil {
				block, reason, hookErr := hooks.Before(ctx, assistant, call)
				if hookErr != nil {
					outcomes[i].err = hookErr
					return
				}
				if block {
					if reason == "" {
						reason = "Tool execution was blocked"
					}
					outcomes[i].content = reason
					outcomes[i].blocked = true
					return
				}
			}
			toolResult, err := executeTool(ctx, tool, call.Args)
			outcomes[i].content, outcomes[i].images, outcomes[i].details, outcomes[i].usage, outcomes[i].addedToolNames, outcomes[i].terminate, outcomes[i].err = toolResult.Text, toolResult.Images, toolResult.Details, toolResult.Usage, toolResult.AddedToolNames, toolResult.Terminate, err
			if hooks != nil && hooks.After != nil {
				updated, isError, hookErr := hooks.After(ctx, assistant, call, toolResult, err != nil)
				if hookErr != nil {
					outcomes[i].err = hookErr
					return
				}
				outcomes[i].content, outcomes[i].images = updated.Text, updated.Images
				outcomes[i].details, outcomes[i].usage, outcomes[i].addedToolNames, outcomes[i].terminate = updated.Details, updated.Usage, updated.AddedToolNames, updated.Terminate
				if isError && outcomes[i].err == nil {
					outcomes[i].err = errors.New("tool result overridden as error")
				}
			}
		}(i, call)
	}
	wait.Wait()
	var canceled error
	toolMessages := make([]Message, 0, len(outcomes))
	for _, outcome := range outcomes {
		content := outcome.content
		isError := outcome.err != nil
		if outcome.err != nil {
			if ctx.Err() != nil {
				content = "Operation aborted"
				canceled = ctx.Err()
			} else {
				content = "Tool error: " + outcome.err.Error()
			}
		}
		if outcome.blocked {
			isError = true
		}
		result.Events = append(result.Events, "tool_execution_end:"+outcome.call.ID, "message_start:toolResult")
		toolMessage := Message{Role: "tool", Content: content, Images: outcome.images, ToolCallID: outcome.call.ID, ToolName: outcome.call.Name, ToolResultDetails: outcome.details, ToolResultUsage: outcome.usage, AddedToolNames: outcome.addedToolNames, ToolResultTerminate: outcome.terminate}
		result.Messages = append(result.Messages, toolMessage)
		toolMessages = append(toolMessages, toolMessage)
		emitEvent(onEvent, Event{Type: "message_start", Message: &toolMessage})
		if onEvent != nil {
			onEvent(Event{Type: "tool_execution_end", ID: outcome.call.ID, Name: outcome.call.Name, Result: content, IsError: isError, Message: &toolMessage, Details: outcome.details, Usage: usageValue(outcome.usage), AddedToolNames: outcome.addedToolNames, Terminate: outcome.terminate})
			onEvent(Event{Type: "tool_result", ID: outcome.call.ID, Name: outcome.call.Name, Result: content, IsError: isError, Message: &toolMessage, Details: outcome.details, Usage: usageValue(outcome.usage), AddedToolNames: outcome.addedToolNames, Terminate: outcome.terminate})
		}
		result.Events = append(result.Events, "message_end:toolResult")
		emitEvent(onEvent, Event{Type: "message_end", Message: &toolMessage})
	}
	return toolMessages, canceled
}

func appendQueuedMessages(result *Result, messages []Message, onEvent EventFunc) {
	for _, message := range messages {
		role := message.Role
		if role == "" {
			role = "user"
		}
		result.Messages = append(result.Messages, message)
		result.Events = append(result.Events, "message_start:"+role, "message_end:"+role)
		queued := result.Messages[len(result.Messages)-1]
		emitEvent(onEvent, Event{Type: "message_start", Message: &queued})
		emitEvent(onEvent, Event{Type: "message_end", Message: &queued})
	}
}

func emitEvent(onEvent EventFunc, event Event) {
	if onEvent != nil {
		onEvent(event)
	}
}

type UnknownToolError struct {
	Name string
}

func (e *UnknownToolError) Error() string { return "unknown tool: " + e.Name }
