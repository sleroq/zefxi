package discord

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

var contentTokens = regexp.MustCompile(`<@!?[0-9]+>|(?s:.)`)

// Discord rejects webhook names containing these reserved words and limits them to 80 characters.
func webhookName(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "clyde") || strings.Contains(lower, "discord") {
		return ""
	}
	units := 0
	for i, r := range name {
		size := 1
		if r > 0xffff {
			size = 2
		}
		units += size
		if units > 80 {
			return name[:i]
		}
	}
	return name
}

func utf16Units(text string) int {
	units := 0
	for _, r := range text {
		units++
		if r > 0xffff {
			units++
		}
	}
	return units
}

func splitContent(content string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, errors.New("discord: sender name exceeds message limit")
	}
	tokens := contentTokens.FindAllString(content, -1)
	var chunks []string
	for start := 0; start < len(tokens); {
		end, boundary, units := start, start, 0
		for end < len(tokens) && units+utf16Units(tokens[end]) <= limit {
			units += utf16Units(tokens[end])
			end++
			if strings.IndexFunc(tokens[end-1], unicode.IsSpace) >= 0 {
				boundary = end
			}
		}
		if end == start {
			return nil, errors.New("discord: mention exceeds message limit")
		}
		if end < len(tokens) && boundary > start {
			end = boundary
		}
		chunks = append(chunks, strings.Join(tokens[start:end], ""))
		start = end
	}
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	return chunks, nil
}
