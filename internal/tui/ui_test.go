package tui

import (
	"bufio"
	"strings"
	"testing"
)

func TestHandleExtensionUISelectReturnsProtocolResponse(t *testing.T) {
	response, err := HandleExtensionUI(nil, map[string]any{
		"id": "ui-1", "method": "select", "title": "Pick", "options": []any{"first", "second"},
	}, bufio.NewReader(strings.NewReader("2\n")), &strings.Builder{})
	if err != nil || response["id"] != "ui-1" || response["value"] != "second" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}
