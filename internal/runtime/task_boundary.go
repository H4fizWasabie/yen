package runtime

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
)

const taskDescriptorType, taskBoundaryType = "task_descriptor", "task_boundary"

func maybeDetectTaskBoundary(ctx context.Context, current *session.Session, provider agent.Provider, prompt, entryID string) {
	if current == nil || provider == nil || strings.TrimSpace(prompt) == "" {
		return
	}
	descriptor := ""
	for _, entry := range current.CustomEntries() {
		if entry.Type == taskDescriptorType {
			if value, ok := entry.Data.(map[string]any); ok {
				if text, ok := value["summary"].(string); ok {
					descriptor = text
				}
			}
		}
	}
	request := `Decide whether the new message continues the current task. Return only JSON: {"related":true,"currentTaskSummary":"one sentence"}. Current task: ` + descriptor + ` New message: ` + prompt
	response, err := provider.Next(ctx, []agent.Message{{Role: "user", Content: request}}, nil)
	if err != nil || response.StopReason == "error" || strings.TrimSpace(response.Text) == "" {
		return
	}
	var verdict struct {
		Related            bool   `json:"related"`
		CurrentTaskSummary string `json:"currentTaskSummary"`
	}
	text := strings.TrimSpace(strings.Trim(response.Text, "`"))
	if json.Unmarshal([]byte(text), &verdict) != nil || strings.TrimSpace(verdict.CurrentTaskSummary) == "" {
		return
	}
	_, _ = current.AppendCustomEntry(taskDescriptorType, map[string]any{"summary": strings.TrimSpace(verdict.CurrentTaskSummary)})
	if !verdict.Related {
		_, _ = current.AppendCustomEntry(taskBoundaryType, map[string]any{"taskSummary": strings.TrimSpace(verdict.CurrentTaskSummary), "beforeEntryId": entryID})
	}
}
