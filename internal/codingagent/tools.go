// Package codingagent owns the product-facing coding tool set.
// The execution loop remains in package agent; channels and runtime depend on
// this seam instead of assembling coding tools themselves.
package codingagent

import (
	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/tools"
)

func NewTools(workspace string) []agent.Tool {
	return []agent.Tool{
		tools.NewReadTool(workspace),
		tools.NewBashTool(workspace),
		tools.NewPowerShellTool(workspace),
		tools.NewEditTool(workspace),
		tools.NewWriteTool(workspace),
		tools.NewGrepTool(workspace),
		tools.NewFindTool(workspace),
		tools.NewListTool(workspace),
	}
}
