// Package rpc exposes the headless JSONL boundary used by integrations.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	providerpkg "github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

type Server struct {
	Runner *runtime.Runner
	Link   conversation.Link

	writeMu  sync.Mutex
	linkMu   sync.RWMutex
	activeMu sync.Mutex
	active   map[string]conversation.Turn
	wg       sync.WaitGroup
}

type command struct {
	ID                string     `json:"id,omitempty"`
	Type              string     `json:"type"`
	Message           string     `json:"message,omitempty"`
	Images            []rpcImage `json:"images,omitempty"`
	StreamingBehavior string     `json:"streamingBehavior,omitempty"`
	Since             string     `json:"since,omitempty"`
	EntryID           string     `json:"entryId,omitempty"`
	Name              string     `json:"name,omitempty"`
	Path              string     `json:"path,omitempty"`
	Provider          string     `json:"provider,omitempty"`
	Model             string     `json:"modelId,omitempty"`
	Level             string     `json:"level,omitempty"`
	KeepRecentTurns   int        `json:"keepRecentTurns,omitempty"`
	Enabled           *bool      `json:"enabled,omitempty"`
}

type rpcImage struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
}

func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if s.Runner == nil || s.Runner.Queue == nil {
		return errors.New("rpc runner is required")
	}
	if s.active == nil {
		s.active = make(map[string]conversation.Turn)
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		var request command
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			if err := s.response(output, "", "", false, nil, err); err != nil {
				return err
			}
			continue
		}
		if err := s.handle(ctx, output, request); err != nil {
			if writeErr := s.response(output, request.ID, request.Type, false, nil, err); writeErr != nil {
				return writeErr
			}
		}
	}
	err := scanner.Err()
	s.wg.Wait()
	return err
}

func (s *Server) handle(ctx context.Context, output io.Writer, request command) error {
	link := s.currentLink()
	switch request.Type {
	case "prompt", "steer", "follow_up":
		if strings.TrimSpace(request.Message) == "" {
			return errors.New("message is required")
		}
		turn, err := s.Runner.Submit(link, request.Message)
		if err != nil {
			return err
		}
		s.activeMu.Lock()
		s.active[turn.ID] = turn
		s.activeMu.Unlock()
		if err := s.response(output, request.ID, request.Type, true, nil, nil); err != nil {
			return err
		}
		images := make([]string, 0, len(request.Images))
		for _, image := range request.Images {
			if image.Type == "image" && image.Data != "" {
				mime := image.MIMEType
				if mime == "" {
					mime = "application/octet-stream"
				}
				images = append(images, "data:"+mime+";base64,"+image.Data)
			}
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runPrompt(ctx, output, turn, images)
		}()
		return nil
	case "abort":
		turn, ok := s.Runner.Active(link.ConversationID)
		if ok {
			if err := s.Runner.Cancel(turn.ID); err != nil {
				return err
			}
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"active": ok}, nil)
	case "get_state":
		session, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		_, active := s.Runner.Active(link.ConversationID)
		return s.response(output, request.ID, request.Type, true, map[string]any{
			"sessionId": link.ConversationID, "isStreaming": active,
			"sessionName":   session.SessionName(),
			"provider":      func() string { name, _ := providerpkg.Describe(s.Runner.Provider); return name }(),
			"model":         func() string { _, model := providerpkg.Describe(s.Runner.Provider); return model }(),
			"thinkingLevel": providerpkg.ThinkingLevel(s.Runner.Provider),
			"messageCount":  len(session.Messages()), "pendingMessageCount": 0,
		}, nil)
	case "get_messages":
		session, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"messages": session.Messages()}, nil)
	case "get_tree", "get_entries", "get_session_stats", "get_last_assistant_text":
		session, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		switch request.Type {
		case "get_tree":
			return s.response(output, request.ID, request.Type, true, map[string]any{"leafId": session.LeafID(), "entries": session.Tree()}, nil)
		case "get_entries":
			entries := session.Tree()
			if request.Since != "" {
				for index, entry := range entries {
					if entry.ID == request.Since {
						entries = entries[index+1:]
						break
					}
				}
			}
			return s.response(output, request.ID, request.Type, true, map[string]any{"entries": entries}, nil)
		case "get_last_assistant_text":
			messages := session.Messages()
			for index := len(messages) - 1; index >= 0; index-- {
				if messages[index].Role == "assistant" {
					return s.response(output, request.ID, request.Type, true, map[string]any{"text": contentText(messages[index].Content)}, nil)
				}
			}
			return s.response(output, request.ID, request.Type, true, map[string]any{"text": ""}, nil)
		default:
			return s.response(output, request.ID, request.Type, true, sessionStats(session), nil)
		}
	case "branch":
		session, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		if err := session.Branch(request.EntryID); err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"leafId": session.LeafID()}, nil)
	case "get_artifacts":
		session, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"artifacts": session.Artifacts()}, nil)
	case "get_fork_messages":
		session, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		messages := make([]map[string]string, 0)
		for _, entry := range session.Tree() {
			if entry.Message != nil && entry.Message.Role == "user" {
				messages = append(messages, map[string]string{"entryId": entry.ID, "text": contentText(entry.Message.Content)})
			}
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"messages": messages}, nil)
	case "set_session_name":
		session, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		if _, err := session.AppendSessionInfo(request.Name); err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"name": session.SessionName()}, nil)
	case "fork":
		session, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		forkID := "fork-" + session.LeafID()
		path := request.Path
		if path == "" {
			path = filepath.Join(filepath.Dir(session.Path()), forkID+".jsonl")
		}
		header := session.Header()
		header.ID = forkID
		forked, err := session.Fork(path, request.EntryID, header)
		if err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"sessionId": forkID, "path": forked.Path(), "leafId": forked.LeafID()}, nil)
	case "import_session", "switch_session":
		if strings.TrimSpace(request.Path) == "" {
			return errors.New("path is required")
		}
		imported, err := s.switchSession(request.Path, link)
		if err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"sessionId": imported.Header().ID, "path": imported.Path(), "leafId": imported.LeafID(), "name": imported.SessionName(), "switched": true}, nil)
	case "compact":
		keep := request.KeepRecentTurns
		if keep < 1 {
			keep = 2
		}
		if err := s.Runner.Compact(ctx, link.ConversationID, keep); err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"keepRecentTurns": keep}, nil)
	case "set_auto_compaction":
		if request.Enabled == nil {
			return errors.New("enabled is required")
		}
		s.Runner.AutoCompactDisabled = !*request.Enabled
		return s.response(output, request.ID, request.Type, true, map[string]any{"enabled": *request.Enabled}, nil)
	case "set_model":
		if strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.Model) == "" {
			return errors.New("provider and modelId are required")
		}
		if _, active := s.Runner.Active(link.ConversationID); active {
			return errors.New("cannot change model during an active operation")
		}
		configured, err := providerpkg.NewConfigured(request.Provider, request.Model)
		if err != nil {
			return err
		}
		s.Runner.Provider = configured
		name, model := providerpkg.Describe(configured)
		return s.response(output, request.ID, request.Type, true, map[string]any{"provider": name, "model": model}, nil)
	case "set_thinking_level":
		if _, active := s.Runner.Active(link.ConversationID); active {
			return errors.New("cannot change thinking level during an active operation")
		}
		configured, err := providerpkg.SetThinkingLevel(s.Runner.Provider, request.Level)
		if err != nil {
			return err
		}
		s.Runner.Provider = configured
		return s.response(output, request.ID, request.Type, true, map[string]any{"level": providerpkg.ThinkingLevel(configured)}, nil)
	case "get_available_thinking_levels":
		return s.response(output, request.ID, request.Type, true, map[string]any{"levels": providerpkg.ThinkingLevels}, nil)
	case "cycle_thinking_level":
		current := providerpkg.ThinkingLevel(s.Runner.Provider)
		index := 0
		for i, level := range providerpkg.ThinkingLevels {
			if level == current {
				index = (i + 1) % len(providerpkg.ThinkingLevels)
				break
			}
		}
		configured, err := providerpkg.SetThinkingLevel(s.Runner.Provider, providerpkg.ThinkingLevels[index])
		if err != nil {
			return err
		}
		s.Runner.Provider = configured
		return s.response(output, request.ID, request.Type, true, map[string]any{"level": providerpkg.ThinkingLevel(configured)}, nil)
	default:
		return fmt.Errorf("unsupported rpc command %q", request.Type)
	}
}

func (s *Server) currentLink() conversation.Link {
	s.linkMu.RLock()
	defer s.linkMu.RUnlock()
	return s.Link
}

func (s *Server) switchSession(source string, current conversation.Link) (*session.Session, error) {
	currentSession, err := s.Runner.OpenSession(current)
	if err != nil {
		return nil, err
	}
	destination := filepath.Join(filepath.Dir(currentSession.Path()), filepath.Base(source))
	imported, err := session.Import(source, destination)
	if err != nil {
		return nil, err
	}
	header := imported.Header()
	conversationID := header.ConversationID
	if conversationID == "" {
		conversationID = header.ID
	}
	workspaceID := header.WorkspaceID
	if workspaceID == "" {
		workspaceID = current.WorkspaceID
	}
	s.Runner.SetSessionPath(conversationID, destination)
	current.ConversationID, current.WorkspaceID = conversationID, workspaceID
	s.linkMu.Lock()
	s.Link = current
	s.linkMu.Unlock()
	return imported, nil
}

func (s *Server) runPrompt(ctx context.Context, output io.Writer, turn conversation.Turn, images []string) {
	defer func() {
		s.activeMu.Lock()
		delete(s.active, turn.ID)
		s.activeMu.Unlock()
	}()
	_, result, err := s.Runner.RunSubmittedWithEventsAndImages(ctx, turn, images, func(delta string) {
		_ = s.event(output, "delta", map[string]any{"text": delta})
	}, func(event agent.Event) {
		_ = s.event(output, event.Type, map[string]any{"id": event.ID, "name": event.Name, "result": event.Result, "isError": event.IsError})
	})
	if err != nil {
		_ = s.event(output, "error", map[string]any{"message": err.Error()})
		return
	}
	_ = s.event(output, "message_end", map[string]any{"text": result.FinalText})
	_ = s.event(output, "done", map[string]any{})
}

func (s *Server) response(output io.Writer, id, command string, success bool, data any, err error) error {
	payload := map[string]any{"type": "response", "command": command, "success": success}
	if id != "" {
		payload["id"] = id
	}
	if data != nil {
		payload["data"] = data
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	return s.write(output, payload)
}

func (s *Server) event(output io.Writer, event string, data any) error {
	return s.write(output, map[string]any{"type": "event", "event": event, "data": data})
}

func (s *Server) write(output io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = fmt.Fprintf(output, "%s\n", data)
	return err
}

func contentText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	if parts, ok := content.([]session.ContentPart); ok {
		var builder strings.Builder
		for _, part := range parts {
			if part.Type == "text" {
				builder.WriteString(part.Text)
			}
		}
		return builder.String()
	}
	return fmt.Sprint(content)
}

func sessionStats(current *session.Session) map[string]any {
	stats := map[string]any{
		"sessionFile": current.Path(), "sessionId": current.Header().ID,
		"userMessages": 0, "assistantMessages": 0, "toolCalls": 0, "toolResults": 0,
		"totalMessages": 0,
		"tokens":        map[string]int{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0},
	}
	for _, message := range current.Messages() {
		stats["totalMessages"] = stats["totalMessages"].(int) + 1
		switch message.Role {
		case "user":
			stats["userMessages"] = stats["userMessages"].(int) + 1
		case "assistant":
			stats["assistantMessages"] = stats["assistantMessages"].(int) + 1
		case "toolResult":
			stats["toolResults"] = stats["toolResults"].(int) + 1
		}
		if message.Usage != nil {
			tokens := stats["tokens"].(map[string]int)
			tokens["input"] += message.Usage.Input
			tokens["output"] += message.Usage.Output
			tokens["cacheRead"] += message.Usage.CacheRead
			tokens["cacheWrite"] += message.Usage.CacheWrite
			tokens["total"] += message.Usage.TotalTokens
		}
		if parts, ok := message.Content.([]session.ContentPart); ok {
			for _, part := range parts {
				if part.Type == "toolCall" {
					stats["toolCalls"] = stats["toolCalls"].(int) + 1
				}
			}
		}
	}
	return stats
}
