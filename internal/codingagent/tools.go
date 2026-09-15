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
	return newTools(workspace, nil, nil)
}

func NewToolsForSession(workspace string, current *session.Session) []agent.Tool {
	return newTools(workspace, current, nil)
}

func NewToolsForSessionWithProvider(workspace string, current *session.Session, provider agent.Provider) []agent.Tool {
	return newTools(workspace, current, provider)
}

func newTools(workspace string, current *session.Session, provider agent.Provider) []agent.Tool {
	result := []agent.Tool{
		tools.NewReadTool(workspace),
		tools.NewBashTool(workspace),
		tools.NewPowerShellTool(workspace),
		tools.NewEditTool(workspace),
		tools.NewWriteTool(workspace),
		tools.NewGrepTool(workspace),
		tools.NewFindTool(workspace),
		tools.NewListTool(workspace),
		convertDocTool{cwd: workspace},
		NewWebSearchTool(),
		generateImageTool{client: nil, session: current},
	}
	if provider != nil {
		result = append(result, newExploreTool(workspace, provider))
	}
	if current != nil {
		result[1] = newSessionBashTool(workspace, current)
		result = append(result,
			newWorkingNoteTool(current),
			operationalNotesTool{path: filepath.Join(workspace, ".theoses-go", "operational-notes.md")},
		)
	}
	result = append(result, loadExternalTools()...)
	return result
}
