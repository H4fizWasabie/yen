package tui

import "time"

// DefaultSpinnerFrames and DefaultSpinnerInterval mirror the oracle's
// default braille animation, packages/tui/src/components/loader.ts:11-12.
var DefaultSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const DefaultSpinnerInterval = 80 * time.Millisecond

// Spinner is a frame-cycling state machine mirroring Loader's
// currentFrame/restartAnimation cycling
// (packages/tui/src/components/loader.ts:72-81): a spinner with one frame
// or fewer never advances, matching the oracle's early return, and
// otherwise wraps back to the first frame after the last.
//
// This is a standalone, tested state machine only. The oracle drives
// Loader's animation with a real interval timer that calls
// ui.requestRender() on every tick; this Go interactive CLI's loop only
// redraws in response to prompt reads and agent events, not on a
// wall-clock timer, so wiring a live-redrawing animated spinner into the
// interactive loop is a separate, larger change to that loop's structure
// and is not attempted here.
type Spinner struct {
	frames  []string
	current int
}

// NewSpinner creates a spinner with the given frames, or the default
// braille animation when frames is nil. Passing a non-nil empty slice
// hides the indicator (Frame returns ""), matching the oracle's
// LoaderIndicatorOptions.frames doc: "Use an empty array to hide the
// indicator."
func NewSpinner(frames []string) *Spinner {
	if frames == nil {
		frames = DefaultSpinnerFrames
	}
	return &Spinner{frames: frames}
}

// Frame returns the current animation frame, or "" if the spinner has no
// frames (matching the oracle's hidden-indicator case).
func (s *Spinner) Frame() string {
	if len(s.frames) == 0 {
		return ""
	}
	return s.frames[s.current]
}

// Advance moves to the next frame, wrapping around at the end. It is a
// no-op when there is one frame or fewer.
func (s *Spinner) Advance() {
	if len(s.frames) <= 1 {
		return
	}
	s.current = (s.current + 1) % len(s.frames)
}

// FormatStatusLine reproduces Loader.updateDisplay's indicator+message
// composition (packages/tui/src/components/loader.ts:83-91): a non-empty
// frame is followed by a space before the message; an empty frame (hidden
// indicator) shows just the message.
func FormatStatusLine(frame, message string) string {
	if frame == "" {
		return message
	}
	return frame + " " + message
}
