package adapters

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

type provider struct{}

func (provider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: "shared response", StopReason: "stop"}, nil
}

func TestTelegramAndDashboardUseOneCanonicalConversationWhenLinked(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, provider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	service := Service{Registry: registry, Runner: runner}
	telegram := Telegram{Service: service, Workspace: dir}
	if result, err := telegram.HandleMessage(context.Background(), "chat-1", "hello"); err != nil || result.FinalText != "shared response" {
		t.Fatalf("telegram = %#v, %v", result, err)
	}
	link, ok := registry.Get("telegram", "chat-1")
	if !ok || !linkOK(link, "telegram") {
		t.Fatalf("telegram link = %#v, %v", link, ok)
	}
	if _, err := registry.Link("dashboard", "tab-1", link.ConversationID, dir); err != nil {
		t.Fatal(err)
	}
	dashboard := Dashboard{Service: service, Workspace: dir}
	if result, err := dashboard.Send(context.Background(), link.ConversationID, "again"); err != nil || result.FinalText != "shared response" {
		t.Fatalf("dashboard = %#v, %v", result, err)
	}
}

func linkOK(link conversation.Link, adapter string) bool {
	return link.ConversationID != "" && link.Adapter == adapter
}

func TestAdaptersReconnectAndReadBackCanonicalSessionAndMemory(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "links.jsonl")
	queuePath := filepath.Join(dir, "queue.jsonl")
	registry, err := conversation.OpenRegistry(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(queuePath)
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, provider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	runner.Memory, err = memory.OpenEngine(filepath.Join(dir, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	telegram := Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}
	if _, err := telegram.HandleMessage(context.Background(), "chat-reconnect", "first"); err != nil {
		t.Fatal(err)
	}
	link, ok := registry.Get("telegram", "chat-reconnect")
	if !ok {
		t.Fatal("telegram link missing")
	}
	if _, err := registry.Link("dashboard", "tab-reconnect", link.ConversationID, dir); err != nil {
		t.Fatal(err)
	}
	if err := runner.Memory.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedRegistry, err := conversation.OpenRegistry(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	reopenedQueue, err := conversation.OpenQueue(queuePath)
	if err != nil {
		t.Fatal(err)
	}
	reopened := runtime.New(reopenedQueue, provider{}, nil)
	reopened.SessionPath = runner.SessionPath
	reopened.Memory, err = memory.OpenEngine(filepath.Join(dir, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Memory.Close()
	dashboard := Dashboard{Service: Service{Registry: reopenedRegistry, Runner: reopened}, Workspace: dir}
	if result, err := dashboard.Send(context.Background(), link.ConversationID, "second"); err != nil || result.FinalText != "shared response" {
		t.Fatalf("dashboard reconnect result=%#v err=%v", result, err)
	}
	stored, err := session.Open(filepath.Join(dir, link.ConversationID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages()) != 4 {
		t.Fatalf("session readback messages=%#v", stored.Messages())
	}
	episodes, err := reopened.Memory.Episodic.Recent(link.ConversationID, 8)
	if err != nil || len(episodes) != 2 {
		t.Fatalf("memory readback episodes=%#v err=%v", episodes, err)
	}
}
