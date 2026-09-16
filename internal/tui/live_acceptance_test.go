package tui

import (
	"bufio"
	"io"
	"os"
	"testing"
	"time"
)

// These tests exercise the raw-terminal code paths through a genuine
// pseudo-terminal rather than the strings.Reader/bufio.Reader pipes used
// elsewhere in this package's tests. EnableRawInput only engages raw mode
// when its argument is an *os.File whose termios ioctls succeed, which
// piped test readers never satisfy — so every other "raw" test in this
// package (SelectRaw, ReadLineWithOutputAndHistory, raw tree fold/unfold
// and paging, and so on) exercises those functions' logic directly but
// never the terminal-detection path that decides whether to use them at
// all. These tests close that gap.
func skipUnlessLiveTerminalAcceptance(t *testing.T) {
	t.Helper()
	if os.Getenv("YEN_LIVE_TERMINAL_ACCEPTANCE") == "" {
		t.Skip("set YEN_LIVE_TERMINAL_ACCEPTANCE=1 to run PTY-backed live terminal acceptance tests")
	}
}

func TestLiveTerminalEnablesRawInputOnRealPTY(t *testing.T) {
	skipUnlessLiveTerminalAcceptance(t)
	master, slave, err := openPTY()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()

	restore, enabled, err := EnableRawInput(slave)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	if !enabled {
		t.Fatal("EnableRawInput did not engage raw mode on a real PTY")
	}
}

func TestLiveTerminalSelectRawRespondsToRealKeystrokes(t *testing.T) {
	skipUnlessLiveTerminalAcceptance(t)
	master, slave, err := openPTY()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()

	restore, enabled, err := EnableRawInput(slave)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	if !enabled {
		t.Fatal("EnableRawInput did not engage raw mode on a real PTY")
	}

	type result struct {
		selected int
		err      error
	}
	done := make(chan result, 1)
	go func() {
		selected, err := SelectRaw(bufio.NewReader(slave), slave, "Pick", []string{"first", "second", "third"})
		done <- result{selected, err}
	}()
	// Drain the rendered prompt/selector output so the PTY's buffer never
	// fills and blocks SelectRaw's writes.
	go func() { _, _ = io.Copy(io.Discard, master) }()

	// A real terminal delivers keystrokes to the reading process one byte
	// at a time; writing to the PTY master reproduces that path through
	// the kernel's line discipline instead of calling SelectRaw's byte
	// handling directly.
	if _, err := master.Write([]byte("j\r")); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.selected != 1 {
			t.Fatalf("selected=%d want=1 (second option, after one real 'j' keystroke)", r.selected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SelectRaw to respond to real PTY keystrokes")
	}
}
