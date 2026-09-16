package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
)

type TelegramBot struct {
	Adapter            Telegram
	Token              string
	OwnerChatID        string
	APIBase            string
	Client             *http.Client
	Offset             int64
	ToolPreferencePath string
	ArtifactDir        string
	attachmentMu       sync.Mutex
	toolMu             sync.Mutex
	toolDetail         bool
	toolOnce           sync.Once
}

const telegramMessageLimit = 4000
const telegramTypingInterval = 4 * time.Second
const telegramDownloadMaxBytes = 20 * 1024 * 1024

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
	Photo          []telegramPhoto  `json:"photo,omitempty"`
	Document       *telegramFile    `json:"document,omitempty"`
	Audio          *telegramFile    `json:"audio,omitempty"`
	Video          *telegramFile    `json:"video,omitempty"`
	Voice          *telegramFile    `json:"voice,omitempty"`
	VideoNote      *telegramFile    `json:"video_note,omitempty"`
	Animation      *telegramFile    `json:"animation,omitempty"`
	ReplyToMessage *telegramMessage `json:"reply_to_message,omitempty"`
}
type telegramPhoto struct {
	FileID string `json:"file_id"`
}
type telegramFile struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
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
	b.loadToolDetail()
	chatID := strconv.FormatInt(update.Message.Chat.ID, 10)
	text := strings.TrimSpace(telegramMessageText(update.Message))
	link, err := b.Adapter.Service.resolve("telegram", chatID, b.Adapter.Workspace)
	if err != nil {
		return err
	}
	current, err := b.Adapter.Service.Runner.OpenSession(link)
	if err != nil {
		return err
	}
	attachmentNote, image, err := b.storeAttachmentWithSession(ctx, update.Message, current)
	if err != nil {
		return b.sendMessage(ctx, chatID, "Error: "+err.Error(), messageReplyID(update.Message.MessageID))
	}
	if text == "" {
		text = attachmentNote
	} else if attachmentNote != "" {
		text += "\n\n" + attachmentNote
	}
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
	if enabled, ok := telegramToolDetailToggle(text); ok {
		b.setToolDetail(enabled)
		state := "off"
		if enabled {
			state = "on"
		}
		return b.sendMessage(ctx, chatID, "Tool call detail: "+state+".", messageReplyID(update.Message.MessageID))
	}
	replyContext := ""
	if update.Message.ReplyToMessage != nil {
		replyContext = telegramMessageText(update.Message.ReplyToMessage)
	}
	var statusMessageID int64
	var statusMu sync.Mutex
	var resultImages []string
	stopTyping := b.startTyping(ctx, chatID)
	result, err := b.Adapter.HandleMessageWithImagesEvents(ctx, chatID, text, replyContext, image, func(event agent.Event) {
		if event.Type == "tool_result" && event.Message != nil {
			resultImages = append(resultImages, event.Message.Images...)
		}
		if event.Type == "tool_call" && event.Name != "" {
			statusMu.Lock()
			defer statusMu.Unlock()
			b.toolMu.Lock()
			detail := b.toolDetail
			b.toolMu.Unlock()
			status := "Running " + event.Name + "..."
			if detail {
				status = telegramToolStatus(event)
			}
			if statusMessageID == 0 {
				statusMessageID, _ = b.sendMessageWithID(ctx, chatID, status, messageReplyID(update.Message.MessageID))
			} else {
				_ = b.editMessage(ctx, chatID, statusMessageID, status)
			}
		}
	})
	stopTyping()
	if err != nil {
		return b.sendMessage(ctx, chatID, "Error: "+err.Error(), messageReplyID(update.Message.MessageID))
	}
	if err := b.sendResultImages(ctx, chatID, resultImages, messageReplyID(update.Message.MessageID)); err != nil {
		return b.sendMessage(ctx, chatID, "Error sending generated image: "+err.Error(), messageReplyID(update.Message.MessageID))
	}
	if statusMessageID != 0 && result.FinalText != "" && len(splitTelegramSections(result.FinalText)) == 1 && len([]rune(result.FinalText)) <= telegramMessageLimit {
		if err := b.editMessage(ctx, chatID, statusMessageID, result.FinalText); err == nil {
			return nil
		}
	}
	return b.sendMessage(ctx, chatID, result.FinalText, messageReplyID(update.Message.MessageID))
}

func (b *TelegramBot) sendResultImages(ctx context.Context, chatID string, images []string, replyTo *int64) error {
	if len(images) > 1 {
		return b.sendMediaGroup(ctx, chatID, images)
	}
	for _, image := range images {
		if err := b.sendPhoto(ctx, chatID, image, replyTo); err != nil {
			return err
		}
	}
	return nil
}

func (b *TelegramBot) sendMediaGroup(ctx context.Context, chatID string, dataURLs []string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	media := make([]map[string]string, 0, len(dataURLs))
	for index, dataURL := range dataURLs {
		data, extension, err := decodeTelegramImage(dataURL)
		if err != nil {
			return err
		}
		field := fmt.Sprintf("file%d", index)
		part, err := writer.CreateFormFile(field, "generated"+extension)
		if err != nil {
			return err
		}
		if _, err := part.Write(data); err != nil {
			return err
		}
		media = append(media, map[string]string{"type": "photo", "media": "attach://" + field})
	}
	encoded, err := json.Marshal(media)
	if err != nil {
		return err
	}
	if err := writer.WriteField("media", string(encoded)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/sendMediaGroup", &body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
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
		return fmt.Errorf("telegram sendMediaGroup returned %s", response.Status)
	}
	return nil
}

func (b *TelegramBot) sendPhoto(ctx context.Context, chatID, dataURL string, replyTo *int64) error {
	data, extension, err := decodeTelegramImage(dataURL)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if replyTo != nil {
		if err := writer.WriteField("reply_parameters", fmt.Sprintf(`{"message_id":%d}`, *replyTo)); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("photo", "generated"+extension)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/sendPhoto", &body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
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
		return fmt.Errorf("telegram sendPhoto returned %s", response.Status)
	}
	return nil
}

func decodeTelegramImage(dataURL string) ([]byte, string, error) {
	comma := strings.IndexByte(dataURL, ',')
	if !strings.HasPrefix(dataURL, "data:image/") || comma < 0 {
		return nil, "", fmt.Errorf("unsupported image result")
	}
	header := dataURL[:comma]
	data, err := base64.StdEncoding.DecodeString(dataURL[comma+1:])
	if err != nil {
		return nil, "", err
	}
	extension := ".jpg"
	if strings.Contains(header, "png") {
		extension = ".png"
	} else if strings.Contains(header, "webp") {
		extension = ".webp"
	} else if strings.Contains(header, "gif") {
		extension = ".gif"
	}
	return data, extension, nil
}

func (b *TelegramBot) setToolDetail(enabled bool) {
	b.toolMu.Lock()
	b.toolDetail = enabled
	b.toolMu.Unlock()
	b.saveToolDetail()
}

func (b *TelegramBot) loadToolDetail() {
	b.toolOnce.Do(func() {
		if b.ToolPreferencePath == "" {
			return
		}
		data, err := os.ReadFile(b.ToolPreferencePath)
		if err != nil {
			return
		}
		var preference struct {
			ToolCallDetail bool `json:"toolCallDetail"`
		}
		if json.Unmarshal(data, &preference) == nil {
			b.toolMu.Lock()
			b.toolDetail = preference.ToolCallDetail
			b.toolMu.Unlock()
		}
	})
}

func (b *TelegramBot) saveToolDetail() {
	if b.ToolPreferencePath == "" {
		return
	}
	b.toolMu.Lock()
	data, err := json.Marshal(struct {
		ToolCallDetail bool `json:"toolCallDetail"`
	}{ToolCallDetail: b.toolDetail})
	b.toolMu.Unlock()
	if err != nil {
		return
	}
	_ = os.WriteFile(b.ToolPreferencePath, append(data, '\n'), 0600)
}

func (b *TelegramBot) storeAttachment(ctx context.Context, message *telegramMessage) (string, error) {
	note, _, err := b.storeAttachmentWithImage(ctx, message)
	return note, err
}

func (b *TelegramBot) storeAttachmentWithImage(ctx context.Context, message *telegramMessage) (string, string, error) {
	return b.storeAttachmentWithSession(ctx, message, nil)
}

func (b *TelegramBot) storeAttachmentWithSession(ctx context.Context, message *telegramMessage, current *session.Session) (string, string, error) {
	// ponytail: one attachment lock protects session JSONL writes; use per-conversation locks if attachment throughput matters.
	b.attachmentMu.Lock()
	defer b.attachmentMu.Unlock()
	if current != nil {
		var note, image string
		var err error
		err = session.WithPathLock(current.Path(), func() error {
			if _, statErr := os.Stat(current.Path()); statErr == nil {
				if err := current.Reload(); err != nil {
					return err
				}
			} else if !os.IsNotExist(statErr) {
				return statErr
			}
			note, image, err = b.storeAttachmentWithSessionLocked(ctx, message, current)
			return err
		})
		return note, image, err
	}
	return b.storeAttachmentWithSessionLocked(ctx, message, current)
}

func (b *TelegramBot) storeAttachmentWithSessionLocked(ctx context.Context, message *telegramMessage, current *session.Session) (string, string, error) {
	if current == nil && b.ArtifactDir == "" {
		return "", "", nil
	}
	fileID, name, kind, mime := "", "attachment", "media", ""
	if len(message.Photo) > 0 {
		fileID, name, kind, mime = message.Photo[len(message.Photo)-1].FileID, "photo.jpg", "photo", "image/jpeg"
	} else {
		file := message.Document
		if file != nil {
			kind = "document"
		}
		if file == nil {
			file = message.Audio
		}
		if file == nil {
			file = message.Video
		}
		if file == nil {
			file = message.Voice
		}
		if file == nil {
			file = message.VideoNote
		}
		if file == nil {
			file = message.Animation
		}
		if file != nil {
			fileID, name = file.FileID, file.FileName
			mime = file.MimeType
			if kind == "media" {
				kind = "media file"
			}
			if name == "" {
				name = "media"
			}
		}
	}
	if fileID == "" {
		return "", "", nil
	}
	path, data, err := "", []byte(nil), error(nil)
	if current != nil {
		data, err = b.downloadAttachmentData(ctx, fileID)
	} else {
		path, data, err = b.downloadAttachment(ctx, fileID, name)
	}
	if err != nil {
		return "", "", err
	}
	image := ""
	if kind == "photo" || strings.HasPrefix(mime, "image/") {
		if mime == "" {
			mime = "application/octet-stream"
		}
		image = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	if strings.TrimSpace(telegramMessageText(message)) != "" {
		if current != nil && kind != "photo" && !(kind == "document" && strings.HasPrefix(mime, "image/")) {
			if _, err := current.StoreArtifact("telegram document", name, data); err != nil {
				return "", "", err
			}
		}
		return "", image, nil
	}
	if kind == "photo" {
		return "User sent a photo without a caption. Describe or act on it as appropriate.", image, nil
	}
	if current != nil && !(kind == "document" && strings.HasPrefix(mime, "image/")) {
		artifact, err := current.StoreArtifact("telegram document", name, data)
		if err != nil {
			return "", "", err
		}
		path = artifact.Path
	}
	if mime == "" {
		mime = "unknown"
	}
	note := fmt.Sprintf("User sent a %s without a caption: %q (mime type %s, %d bytes). ", kind, filepath.Base(name), mime, len(data))
	return note + fmt.Sprintf("It has been stored as a document artifact at `%s`. Use convert_doc to read it if needed, and respond about it.", path), image, nil
}

func (b *TelegramBot) downloadAttachment(ctx context.Context, fileID, name string) (string, []byte, error) {
	data, err := b.downloadAttachmentData(ctx, fileID)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(b.ArtifactDir, 0700); err != nil {
		return "", nil, err
	}
	path := filepath.Join(b.ArtifactDir, filepath.Base(name))
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", nil, err
	}
	return path, data, nil
}

func (b *TelegramBot) downloadAttachmentData(ctx context.Context, fileID string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/getFile?file_id="+url.QueryEscape(fileID), nil)
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
		return nil, fmt.Errorf("telegram getFile returned %s", response.Status)
	}
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK || result.Result.FilePath == "" {
		return nil, errors.New(result.Description)
	}
	downloadURL := strings.TrimRight(b.APIBase, "/") + "/file/bot" + url.PathEscape(b.Token) + "/" + result.Result.FilePath
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	response, err = client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram file download returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, telegramDownloadMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > telegramDownloadMaxBytes {
		return nil, errors.New("telegram file is too large")
	}
	return data, nil
}

func telegramToolDetailToggle(text string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "/on tool call", "/on tool calls":
		return true, true
	case "/off tool call", "/off tool calls":
		return false, true
	default:
		return false, false
	}
}

func telegramToolStatus(event agent.Event) string {
	preview := ""
	for _, key := range []string{"command", "path", "query", "note"} {
		if value, ok := event.Args[key].(string); ok {
			preview = strings.Join(strings.Fields(value), " ")
			break
		}
	}
	if preview == "" {
		preview = "tool call"
	}
	runes := []rune(preview)
	if len(runes) > 140 {
		preview = string(runes[:137]) + "..."
	}
	return "Running " + event.Name + ": " + preview
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
	_, err := b.sendMessageWithID(ctx, chatID, text, replyTo)
	return err
}

func (b *TelegramBot) sendMessageWithID(ctx context.Context, chatID, text string, replyTo *int64) (int64, error) {
	lastID := replyTo
	for _, section := range splitTelegramSections(text) {
		for _, chunk := range chunkTelegramText(section) {
			messageID, err := b.sendRichMessage(ctx, chatID, chunk, lastID)
			if err != nil {
				messageID, err = b.sendClassicMessage(ctx, chatID, chunk, lastID)
			}
			if err != nil {
				return 0, err
			}
			if messageID != 0 {
				lastID = &messageID
			}
		}
	}
	if lastID == nil {
		return 0, nil
	}
	return *lastID, nil
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

func (b *TelegramBot) editMessage(ctx context.Context, chatID string, messageID int64, text string) error {
	if err := b.editRichMessage(ctx, chatID, messageID, text); err == nil {
		return nil
	}
	payload, err := json.Marshal(map[string]string{
		"chat_id":    chatID,
		"message_id": strconv.FormatInt(messageID, 10),
		"text":       formatTelegramHTML(text),
		"parse_mode": "HTML",
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/editMessageText", bytes.NewReader(payload))
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
		return fmt.Errorf("telegram editMessageText returned %s", response.Status)
	}
	return nil
}

func (b *TelegramBot) editRichMessage(ctx context.Context, chatID string, messageID int64, text string) error {
	payload, err := json.Marshal(map[string]any{
		"chat_id":      chatID,
		"message_id":   messageID,
		"rich_message": map[string]string{"markdown": text},
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.APIBase, "/")+"/bot"+url.PathEscape(b.Token)+"/editMessageText", bytes.NewReader(payload))
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
		return fmt.Errorf("telegram editMessageText rich returned %s", response.Status)
	}
	var result telegramResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New(result.Description)
	}
	return nil
}

var (
	telegramFencePattern     = regexp.MustCompile("(?s)```(\\w*)\\n(.*?)```")
	telegramCodePattern      = regexp.MustCompile("`([^`\\n]+)`")
	telegramLinkPattern      = regexp.MustCompile("[[]([^]]+)[]][(]([^)]+)[)]")
	telegramBoldPattern      = regexp.MustCompile("[*][*](.+?)[*][*]")
	telegramItalicPattern    = regexp.MustCompile("(^|[^*])[*]([^*\\n]+)[*]")
	telegramUnderlinePattern = regexp.MustCompile("__([^_\\n]+)__")
	telegramSpoilerPattern   = regexp.MustCompile(`[|][|](.+?)[|][|]`)
	telegramStrikePattern    = regexp.MustCompile(`~~(.+?)~~`)
)

func formatTelegramHTML(markdown string) string {
	stash := make([]string, 0, 3)
	put := func(value string) string {
		stash = append(stash, value)
		return fmt.Sprintf("\\x00TELEGRAM_STASH_%d\\x00", len(stash)-1)
	}
	text := telegramFencePattern.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := telegramFencePattern.FindStringSubmatch(match)
		return put("<pre><code" + func() string {
			if parts[1] == "" {
				return ">"
			}
			return ` class="language-` + html.EscapeString(parts[1]) + `">`
		}() + html.EscapeString(parts[2]) + "</code></pre>")
	})
	text = formatTelegramBlockquotes(text, put)
	text = html.EscapeString(text)
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
	text = telegramItalicPattern.ReplaceAllString(text, "$1<i>$2</i>")
	text = telegramUnderlinePattern.ReplaceAllString(text, "<u>$1</u>")
	text = telegramSpoilerPattern.ReplaceAllString(text, "<tg-spoiler>$1</tg-spoiler>")
	text = telegramStrikePattern.ReplaceAllString(text, "<s>$1</s>")
	for i, value := range stash {
		text = strings.ReplaceAll(text, fmt.Sprintf("\\x00TELEGRAM_STASH_%d\\x00", i), value)
	}
	return formatTelegramTables(text)
}

func formatTelegramBlockquotes(text string, put func(string) string) string {
	lines := strings.Split(text, "\n")
	formatted := make([]string, 0, len(lines))
	var block []string
	expandable := false
	flush := func() {
		if len(block) == 0 {
			return
		}
		tag := "<blockquote>"
		if expandable {
			tag = "<blockquote expandable>"
		}
		formatted = append(formatted, put(tag+html.EscapeString(strings.Join(block, "\n"))+"</blockquote>"))
		block = nil
		expandable = false
	}
	for _, line := range lines {
		if strings.HasPrefix(line, ">!") {
			expandable = true
			block = append(block, strings.TrimPrefix(strings.TrimPrefix(line, ">!"), " "))
			continue
		}
		if strings.HasPrefix(line, ">") {
			block = append(block, strings.TrimPrefix(strings.TrimPrefix(line, ">"), " "))
			continue
		}
		flush()
		formatted = append(formatted, line)
	}
	flush()
	return strings.Join(formatted, "\n")
}

func formatTelegramTables(text string) string {
	lines := strings.Split(text, "\n")
	formatted := make([]string, 0, len(lines))
	var table []string
	flush := func() {
		if len(table) == 0 {
			return
		}
		cells := make([][]string, 0, len(table))
		widths := []int(nil)
		for _, line := range table {
			parts := strings.Split(strings.TrimSpace(line), "|")
			if len(parts) < 3 {
				continue
			}
			row := make([]string, 0, len(parts)-2)
			for _, part := range parts[1 : len(parts)-1] {
				cell := strings.TrimSpace(strings.ReplaceAll(part, `\|`, "|"))
				row = append(row, cell)
			}
			cells = append(cells, row)
			for i, cell := range row {
				visible := len([]rune(html.UnescapeString(telegramTagPattern.ReplaceAllString(cell, ""))))
				if i >= len(widths) {
					widths = append(widths, make([]int, i+1-len(widths))...)
				}
				if visible > widths[i] {
					widths[i] = visible
				}
			}
		}
		if len(cells) == 0 {
			formatted = append(formatted, table...)
			table = nil
			return
		}
		output := make([]string, 0, len(cells)+1)
		for rowIndex, row := range cells {
			values := make([]string, 0, len(row))
			for i, cell := range row {
				visible := len([]rune(html.UnescapeString(telegramTagPattern.ReplaceAllString(cell, ""))))
				padding := 0
				if i < len(widths) {
					padding = widths[i] - visible
				}
				values = append(values, cell+strings.Repeat(" ", max(0, padding)))
			}
			output = append(output, strings.Join(values, "  "))
			if rowIndex == 0 && len(cells) > 1 {
				separators := make([]string, len(widths))
				for i, width := range widths {
					separators[i] = strings.Repeat("─", width)
				}
				output = append(output, strings.Join(separators, "  "))
			}
		}
		formatted = append(formatted, "<pre>"+strings.Join(output, "\n")+"</pre>")
		table = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") && len(trimmed) > 1 {
			if telegramTableDivider(trimmed) {
				if len(table) > 0 {
					continue
				}
			} else {
				table = append(table, trimmed)
				continue
			}
		}
		flush()
		formatted = append(formatted, line)
	}
	flush()
	return strings.Join(formatted, "\n")
}

var telegramTagPattern = regexp.MustCompile(`<[^>]+>`)

func telegramTableDivider(line string) bool {
	for _, char := range strings.Trim(line, "|") {
		if char != '-' && char != ':' && char != '|' && char != ' ' {
			return false
		}
	}
	return true
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
