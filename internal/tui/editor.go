package tui

import (
	"bufio"
	"io"
)

// LineEditor applies the small set of editing keys used by the interactive
// editor while keeping cursor state in runes rather than bytes.
type LineEditor struct {
	value  []rune
	cursor int
}

// LineHistory retains submitted non-empty prompts for interactive recall.
type LineHistory struct {
	entries  []string
	position int
}

func NewLineHistory() *LineHistory { return &LineHistory{position: -1} }

func (h *LineHistory) add(line string) {
	if line == "" {
		h.position = len(h.entries)
		return
	}
	if len(h.entries) == 0 || h.entries[len(h.entries)-1] != line {
		h.entries = append(h.entries, line)
	}
	h.position = len(h.entries)
}

func (h *LineHistory) previous() string {
	if h.position > 0 {
		h.position--
	}
	if h.position >= 0 && h.position < len(h.entries) {
		return h.entries[h.position]
	}
	return ""
}

func (h *LineHistory) next() string {
	if h.position < len(h.entries) {
		h.position++
	}
	if h.position < len(h.entries) {
		return h.entries[h.position]
	}
	return ""
}

func (e *LineEditor) Text() string { return string(e.value) }

func (e *LineEditor) insert(value rune) {
	e.value = append(e.value, 0)
	copy(e.value[e.cursor+1:], e.value[e.cursor:])
	e.value[e.cursor] = value
	e.cursor++
}

func (e *LineEditor) backspace() {
	if e.cursor == 0 {
		return
	}
	e.value = append(e.value[:e.cursor-1], e.value[e.cursor:]...)
	e.cursor--
}

func (e *LineEditor) delete() {
	if e.cursor == len(e.value) {
		return
	}
	e.value = append(e.value[:e.cursor], e.value[e.cursor+1:]...)
}

func (e *LineEditor) left() {
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *LineEditor) right() {
	if e.cursor < len(e.value) {
		e.cursor++
	}
}

func (e *LineEditor) home() { e.cursor = 0 }

func (e *LineEditor) end() { e.cursor = len(e.value) }

func (e *LineEditor) setText(value string) {
	e.value = []rune(value)
	e.cursor = len(e.value)
}

// ReadLine reads one interactive line and applies ANSI cursor-editing keys.
// It deliberately does not echo: the caller owns screen redraws and ordinary
// terminals continue to provide canonical input echo.
func ReadLine(r *bufio.Reader) (string, error) {
	return readLine(r, nil)
}

// ReadLineWithHistory reads a line and supports Up/Down prompt history.
func ReadLineWithHistory(r *bufio.Reader, history *LineHistory) (string, error) {
	return readLine(r, history)
}

func readLine(r *bufio.Reader, history *LineHistory) (string, error) {
	var editor LineEditor
	for {
		value, _, err := r.ReadRune()
		if err != nil {
			if err == io.EOF && len(editor.value) > 0 {
				return editor.Text(), nil
			}
			return "", err
		}
		switch value {
		case '\n', '\r':
			if value == '\r' && r.Buffered() > 0 {
				if next, peekErr := r.Peek(1); peekErr == nil && next[0] == '\n' {
					_, _ = r.ReadByte()
				}
			}
			line := editor.Text()
			if history != nil {
				history.add(line)
			}
			return line, nil
		case 0x04: // Ctrl-D
			if len(editor.value) == 0 {
				return "", io.EOF
			}
			editor.delete()
		case 0x08, 0x7f:
			editor.backspace()
		case 0x01: // Ctrl-A
			editor.home()
		case 0x05: // Ctrl-E
			editor.end()
		case 0x02: // Ctrl-B
			editor.left()
		case 0x06: // Ctrl-F
			editor.right()
		case 0x1b:
			switch editor.applyEscape(r) {
			case 'A':
				if history != nil {
					editor.setText(history.previous())
				}
			case 'B':
				if history != nil {
					editor.setText(history.next())
				}
			}
		default:
			if value >= 0x20 {
				editor.insert(value)
			}
		}
	}
}

func (e *LineEditor) applyEscape(r *bufio.Reader) byte {
	first, err := r.ReadByte()
	if err != nil {
		return 0
	}
	if first == '[' || first == 'O' {
		last, err := r.ReadByte()
		if err != nil {
			return 0
		}
		switch last {
		case 'A', 'B':
			return last
		case 'D':
			e.left()
		case 'C':
			e.right()
		case 'H':
			e.home()
		case 'F':
			e.end()
		case '3':
			if r.Buffered() > 0 {
				if next, peekErr := r.Peek(1); peekErr == nil && next[0] == '~' {
					_, _ = r.ReadByte()
					e.delete()
				}
			}
		}
	}
	return 0
}
