package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Header struct {
	ID               string `json:"id"`
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

type Message struct {
	Role       string `json:"role"`
	Content    any    `json:"content"`
	ToolCallID string `json:"toolCallId,omitempty"`
}

type sessionEntry struct {
	Type             string   `json:"type"`
	Version          int      `json:"version,omitempty"`
	ID               string   `json:"id,omitempty"`
	Timestamp        string   `json:"timestamp"`
	CWD              string   `json:"cwd,omitempty"`
	Channel          string   `json:"channel,omitempty"`
	ChannelSessionID string   `json:"channelSessionId,omitempty"`
	ParentID         *string  `json:"parentId"`
	Message          *Message `json:"message,omitempty"`
}

type sessionHeader struct {
	Type             string `json:"type"`
	Version          int    `json:"version"`
	ID               string `json:"id"`
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
		if line == 1 {
			if err := json.Unmarshal(scanner.Bytes(), &s.header); err != nil {
				return nil, fmt.Errorf("session line %d: %w", line, err)
			}
			if s.header.Type != "session" {
				return nil, fmt.Errorf("session header missing")
			}
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("session line %d: %w", line, err)
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
	id := fmt.Sprintf("entry-%d", len(s.entries)+1)
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

func (s *Session) Messages() []Message {
	messages := make([]Message, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry.Message != nil {
			messages = append(messages, *entry.Message)
		}
	}
	return messages
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
