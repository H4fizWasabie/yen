package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Header struct {
	ID               string `json:"id"`
	ConversationID   string `json:"conversationId,omitempty"`
	WorkspaceID      string `json:"workspaceId,omitempty"`
	CWD              string `json:"cwd"`
	Channel          string `json:"channel"`
	ChannelSessionID string `json:"channelSessionId"`
}

type ContentPart struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments any    `json:"arguments,omitempty"`
}

type Usage struct {
	Input       int `json:"input,omitempty"`
	Output      int `json:"output,omitempty"`
	Reasoning   int `json:"reasoning,omitempty"`
	CacheRead   int `json:"cacheRead,omitempty"`
	CacheWrite  int `json:"cacheWrite,omitempty"`
	TotalTokens int `json:"totalTokens,omitempty"`
}

type Compaction struct {
	Summary          string `json:"summary"`
	FirstKeptEntryID string `json:"firstKeptEntryId"`
	TokensBefore     int    `json:"tokensBefore"`
	Usage            *Usage `json:"usage,omitempty"`
}

type Message struct {
	Role       string `json:"role"`
	Content    any    `json:"content"`
	ToolCallID string `json:"toolCallId,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
	Usage      *Usage `json:"usage,omitempty"`
}

type sessionEntry struct {
	Type             string      `json:"type"`
	Version          int         `json:"version,omitempty"`
	ID               string      `json:"id,omitempty"`
	Timestamp        string      `json:"timestamp"`
	CWD              string      `json:"cwd,omitempty"`
	Channel          string      `json:"channel,omitempty"`
	ChannelSessionID string      `json:"channelSessionId,omitempty"`
	ParentID         *string     `json:"parentId"`
	Message          *Message    `json:"message,omitempty"`
	Compaction       *Compaction `json:"compaction,omitempty"`
}

type sessionHeader struct {
	Type             string `json:"type"`
	Version          int    `json:"version"`
	ID               string `json:"id"`
	ConversationID   string `json:"conversationId,omitempty"`
	WorkspaceID      string `json:"workspaceId,omitempty"`
	Timestamp        string `json:"timestamp"`
	CWD              string `json:"cwd"`
	Channel          string `json:"channel,omitempty"`
	ChannelSessionID string `json:"channelSessionId,omitempty"`
}

type Session struct {
	path    string
	header  sessionHeader
	entries []sessionEntry
	flushed bool
}

func New(path string, header Header) *Session {
	return &Session{
		path: path,
		header: sessionHeader{
			Type:             "session",
			Version:          3,
			ID:               header.ID,
			ConversationID:   header.ConversationID,
			WorkspaceID:      header.WorkspaceID,
			Timestamp:        time.Now().UTC().Format(time.RFC3339Nano),
			CWD:              header.CWD,
			Channel:          header.Channel,
			ChannelSessionID: header.ChannelSessionID,
		},
	}
}

func Open(path string) (*Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	s := &Session{path: path, flushed: true}
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		if len(s.entries) == 0 && s.header.Type == "" {
			if err := json.Unmarshal(scanner.Bytes(), &s.header); err != nil || s.header.Type != "session" || s.header.ID == "" {
				return nil, fmt.Errorf("session header missing")
			}
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		s.entries = append(s.entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if line == 0 {
		return nil, fmt.Errorf("empty session")
	}
	return s, nil
}

func (s *Session) Append(message Message) (string, error) {
	if message.Role == "" {
		return "", fmt.Errorf("message role is required")
	}
	id := newEntryID(s.entries)
	var parentID *string
	if len(s.entries) > 0 {
		parent := s.entries[len(s.entries)-1].ID
		parentID = &parent
	}
	entry := sessionEntry{
		Type:      "message",
		ID:        id,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		ParentID:  parentID,
		Message:   &message,
	}
	s.entries = append(s.entries, entry)
	if message.Role == "assistant" {
		if !s.flushed {
			if err := s.publish(); err != nil {
				return "", err
			}
		} else if err := s.appendFile(entry); err != nil {
			return "", err
		}
	} else if s.flushed {
		if err := s.appendFile(entry); err != nil {
			return "", err
		}
	}
	return id, nil
}

func newEntryID(entries []sessionEntry) string {
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		seen[entry.ID] = struct{}{}
	}
	for i := 0; i < 100; i++ {
		var raw [4]byte
		if _, err := rand.Read(raw[:]); err != nil {
			panic(err)
		}
		id := hex.EncodeToString(raw[:])
		if _, ok := seen[id]; !ok {
			return id
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw[:])
}

func (s *Session) Messages() []Message {
	messages := make([]Message, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry.Message != nil {
			messages = append(messages, *entry.Message)
		}
	}
	return messages
}

// ContextMessages projects the active leaf context after the latest
// compaction boundary. Messages() remains the complete durable read-back.
func (s *Session) ContextMessages() []Message {
	compactionIndex := -1
	for i, entry := range s.entries {
		if entry.Type == "compaction" && entry.Compaction != nil {
			compactionIndex = i
		}
	}
	if compactionIndex < 0 {
		return s.Messages()
	}
	compaction := s.entries[compactionIndex].Compaction
	firstKept := -1
	for i := 0; i < compactionIndex; i++ {
		if s.entries[i].ID == compaction.FirstKeptEntryID {
			firstKept = i
			break
		}
	}
	if firstKept < 0 {
		return s.Messages()
	}
	result := []Message{{Role: "user", Content: "The conversation history before this point was compacted into the following summary:\n\n<summary>\n" + compaction.Summary + "\n</summary>"}}
	for i := firstKept; i < compactionIndex; i++ {
		if s.entries[i].Message != nil {
			result = append(result, *s.entries[i].Message)
		}
	}
	for i := compactionIndex + 1; i < len(s.entries); i++ {
		if s.entries[i].Message != nil {
			result = append(result, *s.entries[i].Message)
		}
	}
	return result
}

func (s *Session) AppendCompaction(summary, firstKeptEntryID string, tokensBefore int, usage *Usage) (string, error) {
	if summary == "" || firstKeptEntryID == "" {
		return "", fmt.Errorf("compaction summary and first kept entry are required")
	}
	id := newEntryID(s.entries)
	var parentID *string
	if len(s.entries) > 0 {
		parent := s.entries[len(s.entries)-1].ID
		parentID = &parent
	}
	entry := sessionEntry{
		Type: "compaction", ID: id, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), ParentID: parentID,
		Compaction: &Compaction{Summary: summary, FirstKeptEntryID: firstKeptEntryID, TokensBefore: tokensBefore, Usage: usage},
	}
	s.entries = append(s.entries, entry)
	if !s.flushed {
		return id, s.publish()
	}
	return id, s.appendFile(entry)
}

func (s *Session) publish() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := writeJSONLine(file, s.header); err != nil {
		return err
	}
	for _, entry := range s.entries {
		if err := writeJSONLine(file, entry); err != nil {
			return err
		}
	}
	s.flushed = true
	return nil
}

func (s *Session) appendFile(entry sessionEntry) error {
	file, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return writeJSONLine(file, entry)
}

func writeJSONLine(file *os.File, entry any) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = file.Write(data)
	return err
}
