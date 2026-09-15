// Package codingagent owns the product-facing coding tool set.
// The execution loop remains in package agent; channels and runtime depend on
// this seam instead of assembling coding tools themselves.
package codingagent

import (
	"os"
	"path/filepath"

	"github.com/H4fizWasabie/yen/internal/agent"
	providerpkg "github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/settings"
	"github.com/H4fizWasabie/yen/internal/tools"
)

func NewTools(workspace string) []agent.Tool {
	return newTools(workspace, nil, nil, true)
}

func NewToolsForSession(workspace string, current *session.Session) []agent.Tool {
	return newTools(workspace, current, nil, true)
}

func NewToolsForSessionWithProvider(workspace string, current *session.Session, provider agent.Provider) []agent.Tool {
	return newTools(workspace, current, provider, true)
}

// NewToolsForSessionWithProviderWithoutExternal returns the turn-scoped
// built-ins. Use NewExternalTools separately when external resources should
// live for the session instead of being recreated on every turn.
func NewToolsForSessionWithProviderWithoutExternal(workspace string, current *session.Session, provider agent.Provider) []agent.Tool {
	return newTools(workspace, current, provider, false)
}

func NewExternalTools() []agent.Tool { return loadExternalTools() }

func newTools(workspace string, current *session.Session, provider agent.Provider, includeExternal bool) []agent.Tool {
	resourceSettings, _ := settings.Load(workspace)
	bashOptions := tools.BashOptions{ShellPath: resourceSettings.ShellPath, CommandPrefix: resourceSettings.ShellCommandPrefix}
	result := []agent.Tool{
		tools.NewReadTool(workspace),
		tools.NewBashToolWithOptions(workspace, bashOptions),
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
		explorer := providerpkg.NewExplorerProvider()
		if explorer.APIKey != "" {
			result = append(result, newExploreTool(workspace, explorer))
		}
	}
	if current != nil {
		result[1] = newSessionBashTool(workspace, current, bashOptions)
		result = append(result,
			newWorkingNoteTool(current),
			operationalNotesTool{path: filepath.Join(workspace, ".theoses-go", "operational-notes.md")},
		)
	}
	if includeExternal {
		external := NewExternalTools()
		if os.Getenv("YEN_DEFER_EXTERNAL_TOOLS") == "1" {
			result = append(result, deferExternalTools(external)...)
		} else {
			result = append(result, external...)
		}
	}
	return result
}
