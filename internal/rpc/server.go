// Package rpc exposes the headless JSONL boundary used by integrations.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

type Server struct {
	Runner *runtime.Runner
	Link   conversation.Link

	writeMu  sync.Mutex
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
	switch request.Type {
	case "prompt", "steer", "follow_up":
		if strings.TrimSpace(request.Message) == "" {
			return errors.New("message is required")
		}
		turn, err := s.Runner.Submit(s.Link, request.Message)
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
		turn, ok := s.Runner.Active(s.Link.ConversationID)
		if ok {
			if err := s.Runner.Cancel(turn.ID); err != nil {
				return err
			}
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"active": ok}, nil)
	case "get_state":
		session, err := s.Runner.OpenSession(s.Link)
		if err != nil {
			return err
		}
		_, active := s.Runner.Active(s.Link.ConversationID)
		return s.response(output, request.ID, request.Type, true, map[string]any{
			"sessionId": s.Link.ConversationID, "isStreaming": active,
			"messageCount": len(session.Messages()), "pendingMessageCount": 0,
		}, nil)
	case "get_messages":
		session, err := s.Runner.OpenSession(s.Link)
		if err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"messages": session.Messages()}, nil)
	case "get_tree", "get_entries", "get_session_stats", "get_last_assistant_text":
		session, err := s.Runner.OpenSession(s.Link)
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
			return s.response(output, request.ID, request.Type, true, map[string]any{"messageCount": len(session.Messages()), "leafId": session.LeafID()}, nil)
		}
	case "branch":
		session, err := s.Runner.OpenSession(s.Link)
		if err != nil {
			return err
		}
		if err := session.Branch(request.EntryID); err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"leafId": session.LeafID()}, nil)
	case "get_artifacts":
		session, err := s.Runner.OpenSession(s.Link)
		if err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"artifacts": session.Artifacts()}, nil)
	case "compact":
		keep := request.KeepRecentTurns
		if keep < 1 {
			keep = 2
		}
		if err := s.Runner.Compact(ctx, s.Link.ConversationID, keep); err != nil {
			return err
		}
		return s.response(output, request.ID, request.Type, true, map[string]any{"keepRecentTurns": keep}, nil)
	case "set_auto_compaction":
		if request.Enabled == nil {
			return errors.New("enabled is required")
		}
		s.Runner.AutoCompactDisabled = !*request.Enabled
		return s.response(output, request.ID, request.Type, true, map[string]any{"enabled": *request.Enabled}, nil)
	default:
		return fmt.Errorf("unsupported rpc command %q", request.Type)
	}
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
