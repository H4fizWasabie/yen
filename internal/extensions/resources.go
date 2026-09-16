package extensions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/H4fizWasabie/yen/internal/settings"
)

type ResourceKind string

const (
	ResourceExtensions ResourceKind = "extensions"
	ResourceSkills     ResourceKind = "skills"
	ResourcePrompts    ResourceKind = "prompts"
	ResourceThemes     ResourceKind = "themes"
	ResourceAgents     ResourceKind = "agents"
)

type Resource struct {
	Path    string
	Enabled bool
	Source  string
	Scope   string
}

type ResourceDiagnostic struct {
	Path    string
	Message string
}

type ResourceLoader struct {
	workspace   string
	agentDir    string
	mu          sync.RWMutex
	resources   map[ResourceKind][]Resource
	diagnostics []ResourceDiagnostic
}

func NewResourceLoader(workspace, agentDir string) *ResourceLoader {
	return &ResourceLoader{workspace: workspace, agentDir: agentDir}
}

func (l *ResourceLoader) Reload() error {
	if l == nil || strings.TrimSpace(l.workspace) == "" {
		return errors.New("resource loader workspace is required")
	}
	workspace, err := filepath.Abs(l.workspace)
	if err != nil {
		return err
	}
	settingsValue, err := settings.Load(workspace)
	if err != nil {
		return err
	}
	resources := map[ResourceKind][]Resource{}
	var diagnostics []ResourceDiagnostic
	trusted := settings.IsTrusted(workspace)
	add := func(kind ResourceKind, path, source, scope string, enabled bool) {
		path = filepath.Clean(path)
		if _, err := os.Stat(path); err != nil {
			if !os.IsNotExist(err) {
				diagnostics = append(diagnostics, ResourceDiagnostic{Path: path, Message: err.Error()})
			}
			return
		}
		resources[kind] = append(resources[kind], Resource{Path: path, Enabled: enabled, Source: source, Scope: scope})
	}
	addDir := func(kind ResourceKind, path, source, scope string) {
		if scope == "project" && !trusted {
			return
		}
		if _, err := os.Stat(path); err == nil {
			add(kind, path, source, scope, true)
		}
	}
	addConfigured := func(kind ResourceKind, path string, enabled bool) {
		if !trusted {
			return
		}
		if _, err := os.Stat(path); err != nil {
			diagnostics = append(diagnostics, ResourceDiagnostic{Path: path, Message: "configured resource not found"})
			return
		}
		add(kind, path, "configured", "project", enabled)
	}
	projectRoot := filepath.Join(workspace, ".theoses")
	addDir(ResourceExtensions, filepath.Join(projectRoot, "extensions"), "auto", "project")
	addDir(ResourceSkills, filepath.Join(projectRoot, "skills"), "auto", "project")
	addDir(ResourcePrompts, filepath.Join(projectRoot, "prompts"), "auto", "project")
	addDir(ResourceThemes, filepath.Join(projectRoot, "themes"), "auto", "project")
	addDir(ResourceAgents, filepath.Join(projectRoot, "agents"), "auto", "project")
	userRoot := filepath.Join(l.agentDir)
	addDir(ResourceExtensions, filepath.Join(userRoot, "extensions"), "auto", "user")
	addDir(ResourceSkills, filepath.Join(userRoot, "skills"), "auto", "user")
	addDir(ResourcePrompts, filepath.Join(userRoot, "prompts"), "auto", "user")
	addDir(ResourceThemes, filepath.Join(userRoot, "themes"), "auto", "user")
	addDir(ResourceAgents, filepath.Join(userRoot, "agents"), "auto", "user")
	for _, path := range settingsValue.SkillDirs {
		addConfigured(ResourceSkills, resolveResourcePath(path, workspace), true)
	}
	for _, path := range settingsValue.PromptDirs {
		addConfigured(ResourcePrompts, resolveResourcePath(path, workspace), true)
	}
	for _, path := range settingsValue.Extensions {
		enabled := !strings.HasPrefix(path, "!") && !strings.HasPrefix(path, "-")
		addConfigured(ResourceExtensions, resolveResourcePath(strings.TrimLeft(path, "!-"), workspace), enabled)
	}
	l.mu.Lock()
	l.resources, l.diagnostics = resources, diagnostics
	l.mu.Unlock()
	return nil
}

func resolveResourcePath(path, base string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

func (l *ResourceLoader) Resources(kind ResourceKind) []Resource {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]Resource(nil), l.resources[kind]...)
}
func (l *ResourceLoader) Diagnostics() []ResourceDiagnostic {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]ResourceDiagnostic(nil), l.diagnostics...)
}
