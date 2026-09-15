package codingagent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/H4fizWasabie/yen/internal/settings"
)

type promptTemplate struct {
	Name, Description, Content, Path string
}

func loadPromptTemplates(workspace string) []promptTemplate {
	workspace, _ = filepath.Abs(workspace)
	paths := []string{filepath.Join(workspace, ".theoses", "prompts")}
	if configDir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(configDir, "yen", "prompts"))
	}
	resourceSettings, _ := settings.Load(workspace)
	if configured := os.Getenv("YEN_PROMPT_DIR"); configured != "" {
		paths = append(paths, configured)
	}
	paths = append(paths, resourceSettings.PromptDirs...)
	for i, path := range paths {
		if !filepath.IsAbs(path) {
			paths[i] = filepath.Join(workspace, path)
		}
	}
	var result []promptTemplate
	for _, root := range paths {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			path := filepath.Join(root, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			body := string(data)
			name := strings.TrimSuffix(entry.Name(), ".md")
			description, content := parsePromptTemplate(body)
			if description == "" {
				for _, line := range strings.Split(content, "\n") {
					if strings.TrimSpace(line) != "" {
						description = strings.TrimSpace(line)
						if len([]rune(description)) > 60 {
							description = string([]rune(description)[:60]) + "..."
						}
						break
					}
				}
			}
			result = append(result, promptTemplate{Name: name, Description: description, Content: content, Path: path})
		}
	}
	return result
}

func parsePromptTemplate(raw string) (description, content string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	content = raw
	if !strings.HasPrefix(raw, "---\n") {
		return "", strings.TrimSpace(content)
	}
	end := strings.Index(raw[4:], "\n---")
	if end < 0 {
		return "", strings.TrimSpace(content)
	}
	end += 4
	for _, line := range strings.Split(raw[4:end], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "description" {
			description = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	content = strings.TrimSpace(raw[end+4:])
	return description, content
}

func substitutePromptArgs(content string, args []string) string {
	all := strings.Join(args, " ")
	for i := len(args); i >= 1; i-- {
		content = strings.ReplaceAll(content, "$"+string(rune('0'+i)), args[i-1])
	}
	content = strings.ReplaceAll(content, "$ARGUMENTS", all)
	return strings.ReplaceAll(content, "$@", all)
}

// ExpandPrompt applies a local prompt template when text starts with /name.
func ExpandPrompt(workspace, text string) string {
	if !strings.HasPrefix(text, "/") {
		return text
	}
	fields := strings.Fields(text[1:])
	if len(fields) == 0 {
		return text
	}
	for _, template := range loadPromptTemplates(workspace) {
		if template.Name == fields[0] {
			return substitutePromptArgs(template.Content, fields[1:])
		}
	}
	return text
}

func PromptCommands(workspace string) []map[string]any {
	commands := make([]map[string]any, 0)
	for _, template := range loadPromptTemplates(workspace) {
		commands = append(commands, map[string]any{
			"name": template.Name, "description": template.Description, "source": "prompt",
			"sourceInfo": map[string]string{"path": template.Path},
		})
	}
	return commands
}
