package memory

import (
	"strings"
	"testing"
)

func TestParseStructuredJSONMatchesOracleRepairCases(t *testing.T) {
	value, err := parseStructuredJSON("```json\n{\"facts\":[{\"id\":\"f1\",\"subject\":\"x\",}],}\n```", "test")
	if err != nil || value == nil {
		t.Fatalf("value=%#v err=%v", value, err)
	}
	value, err = parseStructuredJSON("{\"episode\":{\"summary\":\"line1\nline2\"}}", "test")
	if err != nil || !strings.Contains(value.(map[string]any)["episode"].(map[string]any)["summary"].(string), "line1\nline2") {
		t.Fatalf("value=%#v err=%v", value, err)
	}
}

func TestParseStructuredJSONDoesNotExtractJSONFromSurroundingText(t *testing.T) {
	if _, err := parseStructuredJSON("answer: {\"facts\":[]}", "test"); err == nil {
		t.Fatal("surrounding prose should not be accepted")
	}
}
