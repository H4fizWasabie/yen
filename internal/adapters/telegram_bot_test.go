package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/runtime"
)

func TestTelegramBotOwnerGuardAndSendMessage(t *testing.T) {
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
	var sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottoken/sendMessage" {
			sent.Add(1)
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["chat_id"] != "42" || payload["text"] != "shared response" {
				t.Errorf("payload=%#v", payload)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	update := telegramUpdate{Message: &telegramMessage{Text: "hello"}}
	update.Message.Chat.ID = 42
	if err := bot.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if sent.Load() != 1 {
		t.Fatalf("sent=%d", sent.Load())
	}
	unauthorized := telegramUpdate{Message: &telegramMessage{Text: "ignored"}}
	unauthorized.Message.Chat.ID = 7
	if err := bot.HandleUpdate(context.Background(), unauthorized); err != nil {
		t.Fatal(err)
	}
	if sent.Load() != 1 {
		t.Fatalf("unauthorized sent=%d", sent.Load())
	}
}

func TestTelegramBotPollsUpdatesAdvancesOffsetAndStops(t *testing.T) {
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
	var sends atomic.Int32
	sendComplete := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/getUpdates":
			if r.URL.Query().Get("offset") == "0" {
				_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"chat":{"id":42},"text":"poll me"}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		case "/bottoken/sendMessage":
			sends.Add(1)
			_, _ = w.Write([]byte(`{"ok":true}`))
			close(sendComplete)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	done := make(chan error, 1)
	go func() { done <- bot.Run(ctx) }()
	select {
	case <-sendComplete:
	case <-time.After(2 * time.Second):
		t.Fatal("telegram update was not delivered")
	}
	if sends.Load() != 1 {
		t.Fatalf("sends=%d", sends.Load())
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("run error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("telegram poller did not stop")
	}
	if bot.Offset != 8 {
		t.Fatalf("offset=%d, want 8", bot.Offset)
	}
}
