package codingagent

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
			body := stripUTF8BOM(string(data))
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
	raw = stripUTF8BOM(raw)
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
	pattern := regexp.MustCompile(`\$\{(\d+|ARGUMENTS|@):-([^}]*)\}|\$\{@:(\d+)(?::(\d+))?\}|\$(ARGUMENTS|@|\d+)`)
	return pattern.ReplaceAllStringFunc(content, func(match string) string {
		groups := pattern.FindStringSubmatch(match)
		if groups[1] != "" {
			value := all
			if groups[1] != "@" && groups[1] != "ARGUMENTS" {
				value = promptArg(args, groups[1])
			}
			if value == "" {
				return groups[2]
			}
			return value
		}
		if groups[3] != "" {
			start, _ := strconv.Atoi(groups[3])
			if start < 1 {
				start = 1
			}
			start--
			end := len(args)
			if groups[4] != "" {
				length, _ := strconv.Atoi(groups[4])
				end = start + length
				if end > len(args) {
					end = len(args)
				}
			}
			if start >= len(args) {
				return ""
			}
			return strings.Join(args[start:end], " ")
		}
		if groups[5] == "@" || groups[5] == "ARGUMENTS" {
			return all
		}
		return promptArg(args, groups[5])
	})
}

func promptArg(args []string, number string) string {
	index, err := strconv.Atoi(number)
	if err != nil || index < 1 || index > len(args) {
		return ""
	}
	return args[index-1]
}

func parsePromptArgs(text string) []string {
	var args []string
	var current strings.Builder
	quote := rune(0)
	for _, char := range text {
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
		case char == '\'' || char == '"':
			quote = char
		case char == ' ' || char == '\t' || char == '\n':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(char)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// ExpandPrompt applies a local prompt template when text starts with /name.
func ExpandPrompt(workspace, text string) string {
	if !strings.HasPrefix(text, "/") {
		return text
	}
	if strings.HasPrefix(text, "/skill:") {
		value := strings.TrimPrefix(text, "/skill:")
		name, args := value, ""
		if index := strings.IndexByte(value, ' '); index >= 0 {
			name, args = value[:index], strings.TrimSpace(value[index+1:])
		}
		if path := findSkillPath(workspace, name); path != "" {
			if raw, err := os.ReadFile(path); err == nil {
				body := stripPromptFrontmatter(string(raw))
				block := `<skill name="` + xmlEscape(name) + `" location="` + xmlEscape(path) + `">` +
					"\nReferences are relative to " + xmlEscape(filepath.Dir(path)) + ".\n\n" + body + "\n</skill>"
				if args != "" {
					block += "\n\n" + args
				}
				return block
			}
		}
		return text
	}
	fields := parsePromptArgs(text[1:])
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

func findSkillPath(workspace, name string) string {
	if name == "" {
		return ""
	}
	workspace, _ = filepath.Abs(workspace)
	paths := []string{filepath.Join(workspace, ".theoses", "skills"), filepath.Join(workspace, ".agents", "skills")}
	if dir := os.Getenv("YEN_SKILLS_DIR"); dir != "" {
		paths = append(paths, dir)
	}
	for _, root := range paths {
		var found string
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry == nil || found != "" {
				return nil
			}
			if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if isDiscoverableSkillFile(path, entry) {
				data, readErr := os.ReadFile(path)
				if readErr == nil {
					declared, description := parseSkillFile(path, string(data))
					if declared == name && description != "" {
						found = path
					}
				}
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

func stripPromptFrontmatter(raw string) string {
	raw = stripUTF8BOM(raw)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return strings.TrimSpace(raw)
	}
	if end := strings.Index(raw[4:], "\n---"); end >= 0 {
		return strings.TrimSpace(raw[end+8:])
	}
	return strings.TrimSpace(raw)
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
