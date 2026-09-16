package tui

import "strings"

// RenderDynamicBorder returns a full-width horizontal rule line that
// adjusts to the given viewport width, matching
// packages/coding-agent/src/modes/interactive/components/dynamic-border.ts:22-24
// (`this.color("─".repeat(Math.max(1, width)))`).
//
// The oracle wires DynamicBorder into a Component/Container tree
// (chatContainer.addChild(new DynamicBorder())) to separate chat message
// groups and boxed notifications. This Go TUI's Screen is a flat
// scrollback/footer model with no equivalent container tree, so this slice
// is the tested rendering primitive only; wiring it into a section
// separator (and the oracle's optional per-call color function, which
// requires the same not-yet-built color/theme subsystem noted for
// RenderDiff) remains a separate, larger gap.
func RenderDynamicBorder(width int) string {
	if width < 1 {
		width = 1
	}
	return strings.Repeat("─", width)
}
