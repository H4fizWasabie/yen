package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

func parseStructuredJSON(raw, label string) (any, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		if newline := strings.IndexByte(cleaned, '\n'); newline >= 0 {
			cleaned = strings.TrimSpace(cleaned[newline+1:])
		}
		cleaned = strings.TrimSpace(strings.TrimSuffix(cleaned, "```"))
	}
	var value any
	if err := json.Unmarshal([]byte(cleaned), &value); err == nil {
		return value, nil
	} else {
		repaired := repairJSONStringLiterals(stripTrailingCommas(cleaned))
		if repaired != cleaned {
			if json.Unmarshal([]byte(repaired), &value) == nil {
				return value, nil
			}
		}
		log.Printf("%s returned invalid JSON: %s", label, diagnosticSnippet(cleaned, err))
		return nil, err
	}
}

func decodeStructuredJSON(raw, label string, target any) error {
	value, err := parseStructuredJSON(raw, label)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

func diagnosticSnippet(raw string, err error) string {
	position := -1
	message := err.Error()
	if marker := strings.Index(message, "position "); marker >= 0 {
		_, _ = fmt.Sscanf(message[marker:], "position %d", &position)
	}
	if position < 0 || position > len(raw) {
		if len(raw) > 240 {
			return raw[:240]
		}
		return raw
	}
	start := position - 120
	if start < 0 {
		start = 0
	}
	end := position + 120
	if end > len(raw) {
		end = len(raw)
	}
	return raw[start:end]
}
