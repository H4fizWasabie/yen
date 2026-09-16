package tui

import "testing"

func TestSpinnerAdvanceCyclesThroughDefaultFrames(t *testing.T) {
	s := NewSpinner(nil)
	if got := s.Frame(); got != DefaultSpinnerFrames[0] {
		t.Fatalf("frame=%q want=%q", got, DefaultSpinnerFrames[0])
	}
	for i := 1; i < len(DefaultSpinnerFrames); i++ {
		s.Advance()
		if got := s.Frame(); got != DefaultSpinnerFrames[i] {
			t.Fatalf("frame[%d]=%q want=%q", i, got, DefaultSpinnerFrames[i])
		}
	}
	s.Advance()
	if got := s.Frame(); got != DefaultSpinnerFrames[0] {
		t.Fatalf("frame did not wrap around: %q", got)
	}
}

func TestSpinnerWithSingleFrameNeverAdvances(t *testing.T) {
	s := NewSpinner([]string{"*"})
	s.Advance()
	s.Advance()
	if got := s.Frame(); got != "*" {
		t.Fatalf("frame=%q want=%q", got, "*")
	}
}

func TestSpinnerWithNoFramesRendersEmpty(t *testing.T) {
	s := NewSpinner([]string{})
	if got := s.Frame(); got != "" {
		t.Fatalf("frame=%q want empty", got)
	}
}

func TestFormatStatusLinePrefixesNonEmptyFrame(t *testing.T) {
	got := FormatStatusLine("⠋", "Compacting context...")
	want := "⠋ Compacting context..."
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestFormatStatusLineOmitsFrameWhenEmpty(t *testing.T) {
	got := FormatStatusLine("", "Ready")
	if got != "Ready" {
		t.Fatalf("got=%q want=%q", got, "Ready")
	}
}
