package codingagent

import (
	"context"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/tools"
)

type sessionBashTool struct {
	bash    tools.BashTool
	session *session.Session
}

func newSessionBashTool(workspace string, current *session.Session) agent.Tool {
	return sessionBashTool{bash: tools.NewBashTool(workspace), session: current}
}

func (sessionBashTool) Name() string { return "bash" }

func (t sessionBashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	result, err := t.bash.Execute(ctx, args)
	if command, ok := args["command"].(string); ok && command != "" {
		if len([]rune(command)) > 200 {
			runes := []rune(command)
			command = string(runes[:200]) + "…"
		}
		_, _ = t.session.AppendWorkingNote("ran: " + command)
	}
	return result, err
}
