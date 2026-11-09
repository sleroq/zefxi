package richtext

import (
	"html"
	"sort"
	"strings"
	"unicode/utf8"
)

type renderer struct {
	text     string
	telegram bool
	resolve  func(Entity, string) string
}

func ToDiscord(t Text, resolve func(Entity, string) string) string  { return render(t, resolve, false) }
func ToTelegram(t Text, resolve func(Entity, string) string) string { return render(t, resolve, true) }

func render(t Text, resolve func(Entity, string) string, telegram bool) string {
	entities := make([]Entity, 0, len(t.Entities))
	for _, e := range t.Entities {
		if validEntity(t.Value, e) {
			entities = append(entities, e)
		}
	}
	sort.SliceStable(entities, func(i, j int) bool {
		if entities[i].Start == entities[j].Start {
			if entities[i].End == entities[j].End {
				return entityOrder(entities[i].Kind) < entityOrder(entities[j].Kind)
			}
			return entities[i].End > entities[j].End
		}
		return entities[i].Start < entities[j].Start
	})
	r := renderer{text: t.Value, telegram: telegram, resolve: resolve}
	return r.section(0, len(t.Value), entities)
}

// Equal spans have no inherent nesting order; use a stable destination-safe order.
func entityOrder(kind string) int {
	return map[string]int{
		"blockquote": 0, "link": 1, "bold": 2, "italic": 3,
		"underline": 4, "strike": 5, "spoiler": 6,
		"code": 7, "pre": 8, "mention": 9,
	}[kind]
}

func validEntity(s string, e Entity) bool {
	return supported(e.Kind) && e.Start >= 0 && e.End > e.Start && e.End <= len(s) &&
		utf8.ValidString(s[:e.Start]) && utf8.ValidString(s[:e.End])
}

func (r renderer) escape(s string) string {
	if r.telegram {
		return html.EscapeString(s)
	}
	return escapeDiscord(s)
}

func (r renderer) section(start, end int, entities []Entity) string {
	var b strings.Builder
	pos := start
	for i := 0; i < len(entities); i++ {
		e := entities[i]
		if e.Start < pos || e.End > end {
			continue
		}
		b.WriteString(r.escape(r.text[pos:e.Start]))
		j := i + 1
		for j < len(entities) && entities[j].Start < e.End {
			j++
		}
		b.WriteString(r.entity(e, entities[i+1:j]))
		pos = e.End
		i = j - 1
	}
	b.WriteString(r.escape(r.text[pos:end]))
	return b.String()
}

func (r renderer) entity(e Entity, children []Entity) string {
	source := r.text[e.Start:e.End]
	switch e.Kind {
	case "mention":
		if r.resolve != nil {
			if result := r.resolve(e, source); result != "" {
				return result
			}
		}
		return r.escape(source)
	case "code", "pre":
		return r.code(e.Kind, source)
	}
	body := r.section(e.Start, e.End, children)
	if r.telegram {
		return telegramMark(e, body)
	}
	return discordMark(e, body)
}

func (r renderer) code(kind, source string) string {
	if r.telegram {
		return "<" + kind + ">" + html.EscapeString(source) + "</" + kind + ">"
	}
	if kind == "pre" {
		// Discord fences cannot safely contain a closing triple-backtick fence.
		if strings.Contains(source, "```") {
			return r.escape(source)
		}
		return "```\n" + source + "```"
	}
	if !strings.Contains(source, "`") {
		return "`" + source + "`"
	}
	if !strings.Contains(source, "``") && !strings.HasPrefix(source, "`") && !strings.HasSuffix(source, "`") {
		return "``" + source + "``"
	}
	return r.escape(source)
}

func telegramMark(e Entity, body string) string {
	tags := map[string]string{
		"bold": "b", "italic": "i", "underline": "u", "strike": "s",
		"spoiler": "tg-spoiler", "blockquote": "blockquote", "link": "a",
	}
	tag := tags[e.Kind]
	attr := ""
	if e.Kind == "link" {
		if !safeURL(e.URL) {
			return body
		}
		attr = " href=\"" + html.EscapeString(e.URL) + "\""
	}
	return "<" + tag + attr + ">" + body + "</" + tag + ">"
}

func discordMark(e Entity, body string) string {
	switch e.Kind {
	case "link":
		if !safeURL(e.URL) {
			return body
		}
		url := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(e.URL)
		return "[" + body + "](" + url + ")"
	case "blockquote":
		return "> " + strings.ReplaceAll(body, "\n", "\n> ")
	}
	token := map[string]string{"bold": "**", "italic": "*", "underline": "__", "strike": "~~", "spoiler": "||"}[e.Kind]
	return token + body + token
}

func safeURL(s string) bool {
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"),
		strings.HasPrefix(lower, "mailto:"), strings.HasPrefix(lower, "tg://"):
		return !strings.ContainsAny(s, "\r\n\t <>")
	}
	return false
}

func escapeDiscord(s string) string {
	return strings.NewReplacer(
		"\\", "\\\\", "*", "\\*", "_", "\\_",
		"~", "\\~", "|", "\\|", "`", "\\`",
		"[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)",
		"<@", "\\<@\u200b", "<", "\\<", ">", "\\>", "#", "\\#",
		"@everyone", "@\u200beveryone", "@here", "@\u200bhere",
	).Replace(s)
}
