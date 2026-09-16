package tui

import "testing"

func TestRenderDynamicBorderRepeatsRuleToWidth(t *testing.T) {
	got := RenderDynamicBorder(5)
	want := "─────"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestRenderDynamicBorderClampsToAtLeastOneRune(t *testing.T) {
	for _, width := range []int{0, -1, -100} {
		got := RenderDynamicBorder(width)
		if got != "─" {
			t.Fatalf("width=%d got=%q want=%q", width, got, "─")
		}
	}
}
