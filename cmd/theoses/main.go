package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/H4fizWasabie/theoses2-go/internal/agent"
	"github.com/H4fizWasabie/theoses2-go/internal/provider"
	"github.com/H4fizWasabie/theoses2-go/internal/session"
	"github.com/H4fizWasabie/theoses2-go/internal/tools"
)

func main() {
	prompt := flag.String("p", "", "run one non-interactive prompt")
	flag.Parse()
	if *prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: theoses -p PROMPT")
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	sessionPath := os.Getenv("THEOSES_SESSION_FILE")
	if sessionPath == "" {
		sessionPath = filepath.Join(cwd, ".theoses-go", "session.jsonl")
	}
	var current *session.Session
	if _, err := os.Stat(sessionPath); err == nil {
		current, err = session.Open(sessionPath)
	} else if os.IsNotExist(err) {
		current = session.New(sessionPath, session.Header{ID: "cli-session", CWD: cwd, Channel: "cli", ChannelSessionID: cwd})
	} else {
		fail(err)
	}
	if err != nil {
		fail(err)
	}

	baseURL := os.Getenv("THEOSES_OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := os.Getenv("THEOSES_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	client := provider.NewOpenAICompletions(baseURL, os.Getenv("OPENAI_API_KEY"), model)
	history := toAgentMessages(current.Messages())
	result, err := agent.RunFrom(context.Background(), client, []agent.Tool{tools.NewReadTool(cwd)}, history, *prompt)
	if err != nil {
		fail(err)
	}
	for _, message := range result.Messages[len(history):] {
		if _, err := current.Append(toSessionMessage(message)); err != nil {
			fail(err)
		}
	}
	fmt.Println(result.FinalText)
}

func toAgentMessages(messages []session.Message) []agent.Message {
	result := make([]agent.Message, 0, len(messages))
	for _, message := range messages {
		converted := agent.Message{Role: message.Role, ToolCallID: message.ToolCallID}
		if message.Role == "toolResult" {
			converted.Role = "tool"
		}
		if text, ok := message.Content.(string); ok {
			converted.Content = text
			result = append(result, converted)
			continue
		}
		data, err := json.Marshal(message.Content)
		if err != nil {
			continue
		}
		var parts []session.ContentPart
		if json.Unmarshal(data, &parts) != nil {
			continue
		}
		for _, part := range parts {
			if part.Type == "text" {
				converted.Content += part.Text
			}
			if part.Type == "toolCall" {
				args, _ := part.Arguments.(map[string]any)
				converted.ToolCalls = append(converted.ToolCalls, agent.ToolCall{ID: part.ID, Name: part.Name, Args: args})
			}
		}
		result = append(result, converted)
	}
	return result
}

func toSessionMessage(message agent.Message) session.Message {
	if message.Role == "tool" {
		return session.Message{Role: "toolResult", ToolCallID: message.ToolCallID, Content: []session.ContentPart{{Type: "text", Text: message.Content}}}
	}
	if len(message.ToolCalls) > 0 {
		parts := make([]session.ContentPart, 0, len(message.ToolCalls)+1)
		if message.Content != "" {
			parts = append(parts, session.ContentPart{Type: "text", Text: message.Content})
		}
		for _, call := range message.ToolCalls {
			parts = append(parts, session.ContentPart{Type: "toolCall", ID: call.ID, Name: call.Name, Arguments: call.Args})
		}
		return session.Message{Role: message.Role, Content: parts}
	}
	return session.Message{Role: message.Role, Content: message.Content}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
