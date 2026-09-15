// Package codingagent owns the product-facing coding tool set.
// The execution loop remains in package agent; channels and runtime depend on
// this seam instead of assembling coding tools themselves.
package codingagent

import (
	"path/filepath"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/tools"
)

func NewTools(workspace string) []agent.Tool {
	return newTools(workspace, nil)
}

func NewToolsForSession(workspace string, current *session.Session) []agent.Tool {
	return newTools(workspace, current)
}

func newTools(workspace string, current *session.Session) []agent.Tool {
	result := []agent.Tool{
		tools.NewReadTool(workspace),
		tools.NewBashTool(workspace),
		tools.NewPowerShellTool(workspace),
		tools.NewEditTool(workspace),
		tools.NewWriteTool(workspace),
		tools.NewGrepTool(workspace),
		tools.NewFindTool(workspace),
		tools.NewListTool(workspace),
	}
	if current != nil {
		result = append(result,
			newWorkingNoteTool(current),
			operationalNotesTool{path: filepath.Join(workspace, ".theoses-go", "operational-notes.md")},
		)
	}
	return result
}
