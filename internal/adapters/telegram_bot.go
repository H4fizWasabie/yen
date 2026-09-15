package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
			if err := b.HandleUpdate(ctx, update); err != nil {
				return err
			}
		}
	}
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
	if text == "/stop" || text == "/abort" {
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
	result, err := b.Adapter.HandleMessageWithReply(ctx, chatID, text, replyContext)
	stopTyping()
	if err != nil {
		return b.sendMessage(ctx, chatID, "Error: "+err.Error(), messageReplyID(update.Message.MessageID))
	}
	return b.sendMessage(ctx, chatID, result.FinalText, messageReplyID(update.Message.MessageID))
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
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	for _, chunk := range chunkTelegramText(text) {
		payloadValues := map[string]string{"chat_id": chatID, "text": chunk}
		if replyTo != nil {
			payloadValues["reply_to_message_id"] = strconv.FormatInt(*replyTo, 10)
		}
		payload, err := json.Marshal(payloadValues)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/sendMessage", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("telegram sendMessage returned %s", response.Status)
		}
	}
	return nil
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
