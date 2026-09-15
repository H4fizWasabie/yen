package codingagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/settings"
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
	resourceSettings, _ := settings.Load(workspace)
	if resourceSettings.Trusted != nil && !*resourceSettings.Trusted && os.Getenv("YEN_TRUST_PROJECT") != "1" {
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
	for _, configured := range resourceSettings.ContextFiles {
		path := configured
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil && strings.TrimSpace(string(data)) != "" {
			sections = append(sections, "["+path+"]\n"+strings.TrimSpace(string(data)))
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

type skillInfo struct {
	Name        string
	Description string
	Path        string
}

func SkillsMessage(workspace string) (agent.Message, bool) {
	paths := []string{filepath.Join(workspace, ".theoses", "skills"), filepath.Join(workspace, ".agents", "skills")}
	resourceSettings, _ := settings.Load(workspace)
	for _, configured := range resourceSettings.SkillDirs {
		if !filepath.IsAbs(configured) {
			configured = filepath.Join(workspace, configured)
		}
		paths = append(paths, configured)
	}
	if dir := os.Getenv("YEN_SKILLS_DIR"); dir != "" {
		paths = append(paths, dir)
	}
	var skills []skillInfo
	for _, root := range paths {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry == nil {
				return nil
			}
			if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if entry.IsDir() || entry.Name() != "SKILL.md" {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			name, description := parseSkillFrontmatter(string(data))
			if name != "" && description != "" {
				skills = append(skills, skillInfo{Name: name, Description: description, Path: path})
			}
			return nil
		})
	}
	if len(skills) == 0 {
		return agent.Message{}, false
	}
	var builder strings.Builder
	builder.WriteString("<available_skills>\nUse the read tool to load a matching skill file before applying it.\n")
	for _, skill := range skills {
		fmt.Fprintf(&builder, "<skill><name>%s</name><description>%s</description><location>%s</location></skill>\n", xmlEscape(skill.Name), xmlEscape(skill.Description), xmlEscape(skill.Path))
	}
	builder.WriteString("</available_skills>")
	content := builder.String()
	if len([]rune(content)) > 8000 {
		content = string([]rune(content)[:8000]) + "\n</available_skills>"
	}
	return agent.Message{Role: "system", Content: content}, true
}

func parseSkillFrontmatter(content string) (string, string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	values := make(map[string]string)
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	return values["name"], values["description"]
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	return strings.ReplaceAll(value, "'", "&apos;")
}
