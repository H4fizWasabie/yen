package extensions

import (
	"context"
	"reflect"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestRegistryCommandsAndRenderersAreReplaceableAndDeterministic(t *testing.T) {
	r := New()
	if err := r.RegisterCommand(Command{Name: " z", Handler: func(context.Context, string) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterCommand(Command{Name: "a", Handler: func(context.Context, string) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterCommand(Command{Name: "z", Description: "new", Handler: func(context.Context, string) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	commands := r.Commands()
	if got := []string{commands[0].Name, commands[1].Name}; !reflect.DeepEqual(got, []string{"a", "z"}) || commands[1].Description != "new" {
		t.Fatalf("commands=%#v", commands)
	}
	first := func(any, RenderOptions) (any, bool) { return "first", true }
	second := func(any, RenderOptions) (any, bool) { return "second", true }
	if err := r.RegisterMessageRenderer("note", first); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterMessageRenderer("note", second); err != nil {
		t.Fatal(err)
	}
	renderer, ok := r.MessageRenderer("note")
	if !ok {
		t.Fatal("renderer missing")
	}
	if value, _ := renderer(nil, RenderOptions{}); value != "second" {
		t.Fatalf("renderer=%v", value)
	}
}

func TestRegistryComposesExtensionHooksBeforeBaseHooks(t *testing.T) {
	r := New()
	var order []string
	if err := r.RegisterHooks(Hooks{
		BeforeAgent: func(_ context.Context, messages []agent.Message) ([]agent.Message, error) {
			order = append(order, "extension")
			return append(messages, agent.Message{Role: "system", Content: "extension"}), nil
		},
		ProviderHeaders: func(_ context.Context, _ map[string][]string) { order = append(order, "headers") },
	}); err != nil {
		t.Fatal(err)
	}
	hooks := r.AgentHooks(&agent.ToolHooks{
		BeforeAgentStart: func(_ context.Context, messages []agent.Message) ([]agent.Message, error) {
			order = append(order, "base")
			return messages, nil
		},
		ProviderHeaders: func(_ context.Context, _ map[string][]string) { order = append(order, "base-headers") },
	})
	messages, err := hooks.BeforeAgentStart(context.Background(), []agent.Message{{Role: "user"}})
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
	ctx := agent.WithProviderHeaderHook(context.Background(), hooks.ProviderHeaders)
	if hook := agent.ProviderHeaderHookFromContext(ctx); hook != nil {
		hook(ctx, nil)
	}
	if !reflect.DeepEqual(order, []string{"extension", "base", "headers", "base-headers"}) {
		t.Fatalf("order=%v", order)
	}
}

func TestRegistryRejectsIncompleteRegistrations(t *testing.T) {
	r := New()
	if err := r.RegisterCommand(Command{Handler: func(context.Context, string) error { return nil }}); err == nil {
		t.Fatal("expected command validation error")
	}
	if err := r.RegisterMessageRenderer("note", nil); err == nil {
		t.Fatal("expected renderer validation error")
	}
	if err := r.RegisterMarkdownTransformer(nil); err == nil {
		t.Fatal("expected transformer validation error")
	}
}
