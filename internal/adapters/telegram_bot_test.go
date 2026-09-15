package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

type longTelegramProvider struct{}

func (longTelegramProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: strings.Repeat("界", 5000), StopReason: "stop"}, nil
}

type blockingTelegramProvider struct {
	started chan struct{}
	release chan struct{}
}

type statusTelegramProvider struct{ calls int }

func (p *statusTelegramProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	p.calls++
	if p.calls == 1 {
		return agent.Response{ToolCalls: []agent.ToolCall{{ID: "read-1", Name: "read"}}, StopReason: "toolUse"}, nil
	}
	return agent.Response{Text: "done", StopReason: "stop"}, nil
}

type multiStatusTelegramProvider struct{ calls int }

func (p *multiStatusTelegramProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	p.calls++
	if p.calls == 1 {
		return agent.Response{ToolCalls: []agent.ToolCall{{ID: "one", Name: "one"}, {ID: "two", Name: "two"}}, StopReason: "toolUse"}, nil
	}
	return agent.Response{Text: "done", StopReason: "stop"}, nil
}

func (p blockingTelegramProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	close(p.started)
	<-p.release
	return agent.Response{Text: "done", StopReason: "stop"}, nil
}

func TestTelegramBotSendsTypingActionDuringTurn(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := blockingTelegramProvider{started: make(chan struct{}), release: make(chan struct{})}
	runner := runtime.New(queue, provider, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	typing := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottoken/sendChatAction" {
			typing <- struct{}{}
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	message := &telegramMessage{Text: "hello"}
	message.Chat.ID = 42
	done := make(chan error, 1)
	go func() { done <- bot.HandleUpdate(context.Background(), telegramUpdate{Message: message}) }()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	select {
	case <-typing:
	case <-time.After(time.Second):
		t.Fatal("typing action was not sent")
	}
	close(provider.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTelegramBotGroupsMultipleResultImages(t *testing.T) {
	var media []map[string]string
	var files int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendMediaGroup" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(r.FormValue("media")), &media); err != nil {
			t.Fatal(err)
		}
		for _, parts := range r.MultipartForm.File {
			files += len(parts)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	bot := &TelegramBot{Token: "token", APIBase: server.URL}
	result := agent.Result{Messages: []agent.Message{{Role: "tool", Images: []string{
		"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png")),
		"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("jpeg")),
	}}}}
	if err := bot.sendResultImages(context.Background(), "42", result, nil); err != nil {
		t.Fatal(err)
	}
	if len(media) != 2 || files != 2 || media[0]["media"] != "attach://file0" || media[1]["media"] != "attach://file1" {
		t.Fatalf("media=%#v files=%d", media, files)
	}
}

func TestTelegramBotReportsToolStatus(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, &statusTelegramProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	var messages []string
	var edited string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottoken/editMessageText" {
			var payload struct {
				RichMessage struct {
					Markdown string `json:"markdown"`
				} `json:"rich_message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			edited = payload.RichMessage.Markdown
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path == "/bottoken/sendRichMessage" {
			var payload struct {
				RichMessage struct {
					Markdown string `json:"markdown"`
				} `json:"rich_message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			messages = append(messages, payload.RichMessage.Markdown)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	update := telegramUpdate{Message: &telegramMessage{MessageID: 3, Text: "read it"}}
	update.Message.Chat.ID = 42
	if err := bot.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0] != "Running read..." || edited != "done" {
		t.Fatalf("messages=%#v edited=%q", messages, edited)
	}
}

func TestTelegramBotEditsOneStatusMessageAcrossToolCalls(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, &multiStatusTelegramProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	var sends, edits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/sendRichMessage":
			sends.Add(1)
		case "/bottoken/editMessageText":
			edits.Add(1)
		default:
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9}}`))
	}))
	defer server.Close()

	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	message := &telegramMessage{MessageID: 3, Text: "read it"}
	message.Chat.ID = 42
	if err := bot.HandleUpdate(context.Background(), telegramUpdate{Message: message}); err != nil {
		t.Fatal(err)
	}
	if sends.Load() != 1 || edits.Load() == 0 {
		t.Fatalf("sends=%d edits=%d, want one status send followed by edits", sends.Load(), edits.Load())
	}
}

type replyCaptureProvider struct {
	seen   string
	images []string
}

func (p *replyCaptureProvider) Next(_ context.Context, messages []agent.Message, _ []string) (agent.Response, error) {
	p.seen = messages[len(messages)-1].Content
	p.images = messages[len(messages)-1].Images
	return agent.Response{Text: "reply answer", StopReason: "stop"}, nil
}

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

func TestTelegramBotChunksLongReplies(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, longTelegramProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	var chunks []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendMessage" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		chunks = append(chunks, payload["text"])
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	update := telegramUpdate{Message: &telegramMessage{Text: "long"}}
	update.Message.Chat.ID = 42
	if err := bot.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 || strings.Join(chunks, "") != strings.Repeat("界", 5000) {
		t.Fatalf("chunks=%d reconstructed=%d", len(chunks), len([]rune(strings.Join(chunks, ""))))
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > telegramMessageLimit {
			t.Fatalf("chunk length=%d", len([]rune(chunk)))
		}
	}
}

func TestTelegramBotUsesRichMessageBeforeClassicFallback(t *testing.T) {
	var rich bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottoken/sendRichMessage" {
			rich = true
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["chat_id"] != "42" || payload["rich_message"].(map[string]any)["markdown"] != "hello" {
				t.Fatalf("payload=%#v", payload)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.Error(w, "fallback not expected", http.StatusNotFound)
	}))
	defer server.Close()
	bot := &TelegramBot{Token: "token", APIBase: server.URL}
	if err := bot.sendMessage(context.Background(), "42", "hello", nil); err != nil {
		t.Fatal(err)
	}
	if !rich {
		t.Fatal("rich message was not attempted")
	}
}

func TestTelegramBotSplitsSectionsAndThreadsReplies(t *testing.T) {
	var targets []int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			ReplyParameters *struct {
				MessageID int64 `json:"message_id"`
			} `json:"reply_parameters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.ReplyParameters != nil {
			targets = append(targets, payload.ReplyParameters.MessageID)
		}
		messageID := int64(10 + len(targets))
		_, _ = w.Write([]byte(fmt.Sprintf(`{"ok":true,"result":{"message_id":%d}}`, messageID)))
	}))
	defer server.Close()
	bot := &TelegramBot{Token: "token", APIBase: server.URL}
	replyTo := int64(9)
	if err := bot.sendMessage(context.Background(), "42", "one\n---\ntwo", &replyTo); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0] != 9 || targets[1] != 11 {
		t.Fatalf("reply targets=%v, want [9 11]", targets)
	}
}

func TestSplitTelegramSectionsMatchesTypeScriptBoundary(t *testing.T) {
	got := splitTelegramSections(" first \n---\n second \n---\n")
	if strings.Join(got, "|") != "first|second" {
		t.Fatalf("sections=%#v", got)
	}
}

func TestFormatTelegramHTMLCoversClassicFallback(t *testing.T) {
	got := formatTelegramHTML("# Title\n- **bold** `code`\n~~old~~ [link](https://example.com)")
	want := "<b>Title</b>\n• <b>bold</b> <code>code</code>\n<s>old</s> <a href=\"https://example.com\">link</a>"
	if got != want {
		t.Fatalf("formatted=%q, want %q", got, want)
	}
	if got := formatTelegramHTML("```\n<safe>\n```"); got != "<pre><code>&lt;safe&gt;\n</code></pre>" {
		t.Fatalf("fenced=%q", got)
	}
	got = formatTelegramHTML("> quote\n>! expandable\n\n*italic* __under__ ||secret||")
	want = "<blockquote expandable>quote\nexpandable</blockquote>\n\n<i>italic</i> <u>under</u> <tg-spoiler>secret</tg-spoiler>"
	if got != want {
		t.Fatalf("extended formatting=%q, want %q", got, want)
	}
}

func TestChunkTelegramTextMatchesTypeScriptLimit(t *testing.T) {
	chunks := chunkTelegramText(strings.Repeat("x", 4001))
	if len(chunks) != 2 || len([]rune(chunks[0])) != 4000 || len([]rune(chunks[1])) != 1 {
		t.Fatalf("chunks=%d lengths=%d,%d", len(chunks), len([]rune(chunks[0])), len([]rune(chunks[1])))
	}
}

func TestTelegramBotCarriesReplyContextAndReplyTarget(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &replyCaptureProvider{}
	runner := runtime.New(queue, provider, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	var replyTarget string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottoken/sendRichMessage" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		replyTarget = payload["reply_to_message_id"]
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	update := telegramUpdate{Message: &telegramMessage{MessageID: 9, Text: "answer this"}}
	update.Message.Chat.ID = 42
	update.Message.ReplyToMessage = &telegramMessage{MessageID: 8, Text: "quoted source"}
	if err := bot.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	want := "[Quoted message context]\nquoted source\n[/Quoted message context]\n\nanswer this"
	if provider.seen != want || replyTarget != "9" {
		t.Fatalf("prompt=%q replyTarget=%q", provider.seen, replyTarget)
	}
}

func TestTelegramBotUsesCaptionForMessageAndReply(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &replyCaptureProvider{}
	runner := runtime.New(queue, provider, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	update := telegramUpdate{Message: &telegramMessage{Caption: "answer this caption"}}
	update.Message.Chat.ID = 42
	update.Message.ReplyToMessage = &telegramMessage{Caption: "quoted caption"}
	if err := bot.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	want := "[Quoted message context]\nquoted caption\n[/Quoted message context]\n\nanswer this caption"
	if provider.seen != want {
		t.Fatalf("prompt=%q", provider.seen)
	}
}

func TestTelegramBotRecognizesStopAliases(t *testing.T) {
	for _, command := range []string{"stop", "HALT", "/STOP", "/cancel", "/abort"} {
		if !isTelegramStopCommand(command) {
			t.Errorf("command %q was not recognized", command)
		}
	}
	for _, text := range []string{"", "stop now", "/stopping"} {
		if isTelegramStopCommand(text) {
			t.Errorf("text %q was recognized", text)
		}
	}
}

func TestTelegramToolDetailToggleAndPreview(t *testing.T) {
	if enabled, ok := telegramToolDetailToggle("/ON TOOL CALLS"); !ok || !enabled {
		t.Fatal("tool detail on was not recognized")
	}
	if enabled, ok := telegramToolDetailToggle("/off tool call"); !ok || enabled {
		t.Fatal("tool detail off was not recognized")
	}
	status := telegramToolStatus(agent.Event{Type: "tool_call", Name: "bash", Args: map[string]any{"command": "  echo   hello  "}})
	if status != "Running bash: echo hello" {
		t.Fatalf("status=%q", status)
	}
}

func TestTelegramToolDetailPreferenceSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telegram-preferences.json")
	first := &TelegramBot{ToolPreferencePath: path}
	first.setToolDetail(true)
	second := &TelegramBot{ToolPreferencePath: path}
	second.loadToolDetail()
	second.toolMu.Lock()
	got := second.toolDetail
	second.toolMu.Unlock()
	if !got {
		t.Fatal("tool detail preference was not persisted")
	}
}

func TestTelegramBotStoresDocumentAttachmentForReadTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"documents/report.txt"}}`))
		case "/file/bottoken/documents/report.txt":
			_, _ = w.Write([]byte("attachment contents"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	bot := &TelegramBot{Token: "token", APIBase: server.URL, ArtifactDir: dir}
	note, err := bot.storeAttachment(context.Background(), &telegramMessage{Document: &telegramFile{FileID: "file-1", FileName: "report.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "attachment contents" || !strings.Contains(note, filepath.Join(dir, "report.txt")) {
		t.Fatalf("data=%q note=%q", data, note)
	}
}

func TestTelegramBotAttachmentNoteIncludesDocumentMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"documents/report.txt"}}`))
		case "/file/bottoken/documents/report.txt":
			_, _ = w.Write([]byte("attachment contents"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bot := &TelegramBot{Token: "token", APIBase: server.URL, ArtifactDir: t.TempDir()}
	note, err := bot.storeAttachment(context.Background(), &telegramMessage{Document: &telegramFile{
		FileID: "file-1", FileName: "report.txt", MimeType: "text/plain",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `User sent a document without a caption: "report.txt" (mime type text/plain, 19 bytes). `
	if !strings.Contains(note, want) {
		t.Fatalf("note=%q, want substring %q", note, want)
	}
	if !strings.Contains(note, "Use convert_doc to read it if needed") {
		t.Fatalf("note=%q, missing convert_doc guidance", note)
	}
}

func TestTelegramBotCaptionlessPhotoUsesPhotoPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/photo.jpg"}}`))
		case "/file/bottoken/photos/photo.jpg":
			_, _ = w.Write([]byte("jpeg"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bot := &TelegramBot{Token: "token", APIBase: server.URL, ArtifactDir: t.TempDir()}
	note, _, err := bot.storeAttachmentWithImage(context.Background(), &telegramMessage{Photo: []telegramPhoto{{FileID: "photo-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if note != "User sent a photo without a caption. Describe or act on it as appropriate." {
		t.Fatalf("note=%q", note)
	}
}

func TestTelegramBotPassesPhotoToProviderAsImageContent(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &replyCaptureProvider{}
	runner := runtime.New(queue, provider, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/photo.jpg"}}`))
		case "/file/bottoken/photos/photo.jpg":
			_, _ = w.Write([]byte("jpeg bytes"))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL, ArtifactDir: filepath.Join(dir, "artifacts")}
	update := telegramUpdate{Message: &telegramMessage{MessageID: 4, Caption: "inspect this"}}
	update.Message.Chat.ID = 42
	update.Message.Photo = []telegramPhoto{{FileID: "photo-1"}}
	if err := bot.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if len(provider.images) != 1 || !strings.HasPrefix(provider.images[0], "data:image/jpeg;base64,") {
		t.Fatalf("images=%#v", provider.images)
	}
}

func TestTelegramBotRecordsDocumentInSharedSessionArtifacts(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, &replyCaptureProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"documents/report.txt"}}`))
		case "/file/bottoken/documents/report.txt":
			_, _ = w.Write([]byte("attachment contents"))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9}}`))
		}
	}))
	defer server.Close()

	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL, ArtifactDir: filepath.Join(dir, "legacy-artifacts")}
	message := &telegramMessage{MessageID: 4, Caption: "inspect this", Document: &telegramFile{FileID: "file-1", FileName: "report.txt", MimeType: "text/plain"}}
	message.Chat.ID = 42
	if err := bot.HandleUpdate(context.Background(), telegramUpdate{Message: message}); err != nil {
		t.Fatal(err)
	}

	link, ok := registry.Get("telegram", "42")
	if !ok {
		t.Fatal("telegram link was not created")
	}
	opened, err := session.Open(filepath.Join(dir, link.ConversationID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := opened.ArtifactCatalog(1000); !strings.Contains(got, "telegram document") || !strings.Contains(got, "report.txt") {
		t.Fatalf("artifact catalog=%q", got)
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

func TestTelegramBotHandlesPollBatchConcurrently(t *testing.T) {
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottoken/sendMessage" || r.URL.Path == "/bottoken/sendRichMessage" {
			sends.Add(1)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	updates := []telegramUpdate{
		{Message: &telegramMessage{Text: "first"}},
		{Message: &telegramMessage{Text: "second"}},
	}
	for i := range updates {
		updates[i].Message.Chat.ID = 42
	}
	if err := bot.handleUpdates(context.Background(), updates); err != nil {
		t.Fatal(err)
	}
	if sends.Load() != 2 {
		t.Fatalf("sent=%d, want 2", sends.Load())
	}
}

func TestTelegramBotStopConfirmsCancellation(t *testing.T) {
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
	link, err := registry.Resolve("telegram", "42", dir)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := runner.Submit(link, "long request")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := queue.Claim(link.ConversationID); err != nil || !ok {
		t.Fatalf("claim turn=%#v ok=%v err=%v", turn, ok, err)
	}
	var response string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottoken/sendMessage" {
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			response = payload["text"]
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	bot := &TelegramBot{Adapter: Telegram{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}, Token: "token", OwnerChatID: "42", APIBase: server.URL}
	update := telegramUpdate{Message: &telegramMessage{Text: "/stop"}}
	update.Message.Chat.ID = 42
	if err := bot.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if response != "Stopped." {
		t.Fatalf("stop response=%q", response)
	}
	if active := queue.Active(link.ConversationID); len(active) != 0 {
		t.Fatalf("active after stop=%#v", active)
	}
}
