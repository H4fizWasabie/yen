package codingagent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
)

const workingNotePromptPrefix = "<working_note>\nEstablished by earlier turns; verify this note if it contradicts current evidence.\n"

func WorkingNoteMessage(note string) agent.Message {
	runes := []rune(note)
	if len(runes) > 2000 {
		runes = append(append([]rune(nil), runes[:1000]...), append([]rune("\n...\n"), runes[len(runes)-1000:]...)...)
	}
	return agent.Message{
		Role:    "system",
		Content: workingNotePromptPrefix + string(runes) + "\n</working_note>",
	}
}

const contextPromptPrefix = "<project_context>\nThe following project guidance was loaded from context files; follow it unless current evidence requires otherwise.\n"

// ContextMessage loads the small, repository-local instruction surface used by
// the coding-agent layer. Files are ordered from the workspace root downward.
func ContextMessage(workspace string) (agent.Message, bool) {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return agent.Message{}, false
	}
	var dirs []string
	for dir := workspace; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == filepath.Dir(dir) {
			break
		}
	}
	var sections []string
	for i := len(dirs) - 1; i >= 0; i-- {
		for _, name := range []string{"AGENTS.override.md", "AGENTS.md", "CONTEXT.md"} {
			path := filepath.Join(dirs[i], name)
			data, err := os.ReadFile(path)
			if err != nil || len(data) == 0 {
				continue
			}
			text := strings.TrimSpace(string(data))
			if text != "" {
				sections = append(sections, "["+path+"]\n"+text)
			}
		}
	}
	if len(sections) == 0 {
		return agent.Message{}, false
	}
	content := contextPromptPrefix + strings.Join(sections, "\n\n") + "\n</project_context>"
	if len([]rune(content)) > 12000 {
		runes := []rune(content)
		content = string(runes[:12000]) + "\n</project_context>"
	}
	return agent.Message{Role: "system", Content: content}, true
}
