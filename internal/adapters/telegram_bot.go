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
)

type TelegramBot struct {
	Adapter     Telegram
	Token       string
	OwnerChatID string
	APIBase     string
	Client      *http.Client
	Offset      int64
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message,omitempty"`
}
type telegramMessage struct {
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text string `json:"text"`
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
	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return nil
	}
	if text == "/stop" || text == "/abort" {
		if link, linked := b.Adapter.Service.Registry.Get("telegram", chatID); linked {
			if turn, ok := b.Adapter.Service.Runner.Active(link.ConversationID); ok {
				if err := b.Adapter.Service.Runner.Cancel(turn.ID); err != nil {
					return b.sendMessage(ctx, chatID, "Error: "+err.Error())
				}
				return b.sendMessage(ctx, chatID, "Stopped.")
			}
		}
		return b.sendMessage(ctx, chatID, "No active turn.")
	}
	result, err := b.Adapter.HandleMessage(ctx, chatID, text)
	if err != nil {
		return b.sendMessage(ctx, chatID, "Error: "+err.Error())
	}
	return b.sendMessage(ctx, chatID, result.FinalText)
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

func (b *TelegramBot) sendMessage(ctx context.Context, chatID, text string) error {
	payload, err := json.Marshal(map[string]string{"chat_id": chatID, "text": text})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/sendMessage", bytes.NewReader(payload))
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
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("telegram sendMessage returned %s", response.Status)
	}
	return nil
}
