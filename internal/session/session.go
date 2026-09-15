package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	ErrNothingToCompact = errors.New("nothing to compact")
	ErrAlreadyCompacted = errors.New("already compacted")
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

type CompactionPlan struct {
	FirstKeptEntryID string
	Messages         []Message
	TokensBefore     int
	PreviousSummary  string
}

type Message struct {
	Role               string   `json:"role"`
	Content            any      `json:"content"`
	Images             []string `json:"images,omitempty"`
	ToolCallID         string   `json:"toolCallId,omitempty"`
	StopReason         string   `json:"stopReason,omitempty"`
	ErrorMessage       string   `json:"errorMessage,omitempty"`
	ResponseID         string   `json:"responseId,omitempty"`
	ResponseModel      string   `json:"responseModel,omitempty"`
	RawStopReason      string   `json:"rawStopReason,omitempty"`
	Provider           string   `json:"provider,omitempty"`
	Model              string   `json:"model,omitempty"`
	Usage              *Usage   `json:"usage,omitempty"`
	Command            string   `json:"command,omitempty"`
	Output             string   `json:"output,omitempty"`
	ExitCode           *int     `json:"exitCode,omitempty"`
	Cancelled          bool     `json:"cancelled,omitempty"`
	Truncated          bool     `json:"truncated,omitempty"`
	FullOutputPath     string   `json:"fullOutputPath,omitempty"`
	ExcludeFromContext bool     `json:"excludeFromContext,omitempty"`
	Summary            string   `json:"summary,omitempty"`
}

type TimedMessage struct {
	Message
	Timestamp string
}

type sessionEntry struct {
	Type                string      `json:"type"`
	Version             int         `json:"version,omitempty"`
	ID                  string      `json:"id,omitempty"`
	Timestamp           string      `json:"timestamp"`
	CWD                 string      `json:"cwd,omitempty"`
	Channel             string      `json:"channel,omitempty"`
	ChannelSessionID    string      `json:"channelSessionId,omitempty"`
	ParentID            *string     `json:"parentId"`
	Message             *Message    `json:"message,omitempty"`
	Compaction          *Compaction `json:"compaction,omitempty"`
	Summary             string      `json:"summary,omitempty"`
	FirstKeptEntryID    string      `json:"firstKeptEntryId,omitempty"`
	FirstKeptEntryIndex *int        `json:"firstKeptEntryIndex,omitempty"`
	TokensBefore        int         `json:"tokensBefore,omitempty"`
	Usage               *Usage      `json:"usage,omitempty"`
	raw                 json.RawMessage
}

func (entry sessionEntry) MarshalJSON() ([]byte, error) {
	if len(entry.raw) == 0 {
		type plain sessionEntry
		return json.Marshal(plain(entry))
	}
	var object map[string]any
	if err := json.Unmarshal(entry.raw, &object); err != nil {
		return nil, err
	}
	object["id"] = entry.ID
	if entry.ParentID == nil {
		object["parentId"] = nil
	} else {
		object["parentId"] = *entry.ParentID
	}
	if entry.Message != nil {
		if message, ok := object["message"].(map[string]any); ok {
			if entry.Message.Role == "custom" && message["role"] == "hookMessage" {
				message["role"] = "custom"
			}
		} else {
			object["message"] = entry.Message
		}
	}
	if entry.Compaction != nil {
		object["summary"] = entry.Compaction.Summary
		object["firstKeptEntryId"] = entry.Compaction.FirstKeptEntryID
		object["tokensBefore"] = entry.Compaction.TokensBefore
		if entry.Compaction.Usage != nil {
			object["usage"] = entry.Compaction.Usage
		}
		delete(object, "firstKeptEntryIndex")
	}
	return json.Marshal(object)
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
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	line := 0
	headerFound := false
	for scanner.Scan() {
		line++
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		if !headerFound {
			var header sessionHeader
			if err := json.Unmarshal(scanner.Bytes(), &header); err != nil || header.Type != "session" || header.ID == "" {
				continue
			}
			s.header = header
			headerFound = true
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		entry.raw = append(json.RawMessage(nil), scanner.Bytes()...)
		s.entries = append(s.entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if line == 0 || !headerFound {
		return nil, fmt.Errorf("empty session")
	}
	if migrateSession(s) {
		if err := s.rewrite(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func migrateSession(s *Session) bool {
	version := s.header.Version
	if version == 0 {
		version = 1
	}
	changed := version < 3
	if version >= 3 {
		for i := range s.entries {
			s.normalizeCompaction(&s.entries[i])
		}
		return false
	}
	var parent *string
	for i := range s.entries {
		entry := &s.entries[i]
		if version < 2 {
			if entry.ID == "" {
				entry.ID = newEntryID(s.entries)
			}
			entry.ParentID = parent
			current := entry.ID
			parent = &current
		}
		if entry.Message != nil && entry.Message.Role == "hookMessage" {
			entry.Message.Role = "custom"
		}
		s.normalizeCompaction(entry)
		if version < 2 && entry.Compaction != nil && entry.FirstKeptEntryIndex != nil {
			index := *entry.FirstKeptEntryIndex
			// TypeScript counts the header in the JSONL entry array; Go stores it separately.
			index--
			if index >= 0 && index < len(s.entries) {
				entry.Compaction.FirstKeptEntryID = s.entries[index].ID
			}
			entry.FirstKeptEntryIndex = nil
		}
		if entry.Compaction != nil {
			entry.Summary, entry.FirstKeptEntryID, entry.TokensBefore, entry.Usage = "", "", 0, nil
		}
	}
	s.header.Version = 3
	return changed
}

func (s *Session) normalizeCompaction(entry *sessionEntry) {
	if entry.Type != "compaction" || entry.Compaction != nil {
		return
	}
	entry.Compaction = &Compaction{
		Summary: entry.Summary, FirstKeptEntryID: entry.FirstKeptEntryID,
		TokensBefore: entry.TokensBefore, Usage: entry.Usage,
	}
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

func (s *Session) TimedMessages() []TimedMessage {
	entries := s.activeEntries()
	messages := make([]TimedMessage, 0, len(entries))
	for _, entry := range entries {
		if entry.Message != nil {
			messages = append(messages, TimedMessage{Message: *entry.Message, Timestamp: entry.Timestamp})
		}
	}
	return messages
}

func (s *Session) LastTimestamp() string {
	if len(s.entries) > 0 && s.entries[len(s.entries)-1].Timestamp != "" {
		return s.entries[len(s.entries)-1].Timestamp
	}
	return s.header.Timestamp
}

// ContextMessages projects the active leaf context after the latest
// compaction boundary. Messages() remains the complete durable read-back.
func (s *Session) ContextMessages() []Message {
	entries := s.activeEntries()
	compactionIndex := -1
	for i, entry := range entries {
		if entry.Type == "compaction" && entry.Compaction != nil {
			compactionIndex = i
		}
	}
	if compactionIndex < 0 {
		return messagesFromEntries(entries)
	}
	compaction := entries[compactionIndex].Compaction
	firstKept := -1
	for i := 0; i < compactionIndex; i++ {
		if entries[i].ID == compaction.FirstKeptEntryID {
			firstKept = i
			break
		}
	}
	if firstKept < 0 {
		return s.Messages()
	}
	result := []Message{{Role: "user", Content: "The conversation history before this point was compacted into the following summary:\n\n<summary>\n" + compaction.Summary + "\n</summary>"}}
	for i := firstKept; i < compactionIndex; i++ {
		if entries[i].Message != nil {
			result = append(result, *entries[i].Message)
		}
	}
	for i := compactionIndex + 1; i < len(entries); i++ {
		if entries[i].Message != nil {
			result = append(result, *entries[i].Message)
		}
	}
	return result
}

func (s *Session) activeEntries() []sessionEntry {
	if len(s.entries) == 0 {
		return nil
	}
	byID := make(map[string]int, len(s.entries))
	for i, entry := range s.entries {
		if entry.ID != "" {
			byID[entry.ID] = i
		}
	}
	if len(byID) != len(s.entries) {
		return append([]sessionEntry(nil), s.entries...)
	}
	path := make([]sessionEntry, 0, len(s.entries))
	seen := make(map[string]bool, len(s.entries))
	index := len(s.entries) - 1
	for index >= 0 && !seen[s.entries[index].ID] {
		entry := s.entries[index]
		path = append(path, entry)
		seen[entry.ID] = true
		if entry.ParentID == nil {
			break
		}
		parent, ok := byID[*entry.ParentID]
		if !ok {
			break
		}
		index = parent
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}

func messagesFromEntries(entries []sessionEntry) []Message {
	messages := make([]Message, 0, len(entries))
	for _, entry := range entries {
		if entry.Message != nil {
			messages = append(messages, *entry.Message)
		}
	}
	return messages
}

func (s *Session) PrepareCompaction(keepRecentTurns int) (CompactionPlan, error) {
	if keepRecentTurns <= 0 {
		return CompactionPlan{}, fmt.Errorf("keep recent turns must be positive")
	}
	if len(s.entries) > 0 && s.entries[len(s.entries)-1].Type == "compaction" {
		return CompactionPlan{}, ErrAlreadyCompacted
	}
	start := 0
	previousSummary := ""
	for i := len(s.entries) - 1; i >= 0; i-- {
		if s.entries[i].Type != "compaction" || s.entries[i].Compaction == nil {
			continue
		}
		previousSummary = s.entries[i].Compaction.Summary
		for j, entry := range s.entries {
			if entry.ID == s.entries[i].Compaction.FirstKeptEntryID {
				start = j
				break
			}
		}
		break
	}
	userEntries := make([]int, 0)
	for i := start; i < len(s.entries); i++ {
		if s.entries[i].Message != nil && s.entries[i].Message.Role == "user" {
			userEntries = append(userEntries, i)
		}
	}
	if len(userEntries) <= keepRecentTurns {
		return CompactionPlan{}, ErrNothingToCompact
	}
	cut := userEntries[len(userEntries)-keepRecentTurns]
	plan := CompactionPlan{FirstKeptEntryID: s.entries[cut].ID, PreviousSummary: previousSummary}
	for i := start; i < cut; i++ {
		if s.entries[i].Message != nil {
			plan.Messages = append(plan.Messages, *s.entries[i].Message)
			plan.TokensBefore += estimateMessageTokens(*s.entries[i].Message)
		}
	}
	if len(plan.Messages) == 0 {
		return CompactionPlan{}, ErrNothingToCompact
	}
	return plan, nil
}

// PrepareCompactionByTokens keeps the newest context-visible entries within a
// conservative character-based token budget, matching the oracle's /4
// estimator and never cutting before a tool result's assistant turn.
func (s *Session) PrepareCompactionByTokens(keepRecentTokens int) (CompactionPlan, error) {
	if keepRecentTokens <= 0 {
		return CompactionPlan{}, fmt.Errorf("keep recent tokens must be positive")
	}
	if len(s.entries) > 0 && s.entries[len(s.entries)-1].Type == "compaction" {
		return CompactionPlan{}, ErrAlreadyCompacted
	}
	start, previousSummary := s.compactionStart()
	cutPoints := make([]int, 0)
	for i := start; i < len(s.entries); i++ {
		if s.entries[i].Message != nil && s.entries[i].Message.Role != "toolResult" {
			cutPoints = append(cutPoints, i)
		}
	}
	if len(cutPoints) == 0 {
		return CompactionPlan{}, ErrNothingToCompact
	}
	accumulated := 0
	cut := cutPoints[0]
	for i := len(s.entries) - 1; i >= start; i-- {
		if s.entries[i].Message == nil {
			continue
		}
		tokens := estimateMessageTokens(*s.entries[i].Message)
		if tokens == 0 {
			continue
		}
		accumulated += tokens
		if accumulated >= keepRecentTokens {
			for _, candidate := range cutPoints {
				if candidate >= i {
					cut = candidate
					break
				}
			}
			break
		}
	}
	if accumulated < keepRecentTokens {
		return CompactionPlan{}, ErrNothingToCompact
	}
	plan := CompactionPlan{FirstKeptEntryID: s.entries[cut].ID, PreviousSummary: previousSummary}
	for i := start; i < cut; i++ {
		if s.entries[i].Message != nil {
			plan.Messages = append(plan.Messages, *s.entries[i].Message)
			plan.TokensBefore += estimateMessageTokens(*s.entries[i].Message)
		}
	}
	if len(plan.Messages) == 0 {
		return CompactionPlan{}, ErrNothingToCompact
	}
	return plan, nil
}

func (s *Session) compactionStart() (int, string) {
	start, previousSummary := 0, ""
	for i := len(s.entries) - 1; i >= 0; i-- {
		if s.entries[i].Type != "compaction" || s.entries[i].Compaction == nil {
			continue
		}
		previousSummary = s.entries[i].Compaction.Summary
		for j, entry := range s.entries {
			if entry.ID == s.entries[i].Compaction.FirstKeptEntryID {
				start = j
				break
			}
		}
		break
	}
	return start, previousSummary
}

func estimateMessageTokens(message Message) int {
	data, err := json.Marshal(message.Content)
	if err != nil {
		return 0
	}
	return (len(data) + 3) / 4
}

// EstimateContextTokens uses the same conservative estimate for a context.
func EstimateContextTokens(messages []Message) int {
	total := 0
	for _, message := range messages {
		total += estimateContextMessageTokens(message)
	}
	return total
}

func estimateContextMessageTokens(message Message) int {
	chars := 0
	switch content := message.Content.(type) {
	case string:
		chars = len([]rune(content))
	case []ContentPart:
		for _, part := range content {
			switch part.Type {
			case "text", "thinking":
				chars += len([]rune(part.Text))
			case "toolCall":
				args, _ := json.Marshal(part.Arguments)
				chars += len([]rune(part.Name)) + len(args)
			}
		}
	default:
		data, err := json.Marshal(content)
		if err == nil {
			chars = len(data)
		}
	}
	chars += len(message.Images) * 4800
	return (chars + 3) / 4
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

func (s *Session) rewrite() error {
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".session-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := writeJSONLine(tmp, s.header); err != nil {
		tmp.Close()
		return err
	}
	for _, entry := range s.entries {
		if err := writeJSONLine(tmp, entry); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
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
