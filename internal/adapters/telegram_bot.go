package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type TelegramBot struct {
	Adapter     Telegram
	Token       string
	OwnerChatID string
	APIBase     string
	Client      *http.Client
	Offset      int64
}

const telegramMessageLimit = 4000
const telegramTypingInterval = 4 * time.Second

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message,omitempty"`
}
type telegramMessage struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text           string           `json:"text"`
	Caption        string           `json:"caption"`
	ReplyToMessage *telegramMessage `json:"reply_to_message,omitempty"`
}
type telegramResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

func (b *TelegramBot) Run(ctx context.Context) error {
	if b.Token == "" || b.OwnerChatID == "" {
		return errors.New("telegram token and owner chat ID are required")
	}
	if b.APIBase == "" {
		b.APIBase = "https://api.telegram.org"
	}
	if b.Client == nil {
		b.Client = http.DefaultClient
	}
	for {
		updates, err := b.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		for _, update := range updates {
			if update.UpdateID >= b.Offset {
				b.Offset = update.UpdateID + 1
			}
		}
		if err := b.handleUpdates(ctx, updates); err != nil {
			return err
		}
	}
}

func (b *TelegramBot) handleUpdates(ctx context.Context, updates []telegramUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	errs := make(chan error, len(updates))
	for _, update := range updates {
		go func(update telegramUpdate) { errs <- b.HandleUpdate(ctx, update) }(update)
	}
	for range updates {
		if err := <-errs; err != nil {
			return err
		}
	}
	return nil
}

func (b *TelegramBot) HandleUpdate(ctx context.Context, update telegramUpdate) error {
	if update.Message == nil || strconv.FormatInt(update.Message.Chat.ID, 10) != b.OwnerChatID {
		return nil
	}
	chatID := strconv.FormatInt(update.Message.Chat.ID, 10)
	text := strings.TrimSpace(telegramMessageText(update.Message))
	if text == "" {
		return nil
	}
	if isTelegramStopCommand(text) {
		if link, linked := b.Adapter.Service.Registry.Get("telegram", chatID); linked {
			if turn, ok := b.Adapter.Service.Runner.Active(link.ConversationID); ok {
				if err := b.Adapter.Service.Runner.Cancel(turn.ID); err != nil {
					return b.sendMessage(ctx, chatID, "Error: "+err.Error(), messageReplyID(update.Message.MessageID))
				}
				return b.sendMessage(ctx, chatID, "Stopped.", messageReplyID(update.Message.MessageID))
			}
		}
		return b.sendMessage(ctx, chatID, "No active turn.", messageReplyID(update.Message.MessageID))
	}
	replyContext := ""
	if update.Message.ReplyToMessage != nil {
		replyContext = telegramMessageText(update.Message.ReplyToMessage)
	}
	stopTyping := b.startTyping(ctx, chatID)
	result, err := b.Adapter.HandleMessageWithReplyEvents(ctx, chatID, text, replyContext, func(event agent.Event) {
		if event.Type == "tool_call" && event.Name != "" {
			_ = b.sendMessage(ctx, chatID, "Running "+event.Name+"...", messageReplyID(update.Message.MessageID))
		}
	})
	stopTyping()
	if err != nil {
		return b.sendMessage(ctx, chatID, "Error: "+err.Error(), messageReplyID(update.Message.MessageID))
	}
	return b.sendMessage(ctx, chatID, result.FinalText, messageReplyID(update.Message.MessageID))
}

func isTelegramStopCommand(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "stop", "halt", "/stop", "/cancel", "/abort":
		return true
	default:
		return false
	}
}

func telegramMessageText(message *telegramMessage) string {
	if message == nil || message.Text == "" {
		if message == nil {
			return ""
		}
		return message.Caption
	}
	return message.Text
}

func (b *TelegramBot) startTyping(ctx context.Context, chatID string) func() {
	typingCtx, cancel := context.WithCancel(ctx)
	go func() {
		tick := func() { _ = b.sendChatAction(typingCtx, chatID) }
		tick()
		ticker := time.NewTicker(telegramTypingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				tick()
			}
		}
	}()
	return cancel
}

func (b *TelegramBot) getUpdates(ctx context.Context) ([]telegramUpdate, error) {
	endpoint := strings.TrimRight(b.APIBase, "/") + "/bot" + url.PathEscape(b.Token) + "/getUpdates?timeout=25&offset=" + strconv.FormatInt(b.Offset, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram getUpdates returned %s", response.Status)
	}
	var result struct {
		telegramResponse
		Result []telegramUpdate `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, errors.New(result.Description)
	}
	return result.Result, nil
}

func (b *TelegramBot) sendMessage(ctx context.Context, chatID, text string, replyTo *int64) error {
	lastID := replyTo
	for _, section := range splitTelegramSections(text) {
		for _, chunk := range chunkTelegramText(section) {
			messageID, err := b.sendRichMessage(ctx, chatID, chunk, lastID)
			if err != nil {
				messageID, err = b.sendClassicMessage(ctx, chatID, chunk, lastID)
			}
			if err != nil {
				return err
			}
			if messageID != 0 {
				lastID = &messageID
			}
		}
	}
	return nil
}

func splitTelegramSections(text string) []string {
	var sections []string
	var current []string
	flush := func() {
		section := strings.TrimSpace(strings.Join(current, "\n"))
		if section != "" {
			sections = append(sections, section)
		}
		current = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "---" {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	if len(sections) == 0 {
		return []string{""}
	}
	return sections
}

func (b *TelegramBot) sendRichMessage(ctx context.Context, chatID, text string, replyTo *int64) (int64, error) {
	payload := map[string]any{
		"chat_id":      chatID,
		"rich_message": map[string]string{"markdown": text},
	}
	if replyTo != nil {
		payload["reply_parameters"] = map[string]int64{"message_id": *replyTo}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/sendRichMessage", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("telegram sendRichMessage returned %s", response.Status)
	}
	var result telegramResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return 0, err
	}
	if !result.OK {
		return 0, errors.New(result.Description)
	}
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	if len(result.Result) > 0 {
		_ = json.Unmarshal(result.Result, &sent)
	}
	return sent.MessageID, nil
}

func (b *TelegramBot) sendClassicMessage(ctx context.Context, chatID, text string, replyTo *int64) (int64, error) {
	payloadValues := map[string]string{"chat_id": chatID, "text": formatTelegramHTML(text), "parse_mode": "HTML"}
	if replyTo != nil {
		payloadValues["reply_to_message_id"] = strconv.FormatInt(*replyTo, 10)
	}
	payload, err := json.Marshal(payloadValues)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("telegram sendMessage returned %s", response.Status)
	}
	var result telegramResponse
	if json.Unmarshal(body, &result) != nil || !result.OK {
		return 0, nil
	}
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	_ = json.Unmarshal(result.Result, &sent)
	return sent.MessageID, nil
}

var (
	telegramFencePattern  = regexp.MustCompile("(?s)```[^\\n]*\\n(.*?)```")
	telegramCodePattern   = regexp.MustCompile("`([^`\\n]+)`")
	telegramLinkPattern   = regexp.MustCompile("[[]([^]]+)[]][(]([^)]+)[)]")
	telegramBoldPattern   = regexp.MustCompile("[*][*](.+?)[*][*]")
	telegramStrikePattern = regexp.MustCompile(`~~(.+?)~~`)
)

func formatTelegramHTML(markdown string) string {
	stash := make([]string, 0, 3)
	put := func(value string) string {
		stash = append(stash, value)
		return fmt.Sprintf("\\x00TELEGRAM_STASH_%d\\x00", len(stash)-1)
	}
	text := html.EscapeString(markdown)
	text = telegramFencePattern.ReplaceAllStringFunc(text, func(match string) string {
		body := strings.TrimSuffix(strings.TrimPrefix(match, "```"), "```")
		if newline := strings.IndexByte(body, '\n'); newline >= 0 {
			body = body[newline+1:]
		}
		return put("<pre><code>" + body + "</code></pre>")
	})
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] == '#' {
			if space := strings.IndexByte(trimmed, ' '); space > 0 && space <= 3 {
				lines[i] = "<b>" + trimmed[space+1:] + "</b>"
				continue
			}
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			lines[i] = "• " + trimmed[2:]
		}
	}
	text = strings.Join(lines, "\n")
	text = telegramCodePattern.ReplaceAllStringFunc(text, func(match string) string {
		return put("<code>" + match[1:len(match)-1] + "</code>")
	})
	text = telegramLinkPattern.ReplaceAllString(text, `<a href="$2">$1</a>`)
	text = telegramBoldPattern.ReplaceAllString(text, "<b>$1</b>")
	text = telegramStrikePattern.ReplaceAllString(text, "<s>$1</s>")
	for i, value := range stash {
		text = strings.ReplaceAll(text, fmt.Sprintf("\\x00TELEGRAM_STASH_%d\\x00", i), value)
	}
	return text
}

func (b *TelegramBot) sendChatAction(ctx context.Context, chatID string) error {
	payload, err := json.Marshal(map[string]string{"chat_id": chatID, "action": "typing"})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/sendChatAction", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("telegram sendChatAction returned %s", response.Status)
	}
	return nil
}

func messageReplyID(messageID int64) *int64 {
	if messageID == 0 {
		return nil
	}
	return &messageID
}

func chunkTelegramText(text string) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	chunks := make([]string, 0, (len(runes)+telegramMessageLimit-1)/telegramMessageLimit)
	for len(runes) > 0 {
		n := telegramMessageLimit
		if len(runes) < n {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}
