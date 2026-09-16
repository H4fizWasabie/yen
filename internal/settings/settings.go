package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Settings struct {
	Provider              string              `json:"provider,omitempty"`
	Model                 string              `json:"model,omitempty"`
	DefaultProvider       string              `json:"defaultProvider,omitempty"`
	DefaultModel          string              `json:"defaultModel,omitempty"`
	SummarizationProvider string              `json:"summarizationProvider,omitempty"`
	SummarizationModel    string              `json:"summarizationModel,omitempty"`
	BaseURL               string              `json:"baseUrl,omitempty"`
	ShellPath             string              `json:"shellPath,omitempty"`
	ShellCommandPrefix    string              `json:"shellCommandPrefix,omitempty"`
	Reasoning             string              `json:"reasoningEffort,omitempty"`
	DefaultThinkingLevel  string              `json:"defaultThinkingLevel,omitempty"`
	ContextFiles          []string            `json:"contextFiles,omitempty"`
	SkillDirs             []string            `json:"skillDirs,omitempty"`
	PromptDirs            []string            `json:"promptDirs,omitempty"`
	Extensions            []string            `json:"extensions,omitempty"`
	Trusted               *bool               `json:"trusted,omitempty"`
	AutoCompaction        *bool               `json:"autoCompaction,omitempty"`
	Compaction            *CompactionSettings `json:"compaction,omitempty"`
	Retry                 *RetrySettings      `json:"retry,omitempty"`
	SteeringMode          string              `json:"steeringMode,omitempty"`
	FollowUpMode          string              `json:"followUpMode,omitempty"`
}

type CompactionSettings struct {
	Enabled          *bool `json:"enabled,omitempty"`
	ReserveTokens    int   `json:"reserveTokens,omitempty"`
	KeepRecentTokens int   `json:"keepRecentTokens,omitempty"`
	MaxHistoryTurns  int   `json:"maxHistoryTurns,omitempty"`
	present          map[string]bool
}

type RetrySettings struct {
	Enabled     *bool                  `json:"enabled,omitempty"`
	MaxRetries  int                    `json:"maxRetries,omitempty"`
	BaseDelayMs int                    `json:"baseDelayMs,omitempty"`
	Provider    *ProviderRetrySettings `json:"provider,omitempty"`
}

type ProviderRetrySettings struct {
	TimeoutMs, MaxRetries, MaxRetryDelayMs int
	present                                map[string]bool
}

func (s *ProviderRetrySettings) Has(name string) bool { return s != nil && s.present[name] }

func (s *CompactionSettings) UnmarshalJSON(data []byte) error {
	type plain CompactionSettings
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = CompactionSettings(v)
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(data, &keys)
	s.present = map[string]bool{}
	for k := range keys {
		s.present[k] = true
	}
	return nil
}
func (s *ProviderRetrySettings) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if b := raw["timeoutMs"]; b != nil {
		_ = json.Unmarshal(b, &s.TimeoutMs)
	}
	if b := raw["maxRetries"]; b != nil {
		_ = json.Unmarshal(b, &s.MaxRetries)
	}
	if b := raw["maxRetryDelayMs"]; b != nil {
		_ = json.Unmarshal(b, &s.MaxRetryDelayMs)
	}
	s.present = map[string]bool{}
	for k := range raw {
		s.present[k] = true
	}
	return nil
}

func Load(workspace string) (Settings, error) {
	var result Settings
	paths := []string{}
	if path := os.Getenv("YEN_SETTINGS_FILE"); path != "" {
		paths = append(paths, path)
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(configDir, "yen", "settings.json"))
	}
	paths = append(paths, filepath.Join(workspace, ".theoses", "settings.json"))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return result, err
		}
		var current Settings
		if err := json.Unmarshal(data, &current); err != nil {
			return result, err
		}
		if current.SteeringMode == "" {
			var legacy struct {
				QueueMode string `json:"queueMode"`
			}
			if json.Unmarshal(data, &legacy) == nil && legacy.QueueMode != "" {
				current.SteeringMode = legacy.QueueMode
			}
		}
		merge(&result, current)
	}
	return result, nil
}

func Save(workspace string, current Settings) error {
	path := os.Getenv("YEN_SETTINGS_FILE")
	if path == "" {
		path = filepath.Join(workspace, ".theoses", "settings.json")
	}
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func TrustPath(workspace string) string {
	if path := os.Getenv("YEN_TRUST_FILE"); path != "" {
		return path
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configDir, "yen", "trusted-projects.json")
	}
	return filepath.Join(workspace, ".theoses", "trusted-projects.json")
}

func IsTrusted(workspace string) bool {
	if os.Getenv("YEN_TRUST_PROJECT") == "1" {
		return true
	}
	settings, err := Load(workspace)
	if err == nil && settings.Trusted != nil {
		return *settings.Trusted
	}
	data, err := os.ReadFile(TrustPath(workspace))
	if err != nil {
		return false
	}
	var projects map[string]bool
	return json.Unmarshal(data, &projects) == nil && projects[workspace]
}

func Trust(workspace string) error {
	path := TrustPath(workspace)
	data, err := os.ReadFile(path)
	projects := make(map[string]bool)
	if err == nil {
		_ = json.Unmarshal(data, &projects)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	projects[workspace] = true
	encoded, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o600)
}

func merge(target *Settings, source Settings) {
	if source.Provider != "" {
		target.Provider = source.Provider
	}
	if source.Model != "" {
		target.Model = source.Model
	}
	if source.DefaultProvider != "" {
		target.DefaultProvider = source.DefaultProvider
	}
	if source.DefaultModel != "" {
		target.DefaultModel = source.DefaultModel
	}
	if source.SummarizationProvider != "" {
		target.SummarizationProvider = source.SummarizationProvider
	}
	if source.SummarizationModel != "" {
		target.SummarizationModel = source.SummarizationModel
	}
	if source.BaseURL != "" {
		target.BaseURL = source.BaseURL
	}
	if source.ShellPath != "" {
		target.ShellPath = source.ShellPath
	}
	if source.ShellCommandPrefix != "" {
		target.ShellCommandPrefix = source.ShellCommandPrefix
	}
	if source.Reasoning != "" {
		target.Reasoning = source.Reasoning
	}
	if source.DefaultThinkingLevel != "" {
		target.DefaultThinkingLevel = source.DefaultThinkingLevel
	}
	if source.ContextFiles != nil {
		target.ContextFiles = source.ContextFiles
	}
	if source.SkillDirs != nil {
		target.SkillDirs = source.SkillDirs
	}
	if source.PromptDirs != nil {
		target.PromptDirs = source.PromptDirs
	}
	if source.Extensions != nil {
		target.Extensions = source.Extensions
	}
	if source.Trusted != nil {
		target.Trusted = source.Trusted
	}
	if source.AutoCompaction != nil {
		target.AutoCompaction = source.AutoCompaction
	}
	if source.Compaction != nil {
		if target.Compaction == nil {
			target.Compaction = &CompactionSettings{}
		}
		if target.Compaction.present == nil {
			target.Compaction.present = map[string]bool{}
		}
		for key := range source.Compaction.present {
			target.Compaction.present[key] = true
		}
		if source.Compaction.Enabled != nil {
			target.Compaction.Enabled = source.Compaction.Enabled
		}
		if source.Compaction.ReserveTokens != 0 || source.Compaction.present["reserveTokens"] {
			target.Compaction.ReserveTokens = source.Compaction.ReserveTokens
		}
		if source.Compaction.KeepRecentTokens != 0 || source.Compaction.present["keepRecentTokens"] {
			target.Compaction.KeepRecentTokens = source.Compaction.KeepRecentTokens
		}
		if source.Compaction.MaxHistoryTurns != 0 || source.Compaction.present["maxHistoryTurns"] {
			target.Compaction.MaxHistoryTurns = source.Compaction.MaxHistoryTurns
		}
	}
	if source.Retry != nil {
		if target.Retry == nil {
			target.Retry = &RetrySettings{}
		}
		if source.Retry.Enabled != nil {
			target.Retry.Enabled = source.Retry.Enabled
		}
		if source.Retry.MaxRetries != 0 {
			target.Retry.MaxRetries = source.Retry.MaxRetries
		}
		if source.Retry.BaseDelayMs != 0 {
			target.Retry.BaseDelayMs = source.Retry.BaseDelayMs
		}
		if source.Retry.Provider != nil {
			if target.Retry.Provider == nil {
				target.Retry.Provider = &ProviderRetrySettings{}
			}
			p, q := source.Retry.Provider, target.Retry.Provider
			if q.present == nil {
				q.present = map[string]bool{}
			}
			for key := range p.present {
				q.present[key] = true
			}
			if p.present["timeoutMs"] {
				q.TimeoutMs = p.TimeoutMs
			}
			if p.present["maxRetries"] {
				q.MaxRetries = p.MaxRetries
			}
			if p.present["maxRetryDelayMs"] {
				q.MaxRetryDelayMs = p.MaxRetryDelayMs
			}
		}
	}
	if source.SteeringMode == "all" || source.SteeringMode == "one-at-a-time" {
		target.SteeringMode = source.SteeringMode
	}
	if source.FollowUpMode == "all" || source.FollowUpMode == "one-at-a-time" {
		target.FollowUpMode = source.FollowUpMode
	}
}

func QueueModes(current Settings) (string, string) {
	steering, followUp := current.SteeringMode, current.FollowUpMode
	if steering != "all" && steering != "one-at-a-time" {
		steering = "one-at-a-time"
	}
	if followUp != "all" && followUp != "one-at-a-time" {
		followUp = "one-at-a-time"
	}
	return steering, followUp
}
