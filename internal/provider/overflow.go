package provider

import (
	"regexp"
	"strings"
)

var contextOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prompt is too long`),
	regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`),
	regexp.MustCompile(`(?i)request_too_large`),
	regexp.MustCompile(`(?i)exceeds the context window`),
	regexp.MustCompile(`(?i)exceeds (?:the )?(?:model's )?maximum context length`),
	regexp.MustCompile(`(?i)maximum context length is [0-9,]+ tokens`),
	regexp.MustCompile(`(?i)exceeds (?:the )?maximum allowed input length`),
	regexp.MustCompile(`(?i)input \([0-9,]+ tokens\) is longer than the model's context length`),
	regexp.MustCompile(`(?i)range of input length should be`),
	regexp.MustCompile(`(?i)too many tokens`),
	regexp.MustCompile(`(?i)token limit exceeded`),
}

func IsContextOverflowError(message string) bool {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") || strings.Contains(lower, "service unavailable") {
		return false
	}
	for _, pattern := range contextOverflowPatterns {
		if pattern.MatchString(message) {
			return true
		}
	}
	return false
}
