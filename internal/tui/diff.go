package tui

import (
	"regexp"
	"strings"
)

// RenderDiff renders a diff string (lines prefixed with "+", "-", or a
// space for added/removed/context) with intra-line word-level highlighting
// when a single removed line is immediately followed by a single added
// line, matching
// packages/coding-agent/src/modes/interactive/components/diff.ts:79-147.
//
// The oracle also applies a per-line-kind foreground color theme
// (toolDiffAdded/toolDiffRemoved/toolDiffContext, loaded from JSON theme
// files under packages/coding-agent/src/modes/interactive/theme/). This Go
// TUI has no color/theme subsystem yet, so that part of the oracle's
// behavior is not implemented here; only the structural line grouping and
// intra-line inverse-video highlighting are. Inverse video uses the
// standard SGR codes \x1b[7m/\x1b[27m in place of chalk.inverse.
func RenderDiff(diffText string) string {
	lines := strings.Split(diffText, "\n")
	var result []string

	i := 0
	for i < len(lines) {
		prefix, lineNum, content, ok := parseDiffLine(lines[i])
		if !ok {
			result = append(result, lines[i])
			i++
			continue
		}

		if prefix == "-" {
			var removed, added []diffLine
			for i < len(lines) {
				p, n, c, ok := parseDiffLine(lines[i])
				if !ok || p != "-" {
					break
				}
				removed = append(removed, diffLine{n, c})
				i++
			}
			for i < len(lines) {
				p, n, c, ok := parseDiffLine(lines[i])
				if !ok || p != "+" {
					break
				}
				added = append(added, diffLine{n, c})
				i++
			}

			if len(removed) == 1 && len(added) == 1 {
				removedLine, addedLine := renderIntraLineDiff(replaceTabs(removed[0].content), replaceTabs(added[0].content))
				result = append(result, "-"+removed[0].lineNum+" "+removedLine)
				result = append(result, "+"+added[0].lineNum+" "+addedLine)
			} else {
				for _, r := range removed {
					result = append(result, "-"+r.lineNum+" "+replaceTabs(r.content))
				}
				for _, a := range added {
					result = append(result, "+"+a.lineNum+" "+replaceTabs(a.content))
				}
			}
			continue
		}

		if prefix == "+" {
			result = append(result, "+"+lineNum+" "+replaceTabs(content))
			i++
			continue
		}

		result = append(result, " "+lineNum+" "+replaceTabs(content))
		i++
	}

	return strings.Join(result, "\n")
}

type diffLine struct {
	lineNum string
	content string
}

var diffLinePattern = regexp.MustCompile(`^([+\-\s])(\s*[0-9]*)\s(.*)$`)

func parseDiffLine(line string) (prefix, lineNum, content string, ok bool) {
	match := diffLinePattern.FindStringSubmatch(line)
	if match == nil {
		return "", "", "", false
	}
	return match[1], match[2], match[3], true
}

func replaceTabs(text string) string {
	return strings.ReplaceAll(text, "\t", "   ")
}

// renderIntraLineDiff computes a word-level diff and wraps changed spans in
// inverse video, matching diff.ts:26-66. Leading whitespace on the first
// changed span is left unhighlighted to avoid highlighting indentation.
func renderIntraLineDiff(oldContent, newContent string) (removedLine, addedLine string) {
	parts := diffWords(oldContent, newContent)
	firstRemoved, firstAdded := true, true
	for _, part := range parts {
		switch part.kind {
		case diffRemoved:
			value := part.value
			if firstRemoved {
				leading := leadingWhitespace(value)
				value = value[len(leading):]
				removedLine += leading
				firstRemoved = false
			}
			if value != "" {
				removedLine += "\x1b[7m" + value + "\x1b[27m"
			}
		case diffAdded:
			value := part.value
			if firstAdded {
				leading := leadingWhitespace(value)
				value = value[len(leading):]
				addedLine += leading
				firstAdded = false
			}
			if value != "" {
				addedLine += "\x1b[7m" + value + "\x1b[27m"
			}
		default:
			removedLine += part.value
			addedLine += part.value
		}
	}
	return removedLine, addedLine
}

func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

type diffPartKind int

const (
	diffCommon diffPartKind = iota
	diffRemoved
	diffAdded
)

type diffPart struct {
	kind  diffPartKind
	value string
}

var wordTokenPattern = regexp.MustCompile(`\S+\s*|\s+`)

// diffWords computes a word-level LCS diff between oldText and newText,
// tokenizing each non-whitespace run together with its trailing whitespace
// (matching jsdiff's diffWords tokenization), then merges adjacent parts of
// the same kind.
func diffWords(oldText, newText string) []diffPart {
	oldTokens := wordTokenPattern.FindAllString(oldText, -1)
	newTokens := wordTokenPattern.FindAllString(newText, -1)

	n, m := len(oldTokens), len(newTokens)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if oldTokens[i-1] == newTokens[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] >= lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}

	var reversed []diffPart
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && oldTokens[i-1] == newTokens[j-1]:
			reversed = append(reversed, diffPart{diffCommon, oldTokens[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]):
			reversed = append(reversed, diffPart{diffAdded, newTokens[j-1]})
			j--
		default:
			reversed = append(reversed, diffPart{diffRemoved, oldTokens[i-1]})
			i--
		}
	}

	parts := make([]diffPart, len(reversed))
	for k, part := range reversed {
		parts[len(reversed)-1-k] = part
	}
	return mergeDiffParts(parts)
}

func mergeDiffParts(parts []diffPart) []diffPart {
	var merged []diffPart
	for _, part := range parts {
		if len(merged) > 0 && merged[len(merged)-1].kind == part.kind {
			merged[len(merged)-1].value += part.value
			continue
		}
		merged = append(merged, part)
	}
	return merged
}
