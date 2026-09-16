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

// ReadLine reads one interactive line and applies ANSI cursor-editing keys.
// It deliberately does not echo: the caller owns screen redraws and ordinary
// terminals continue to provide canonical input echo.
func ReadLine(r *bufio.Reader) (string, error) {
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
			return editor.Text(), nil
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
			editor.applyEscape(r)
		default:
			if value >= 0x20 {
				editor.insert(value)
			}
		}
	}
}

func (e *LineEditor) applyEscape(r *bufio.Reader) {
	first, err := r.ReadByte()
	if err != nil {
		return
	}
	if first == '[' || first == 'O' {
		last, err := r.ReadByte()
		if err != nil {
			return
		}
		switch last {
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
}
