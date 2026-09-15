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
	result, err := t.bash.ExecuteResult(ctx, args)
	t.log(ctx, args, result)
	return result.Output, err
}

func (t sessionBashTool) ExecuteRich(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
	result, err := t.bash.ExecuteResult(ctx, args)
	t.log(ctx, args, result)
	return agent.ToolResult{Text: result.Output}, err
}

func (t sessionBashTool) log(_ context.Context, args map[string]any, result tools.BashResult) {
	if command, ok := args["command"].(string); ok && command != "" {
		fullCommand := command
		if len([]rune(command)) > 200 {
			runes := []rune(command)
			command = string(runes[:200]) + "…"
		}
		_, _ = t.session.AppendWorkingNote("ran: " + command)
		exclude, _ := args["excludeFromContext"].(bool)
		_, _ = t.session.AppendBashExecution(fullCommand, result.Output, result.ExitCode, result.Cancelled, result.Truncated, exclude, result.FullOutputPath)
	}
}
