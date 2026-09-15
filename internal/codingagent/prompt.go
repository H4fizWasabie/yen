package codingagent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/settings"
)

// SystemPromptMessage is the stable product-facing instruction layer. Resource
// files, skills, and working notes remain separate messages so they can be
// refreshed without changing this baseline prompt.
func SystemPromptMessage(workspace string, available []agent.Tool) agent.Message {
	workspace, _ = filepath.Abs(workspace)
	var builder strings.Builder
	builder.WriteString("You are Yen, a blended personal assistant and coding agent operating inside Yen. Adapt to the user's current task while helping with conversation, research, file reading, command execution, editing, and writing.\n\nAvailable tools:\n")
	seen := make(map[string]bool, len(available))
	for _, tool := range available {
		if tool == nil || seen[tool.Name()] {
			continue
		}
		seen[tool.Name()] = true
		fmt.Fprintf(&builder, "- %s\n", tool.Name())
	}
	builder.WriteString("\nGuidelines:\n- Be concise in your responses\n- Show file paths clearly when working with files\n\nCurrent working directory: ")
	builder.WriteString(filepath.ToSlash(workspace))
	return agent.Message{Role: "system", Content: builder.String()}
}

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

func ArtifactCatalogMessage(catalog string) agent.Message {
	return agent.Message{Role: "system", Content: "<document_artifacts>\n" + strings.TrimSpace(catalog) + "\nUse convert_doc on a document path when you need its contents.\n</document_artifacts>"}
}

const contextPromptPrefix = "<project_context>\nThe following project guidance was loaded from context files; follow it unless current evidence requires otherwise.\n"

func stripUTF8BOM(content string) string { return strings.TrimPrefix(content, "\ufeff") }

var contextFileNames = []string{"AGENTS.override.md", "AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD", "CONTEXT.md"}

func appendContextFiles(sections []string, dir string) []string {
	for _, name := range contextFileNames {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		content := stripUTF8BOM(string(data))
		if err == nil {
			if strings.TrimSpace(content) != "" {
				sections = append(sections, "["+path+"]\n"+strings.TrimSpace(content))
			}
			break
		}
	}
	path := filepath.Join(dir, "YEN.md")
	if data, err := os.ReadFile(path); err == nil {
		content := stripUTF8BOM(string(data))
		if strings.TrimSpace(content) != "" {
			sections = append(sections, "["+path+"]\n"+strings.TrimSpace(content))
		}
	}
	return sections
}

func shadowedWorktreeContextFile(cwd string) string {
	common, err := gitPath(cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return ""
	}
	root, err := gitPath(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	common = resolveGitPath(cwd, common)
	root = resolveGitPath(cwd, root)
	if common == "" || root == "" {
		return ""
	}
	common, err = filepath.Abs(common)
	if err != nil {
		return ""
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return ""
	}
	common = canonicalPath(common)
	root = canonicalPath(root)
	mainRoot := filepath.Dir(common)
	if filepath.Clean(root) == filepath.Clean(mainRoot) || !isWithin(root, mainRoot) || filepath.Clean(filepath.Join(mainRoot, ".git")) != filepath.Clean(common) {
		return ""
	}
	for _, name := range contextFileNames {
		path := filepath.Join(root, name)
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
			return filepath.Join(mainRoot, name)
		}
	}
	return ""
}

func resolveGitPath(cwd, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	resolved, err := filepath.Abs(filepath.Join(cwd, path))
	if err != nil {
		return ""
	}
	return resolved
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func gitPath(dir string, args ...string) (string, error) {
	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(output)), err
}

func isWithin(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

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
	if agentDir := contextAgentDir(); agentDir != "" {
		sections = appendContextFiles(sections, agentDir)
	}
	shadowed := shadowedWorktreeContextFile(workspace)
	for i := len(dirs) - 1; i >= 0; i-- {
		sections = appendContextFiles(sections, dirs[i])
	}
	if shadowed != "" {
		prefix := "[" + shadowed + "]\n"
		filtered := sections[:0]
		for _, section := range sections {
			if !strings.HasPrefix(section, prefix) {
				filtered = append(filtered, section)
			}
		}
		sections = filtered
	}
	for _, configured := range resourceSettings.ContextFiles {
		path := configured
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		data, readErr := os.ReadFile(path)
		content := stripUTF8BOM(string(data))
		if readErr == nil && strings.TrimSpace(content) != "" {
			sections = append(sections, "["+path+"]\n"+strings.TrimSpace(content))
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

func contextAgentDir() string {
	if dir := strings.TrimSpace(os.Getenv("YEN_AGENT_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".theoses", "agent")
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
			if !isDiscoverableSkillFile(path, entry) {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			name, description := parseSkillFile(path, string(data))
			if name != "" && description != "" && !skillDisablesModelInvocation(string(data)) {
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

func isDiscoverableSkillFile(path string, entry os.DirEntry) bool {
	if entry == nil || entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
		return false
	}
	if entry.Name() == "SKILL.md" {
		return true
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(path), "SKILL.md"))
	return err != nil || !info.Mode().IsRegular()
}

func parseSkillFile(path, content string) (name, description string) {
	name, description = parseSkillFrontmatter(content)
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	return name, description
}

func skillDisablesModelInvocation(content string) bool {
	content = stripUTF8BOM(content)
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return false
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "disable-model-invocation" {
			return strings.Trim(strings.TrimSpace(value), "\"'") == "true"
		}
	}
	return false
}

// SkillCommands exposes the same local skill discovery to headless command
// clients without loading skill bodies into the prompt.
func SkillCommands(workspace string) []map[string]any {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil
	}
	resourceSettings, _ := settings.Load(workspace)
	if resourceSettings.Trusted != nil && !*resourceSettings.Trusted && os.Getenv("YEN_TRUST_PROJECT") != "1" {
		return nil
	}
	paths := []string{filepath.Join(workspace, ".theoses", "skills"), filepath.Join(workspace, ".agents", "skills")}
	for _, configured := range resourceSettings.SkillDirs {
		if !filepath.IsAbs(configured) {
			configured = filepath.Join(workspace, configured)
		}
		paths = append(paths, configured)
	}
	if dir := os.Getenv("YEN_SKILLS_DIR"); dir != "" {
		paths = append(paths, dir)
	}
	commands := make([]map[string]any, 0)
	for _, root := range paths {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry == nil {
				return nil
			}
			if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !isDiscoverableSkillFile(path, entry) {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			name, description := parseSkillFile(path, string(data))
			if name != "" && description != "" {
				commands = append(commands, map[string]any{
					"name": "skill:" + name, "description": description, "source": "skill",
					"sourceInfo": map[string]string{"path": path},
				})
			}
			return nil
		})
	}
	return commands
}

func parseSkillFrontmatter(content string) (string, string) {
	content = stripUTF8BOM(content)
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
