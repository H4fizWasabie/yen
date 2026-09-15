// Package rpc exposes the headless JSONL boundary used by integrations.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/codingagent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	providerpkg "github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	sessionpkg "github.com/H4fizWasabie/yen/internal/session"
)

type Server struct {
	Runner *runtime.Runner
	Link   conversation.Link

	writeMu                    sync.Mutex
	linkMu                     sync.RWMutex
	activeMu                   sync.Mutex
	active                     map[string]conversation.Turn
	modeMu                     sync.RWMutex
	steeringMode, followUpMode string
	wg                         sync.WaitGroup
}

type command struct {
	ID                 string     `json:"id,omitempty"`
	Type               string     `json:"type"`
	Message            string     `json:"message,omitempty"`
	Command            string     `json:"command,omitempty"`
	Images             []rpcImage `json:"images,omitempty"`
	StreamingBehavior  string     `json:"streamingBehavior,omitempty"`
	Since              string     `json:"since,omitempty"`
	EntryID            string     `json:"entryId,omitempty"`
	Name               string     `json:"name,omitempty"`
	Path               string     `json:"path,omitempty"`
	OutputPath         string     `json:"outputPath,omitempty"`
	ParentSession      string     `json:"parentSession,omitempty"`
	Provider           string     `json:"provider,omitempty"`
	Model              string     `json:"modelId,omitempty"`
	Level              string     `json:"level,omitempty"`
	Direction          string     `json:"direction,omitempty"`
	Mode               string     `json:"mode,omitempty"`
	KeepRecentTurns    int        `json:"keepRecentTurns,omitempty"`
	Enabled            *bool      `json:"enabled,omitempty"`
	ExcludeFromContext bool       `json:"excludeFromContext,omitempty"`
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
	case "bash":
		if strings.TrimSpace(request.Command) == "" {
			return errors.New("command is required")
		}
		current, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		workspace := link.WorkspaceID
		if workspace == "" {
			workspace = current.Header().CWD
		}
		for _, tool := range codingagent.NewToolsForSession(workspace, current) {
			if tool.Name() == "bash" {
				result, err := tool.Execute(ctx, map[string]any{"command": request.Command, "excludeFromContext": request.ExcludeFromContext})
				if err != nil {
					return err
				}
				return s.response(output, request.ID, request.Type, true, map[string]any{"output": result}, nil)
			}
		}
		return errors.New("bash tool is unavailable")
	case "abort":
		turn, ok := s.Runner.Active(link.ConversationID)
		if ok {
			if err := s.Runner.Cancel(turn.ID); err != nil {
				return err
			}
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"active": ok}, nil)
	case "abort_retry", "abort_bash":
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
		steeringMode, followUpMode := s.modes()
		return s.response(output, request.ID, request.Type, true, map[string]any{
			"sessionId": link.ConversationID, "isStreaming": active,
			"sessionName":   session.SessionName(),
			"provider":      func() string { name, _ := providerpkg.Describe(s.Runner.Provider); return name }(),
			"model":         func() string { _, model := providerpkg.Describe(s.Runner.Provider); return model }(),
			"thinkingLevel": providerpkg.ThinkingLevel(s.Runner.Provider),
			"autoRetry":     providerpkg.RetryEnabled(s.Runner.Provider),
			"steeringMode":  steeringMode, "followUpMode": followUpMode,
			"messageCount": len(session.Messages()), "pendingMessageCount": 0,
		}, nil)
	case "new_session":
		current, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		id := fmt.Sprintf("session-%d", time.Now().UnixNano())
		path := filepath.Join(filepath.Dir(current.Path()), id+".jsonl")
		header := current.Header()
		header.ID, header.ConversationID = id, id
		if request.ParentSession != "" {
			header.ParentSession = request.ParentSession
		}
		created := sessionpkg.New(path, header)
		if _, err := created.AppendSessionInfo(""); err != nil {
			return err
		}
		s.activateSession(link, id, path)
		return s.response(output, request.ID, request.Type, true, map[string]any{"cancelled": false, "sessionId": id, "path": created.Path()}, nil)
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
			return s.response(output, request.ID, request.Type, true, sessionpkg.Stats(session), nil)
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
	case "export_html":
		current, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		path := request.OutputPath
		if path == "" {
			path = current.Path() + ".html"
		}
		if err := os.WriteFile(path, []byte(sessionHTML(current)), 0o600); err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"path": path}, nil)
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
	case "clone":
		current, err := s.Runner.OpenSession(link)
		if err != nil {
			return err
		}
		cloneID := fmt.Sprintf("clone-%d", time.Now().UnixNano())
		header := current.Header()
		header.ID = cloneID
		path := filepath.Join(filepath.Dir(current.Path()), cloneID+".jsonl")
		clone, err := current.Fork(path, current.LeafID(), header)
		if err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"sessionId": cloneID, "path": clone.Path(), "leafId": clone.LeafID()}, nil)
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
	case "set_steering_mode", "set_follow_up_mode":
		if request.Mode != "all" && request.Mode != "one-at-a-time" {
			return errors.New("mode must be all or one-at-a-time")
		}
		s.modeMu.Lock()
		if request.Type == "set_steering_mode" {
			s.steeringMode = request.Mode
		} else {
			s.followUpMode = request.Mode
		}
		steering, followUp := s.steeringMode, s.followUpMode
		s.modeMu.Unlock()
		if steering == "" {
			steering = "one-at-a-time"
		}
		if followUp == "" {
			followUp = "one-at-a-time"
		}
		s.Runner.SetQueueModes(steering, followUp)
		return s.response(output, request.ID, request.Type, true, map[string]any{"mode": request.Mode}, nil)
	case "get_commands":
		return s.response(output, request.ID, request.Type, true, map[string]any{"commands": builtinCommands()}, nil)
	case "set_model":
		if strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.Model) == "" {
			return errors.New("provider and modelId are required")
		}
		if _, active := s.Runner.Active(link.ConversationID); active {
			return errors.New("cannot change model during an active operation")
		}
		currentProvider, _ := providerpkg.Describe(s.Runner.Provider)
		var configured agent.Provider
		var err error
		if strings.EqualFold(currentProvider, request.Provider) {
			configured, err = providerpkg.SetModel(s.Runner.Provider, request.Model)
		} else {
			configured, err = providerpkg.NewConfigured(request.Provider, request.Model)
		}
		if err != nil {
			return err
		}
		s.Runner.Provider = configured
		name, model := providerpkg.Describe(configured)
		return s.response(output, request.ID, request.Type, true, map[string]any{"provider": name, "model": model}, nil)
	case "get_available_models":
		models, err := providerpkg.AvailableModels(ctx, s.Runner.Provider)
		if err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"models": models}, nil)
	case "cycle_model":
		models, err := providerpkg.AvailableModels(ctx, s.Runner.Provider)
		if err != nil {
			return err
		}
		if len(models) < 2 {
			return s.response(output, request.ID, request.Type, true, nil, nil)
		}
		_, currentModel := providerpkg.Describe(s.Runner.Provider)
		index := -1
		for i, model := range models {
			if model.ID == currentModel {
				index = i
				break
			}
		}
		if index < 0 {
			index = 0
		}
		if request.Direction == "backward" {
			index = (index - 1 + len(models)) % len(models)
		} else {
			index = (index + 1) % len(models)
		}
		configured, err := providerpkg.SetModel(s.Runner.Provider, models[index].ID)
		if err != nil {
			return err
		}
		s.Runner.Provider = configured
		return s.response(output, request.ID, request.Type, true, map[string]any{"provider": models[index].Provider, "model": models[index].ID, "thinkingLevel": providerpkg.ThinkingLevel(configured)}, nil)
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
	case "set_auto_retry":
		if request.Enabled == nil {
			return errors.New("enabled is required")
		}
		configured, err := providerpkg.SetRetryEnabled(s.Runner.Provider, *request.Enabled)
		if err != nil {
			return err
		}
		s.Runner.Provider = configured
		return s.response(output, request.ID, request.Type, true, map[string]any{"enabled": providerpkg.RetryEnabled(configured)}, nil)
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

func (s *Server) modes() (string, string) {
	s.modeMu.RLock()
	defer s.modeMu.RUnlock()
	steering, followUp := s.steeringMode, s.followUpMode
	if steering == "" {
		steering = "one-at-a-time"
	}
	if followUp == "" {
		followUp = "one-at-a-time"
	}
	return steering, followUp
}

func builtinCommands() []map[string]string {
	return []map[string]string{
		{"name": "settings", "description": "Open settings menu", "source": "builtin"},
		{"name": "model", "description": "Select model", "source": "builtin"},
		{"name": "thinking", "description": "Set thinking level", "source": "builtin"},
		{"name": "session", "description": "Show session info and stats", "source": "builtin"},
		{"name": "working-note", "description": "Show the current Working Note", "source": "builtin"},
		{"name": "fork", "description": "Create a new fork", "source": "builtin"},
		{"name": "new", "description": "Start a new session", "source": "builtin"},
		{"name": "compact", "description": "Manually compact the session context", "source": "builtin"},
	}
}

func (s *Server) currentLink() conversation.Link {
	s.linkMu.RLock()
	defer s.linkMu.RUnlock()
	return s.Link
}

func (s *Server) switchSession(source string, current conversation.Link) (*sessionpkg.Session, error) {
	currentSession, err := s.Runner.OpenSession(current)
	if err != nil {
		return nil, err
	}
	destination := filepath.Join(filepath.Dir(currentSession.Path()), filepath.Base(source))
	imported, err := sessionpkg.Import(source, destination)
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
	current.WorkspaceID = workspaceID
	s.activateSession(current, conversationID, destination)
	return imported, nil
}

func (s *Server) activateSession(link conversation.Link, conversationID, path string) {
	s.Runner.SetSessionPath(conversationID, path)
	link.ConversationID = conversationID
	s.linkMu.Lock()
	s.Link = link
	s.linkMu.Unlock()
}

func sessionHTML(current *sessionpkg.Session) string {
	var builder strings.Builder
	builder.WriteString("<!doctype html><meta charset=\"utf-8\"><title>Yen session</title><main>")
	for _, message := range current.Messages() {
		builder.WriteString("<section><h2>")
		builder.WriteString(html.EscapeString(message.Role))
		builder.WriteString("</h2><pre>")
		builder.WriteString(html.EscapeString(contentText(message.Content)))
		builder.WriteString("</pre></section>")
	}
	builder.WriteString("</main>")
	return builder.String()
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
	if parts, ok := content.([]sessionpkg.ContentPart); ok {
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
