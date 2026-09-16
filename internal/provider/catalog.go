package provider

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
)

// The catalog snapshot is generated from Theoses2 commit
// f283c608a157301c5d80c57595d118a0c33f9179, packages/ai/src/providers/data.
// It is the offline fallback; provider APIs remain authoritative when reachable.
//
//go:embed catalog/*.json
var catalogFiles embed.FS

func staticCatalog(provider string) ([]ModelInfo, error) {
	data, err := fs.ReadFile(catalogFiles, "catalog/"+provider+".json")
	if err != nil {
		return nil, fmt.Errorf("static catalog for %s: %w", provider, err)
	}
	var groups map[string]map[string]ModelInfo
	if err := json.Unmarshal(data, &groups); err != nil {
		return nil, fmt.Errorf("decode static catalog for %s: %w", provider, err)
	}
	models := make([]ModelInfo, 0)
	for api, entries := range groups {
		for id, model := range entries {
			model.Provider = provider
			model.ID = id
			model.API = api
			models = append(models, model)
		}
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].ID == models[j].ID {
			return models[i].API < models[j].API
		}
		return models[i].ID < models[j].ID
	})
	return models, nil
}
