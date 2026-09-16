// Package extensions contains the in-process extension registration boundary.
// Loading extension files is deliberately a separate concern.
package extensions

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type Command struct {
	Name        string
	Description string
	Handler     func(context.Context, string) error
}

type RenderOptions struct{ Expanded bool }

type Renderer func(value any, options RenderOptions) (rendered any, ok bool)
type MarkdownTransformer func(string) string

// Hooks is the extension-facing form of the agent interception events.
type Hooks struct {
	BeforeTool          func(context.Context, agent.Message, agent.ToolCall) (bool, string, error)
	AfterTool           func(context.Context, agent.Message, agent.ToolCall, agent.ToolResult, bool) (agent.ToolResult, bool, error)
	BeforeAgent         func(context.Context, []agent.Message) ([]agent.Message, error)
	Context             func(context.Context, []agent.Message) ([]agent.Message, error)
	BeforeProvider      func(context.Context, []agent.Message, []string) ([]agent.Message, error)
	AfterProvider       func(context.Context, agent.Response) error
	ProviderHeaders     agent.ProviderHeaderHook
	ProviderResponse    agent.ProviderResponseHook
	TransformContext    func(context.Context, []agent.Message) ([]agent.Message, error)
	GetAPIKey           func(context.Context, string) string
	PrepareNextTurn     func(context.Context, agent.Response, []agent.Message, []agent.Message) error
	ShouldStopAfterTurn func(context.Context, agent.Response, []agent.Message, []agent.Message) bool
}

type Registry struct {
	mu                   sync.RWMutex
	commands             map[string]Command
	messageRenderers     map[string]Renderer
	entryRenderers       map[string]Renderer
	markdownTransformers []MarkdownTransformer
	hooks                []Hooks
	tools                []agent.Tool
}

func (r *Registry) RegisterTool(tool agent.Tool) error {
	if r == nil || tool == nil || strings.TrimSpace(tool.Name()) == "" {
		return errors.New("extension tool requires a name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = append(r.tools, tool)
	return nil
}

func (r *Registry) Tools() []agent.Tool {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]agent.Tool(nil), r.tools...)
}

func New() *Registry {
	return &Registry{commands: make(map[string]Command), messageRenderers: make(map[string]Renderer), entryRenderers: make(map[string]Renderer)}
}

func (r *Registry) RegisterCommand(command Command) error {
	if r == nil || strings.TrimSpace(command.Name) == "" || command.Handler == nil {
		return errors.New("extension command requires a name and handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.commands == nil {
		r.commands = make(map[string]Command)
	}
	command.Name = strings.TrimSpace(command.Name)
	r.commands[command.Name] = command
	return nil
}

func (r *Registry) Commands() []Command {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	commands := make([]Command, 0, len(r.commands))
	for _, command := range r.commands {
		commands = append(commands, command)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func (r *Registry) RegisterMessageRenderer(customType string, renderer Renderer) error {
	return r.registerRenderer(r.messageRenderers, customType, renderer)
}

func (r *Registry) RegisterEntryRenderer(customType string, renderer Renderer) error {
	return r.registerRenderer(r.entryRenderers, customType, renderer)
}

func (r *Registry) registerRenderer(target map[string]Renderer, customType string, renderer Renderer) error {
	if r == nil || strings.TrimSpace(customType) == "" || renderer == nil {
		return errors.New("extension renderer requires a type and function")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if target == nil {
		return errors.New("extension registry is not initialized")
	}
	target[strings.TrimSpace(customType)] = renderer
	return nil
}

func (r *Registry) RegisterMarkdownTransformer(transformer MarkdownTransformer) error {
	if r == nil || transformer == nil {
		return errors.New("extension markdown transformer is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markdownTransformers = append(r.markdownTransformers, transformer)
	return nil
}

func (r *Registry) TransformMarkdown(markdown string) string {
	if r == nil {
		return markdown
	}
	r.mu.RLock()
	transformers := append([]MarkdownTransformer(nil), r.markdownTransformers...)
	r.mu.RUnlock()
	for _, transformer := range transformers {
		markdown = transformer(markdown)
	}
	return markdown
}

func (r *Registry) RegisterHooks(hooks Hooks) error {
	if r == nil {
		return errors.New("extension registry is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, hooks)
	return nil
}

// AgentHooks composes registered handlers in registration order. A non-nil
// base hook is run after extension handlers so existing runtime policy remains
// authoritative.
func (r *Registry) AgentHooks(base *agent.ToolHooks) *agent.ToolHooks {
	if r == nil {
		return base
	}
	r.mu.RLock()
	registered := append([]Hooks(nil), r.hooks...)
	r.mu.RUnlock()
	if len(registered) == 0 {
		return base
	}
	all := append([]Hooks(nil), registered...)
	if base != nil {
		all = append(all, Hooks{BeforeTool: base.Before, AfterTool: base.After, BeforeAgent: base.BeforeAgentStart, Context: base.Context, BeforeProvider: base.ProviderBefore, AfterProvider: base.ProviderAfter, ProviderHeaders: base.ProviderHeaders, ProviderResponse: base.ProviderResponse, TransformContext: base.TransformContext, GetAPIKey: base.GetAPIKey, PrepareNextTurn: base.PrepareNextTurn, ShouldStopAfterTurn: base.ShouldStopAfterTurn})
	}
	result := &agent.ToolHooks{}
	result.Before = func(ctx context.Context, message agent.Message, call agent.ToolCall) (bool, string, error) {
		for _, hook := range all {
			if hook.BeforeTool != nil {
				block, reason, err := hook.BeforeTool(ctx, message, call)
				if err != nil || block {
					return block, reason, err
				}
			}
		}
		return false, "", nil
	}
	result.After = func(ctx context.Context, message agent.Message, call agent.ToolCall, toolResult agent.ToolResult, isError bool) (agent.ToolResult, bool, error) {
		for _, hook := range all {
			if hook.AfterTool != nil {
				var err error
				toolResult, isError, err = hook.AfterTool(ctx, message, call, toolResult, isError)
				if err != nil {
					return toolResult, isError, err
				}
			}
		}
		return toolResult, isError, nil
	}
	result.BeforeAgentStart = chainMessages(all, func(h Hooks) func(context.Context, []agent.Message) ([]agent.Message, error) { return h.BeforeAgent })
	result.Context = chainMessages(all, func(h Hooks) func(context.Context, []agent.Message) ([]agent.Message, error) { return h.Context })
	result.ProviderBefore = chainProvider(all)
	result.ProviderAfter = func(ctx context.Context, response agent.Response) error {
		for _, hook := range all {
			if hook.AfterProvider != nil {
				if err := hook.AfterProvider(ctx, response); err != nil {
					return err
				}
			}
		}
		return nil
	}
	result.ProviderHeaders = func(ctx context.Context, headers map[string][]string) {
		for _, hook := range all {
			if hook.ProviderHeaders != nil {
				hook.ProviderHeaders(ctx, headers)
			}
		}
	}
	result.ProviderResponse = func(ctx context.Context, status int, headers map[string][]string) {
		for _, hook := range all {
			if hook.ProviderResponse != nil {
				hook.ProviderResponse(ctx, status, headers)
			}
		}
	}
	result.TransformContext = chainMessages(all, func(h Hooks) func(context.Context, []agent.Message) ([]agent.Message, error) {
		return h.TransformContext
	})
	result.GetAPIKey = func(ctx context.Context, provider string) string {
		for _, h := range all {
			if h.GetAPIKey != nil {
				if key := h.GetAPIKey(ctx, provider); key != "" {
					return key
				}
			}
		}
		return ""
	}
	result.PrepareNextTurn = func(ctx context.Context, response agent.Response, results, messages []agent.Message) error {
		for _, h := range all {
			if h.PrepareNextTurn != nil {
				if err := h.PrepareNextTurn(ctx, response, results, messages); err != nil {
					return err
				}
			}
		}
		return nil
	}
	result.ShouldStopAfterTurn = func(ctx context.Context, response agent.Response, results, messages []agent.Message) bool {
		for _, h := range all {
			if h.ShouldStopAfterTurn != nil && h.ShouldStopAfterTurn(ctx, response, results, messages) {
				return true
			}
		}
		return false
	}
	return result
}

func chainMessages(all []Hooks, selectHook func(Hooks) func(context.Context, []agent.Message) ([]agent.Message, error)) func(context.Context, []agent.Message) ([]agent.Message, error) {
	return func(ctx context.Context, messages []agent.Message) ([]agent.Message, error) {
		current := messages
		for _, hook := range all {
			if fn := selectHook(hook); fn != nil {
				var err error
				current, err = fn(ctx, current)
				if err != nil {
					return nil, err
				}
			}
		}
		return current, nil
	}
}

func chainProvider(all []Hooks) func(context.Context, []agent.Message, []string) ([]agent.Message, error) {
	return func(ctx context.Context, messages []agent.Message, tools []string) ([]agent.Message, error) {
		current := messages
		for _, hook := range all {
			if hook.BeforeProvider != nil {
				var err error
				current, err = hook.BeforeProvider(ctx, current, tools)
				if err != nil {
					return nil, err
				}
			}
		}
		return current, nil
	}
}

func (r *Registry) MessageRenderer(customType string) (Renderer, bool) {
	return r.renderer(r.messageRenderers, customType)
}
func (r *Registry) EntryRenderer(customType string) (Renderer, bool) {
	return r.renderer(r.entryRenderers, customType)
}
func (r *Registry) renderer(source map[string]Renderer, customType string) (Renderer, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	renderer, ok := source[customType]
	return renderer, ok
}
