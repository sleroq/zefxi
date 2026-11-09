package richtext

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type discordParser struct {
	out      strings.Builder
	entities []Entity
}

// FromDiscord parses Discord Markdown without interpreting HTML or mass mentions.
func FromDiscord(content string) Text {
	p := discordParser{}
	p.parse(content)
	return Text{Value: p.out.String(), Entities: p.entities}
}

var discordEmoji = regexp.MustCompile(`^<a?:([A-Za-z0-9_]+):[0-9]+>`)

var discordMarks = []struct{ token, kind string }{
	{"***", "bold"}, {"___", "underline"}, {"**", "bold"}, {"__", "underline"},
	{"~~", "strike"}, {"||", "spoiler"}, {"*", "italic"}, {"_", "italic"},
}

func (p *discordParser) parse(s string) {
	for len(s) > 0 {
		consumed := p.special(s)
		if consumed > 0 {
			s = s[consumed:]
			continue
		}
		if p.out.Len() == 0 || strings.HasSuffix(p.out.String(), "\n") {
			if consumed = p.quote(s); consumed > 0 {
				s = s[consumed:]
				continue
			}
		}
		consumed = p.mark(s)
		if consumed > 0 {
			s = s[consumed:]
			continue
		}
		_, n := utf8.DecodeRuneInString(s)
		p.out.WriteString(s[:n])
		s = s[n:]
	}
}

func (p *discordParser) special(s string) int {
	if s[0] == '\\' && len(s) > 1 {
		_, n := utf8.DecodeRuneInString(s[1:])
		p.out.WriteString(s[1 : 1+n])
		return 1 + n
	}
	if s[0] == '`' {
		return p.code(s)
	}
	if strings.HasPrefix(s, "<@") {
		return p.mention(s)
	}
	if strings.HasPrefix(s, "<:") || strings.HasPrefix(s, "<a:") {
		if match := discordEmoji.FindStringSubmatch(s); match != nil {
			p.out.WriteByte(':')
			p.out.WriteString(match[1])
			p.out.WriteByte(':')
			return len(match[0])
		}
	}
	if s[0] == '[' {
		return p.link(s)
	}
	return 0
}

func (p *discordParser) code(s string) int {
	count := len(s) - len(strings.TrimLeft(s, "`"))
	token := s[:count]
	end := strings.Index(s[count:], token)
	if end < 0 {
		return 0
	}
	end += count
	body := s[count:end]
	kind := "code"
	if count >= 3 {
		kind = "pre"
		if nl := strings.IndexByte(body, '\n'); nl >= 0 {
			body = body[nl+1:]
		}
	}
	start := p.out.Len()
	p.out.WriteString(body)
	p.entities = append(p.entities, Entity{Kind: kind, Start: start, End: p.out.Len()})
	return end + count
}

func (p *discordParser) mention(s string) int {
	end := strings.IndexByte(s, '>')
	if end < 3 {
		return 0
	}
	id := strings.TrimPrefix(s[2:end], "!")
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return 0
	}
	start := p.out.Len()
	p.out.WriteString(s[:end+1])
	entity := Entity{Kind: "mention", Start: start, End: p.out.Len(), UserID: id}
	p.entities = append(p.entities, entity)
	return end + 1
}

func (p *discordParser) link(s string) int {
	mid := unescapedIndex(s, "](", 1)
	if mid < 0 {
		return 0
	}
	end := linkEnd(s, mid+2)
	if end < 0 {
		return 0
	}
	start := p.out.Len()
	p.parse(s[1:mid])
	entity := Entity{Kind: "link", Start: start, End: p.out.Len(), URL: unescape(s[mid+2 : end])}
	p.entities = append(p.entities, entity)
	return end + 1
}

func linkEnd(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

func (p *discordParser) quote(s string) int {
	if !strings.HasPrefix(s, "> ") {
		return 0
	}
	var lines []string
	consumed := 0
	for strings.HasPrefix(s[consumed:], "> ") {
		end := strings.IndexByte(s[consumed:], '\n')
		if end < 0 {
			lines = append(lines, s[consumed+2:])
			consumed = len(s)
			break
		}
		lines = append(lines, s[consumed+2:consumed+end])
		consumed += end + 1
	}
	start := p.out.Len()
	p.parse(strings.Join(lines, "\n"))
	p.entities = append(p.entities, Entity{Kind: "blockquote", Start: start, End: p.out.Len()})
	if consumed > 0 && s[consumed-1] == '\n' {
		p.out.WriteByte('\n')
	}
	return consumed
}

func (p *discordParser) mark(s string) int {
	for _, mark := range discordMarks {
		if !strings.HasPrefix(s, mark.token) {
			continue
		}
		end := closingMark(s, mark.token, len(mark.token))
		if end <= len(mark.token) {
			continue
		}
		start := p.out.Len()
		p.parse(s[len(mark.token):end])
		p.entities = append(p.entities, Entity{Kind: mark.kind, Start: start, End: p.out.Len()})
		if mark.token == "***" || mark.token == "___" {
			p.entities = append(p.entities, Entity{Kind: "italic", Start: start, End: p.out.Len()})
		}
		return end + len(mark.token)
	}
	return 0
}

// Skip complete inner marks while searching for the matching outer delimiter.
func closingMark(s, token string, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], token) {
			// A triple run closes the inner single mark before its outer double mark.
			// Only a double run can open a nested mark while a single mark is active.
			if len(token) == 1 && strings.HasPrefix(s[i:], token+token) && !strings.HasPrefix(s[i:], token+token+token) {
				if end := closingMark(s, token+token, i+2); end >= 0 {
					i = end + 1
					continue
				}
			}
			return i
		}
		if s[i] == '`' {
			count := len(s[i:]) - len(strings.TrimLeft(s[i:], "`"))
			if end := strings.Index(s[i+count:], s[i:i+count]); end >= 0 {
				i += count + end + count - 1
				continue
			}
		}
		for _, mark := range discordMarks {
			if mark.token == token || !strings.HasPrefix(s[i:], mark.token) {
				continue
			}
			if end := closingMark(s, mark.token, i+len(mark.token)); end >= 0 {
				i = end + len(mark.token) - 1
				break
			}
		}
	}
	return -1
}

func unescapedIndex(s, needle string, start int) int {
	for i := start; i+len(needle) <= len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], needle) {
			return i
		}
	}
	return -1
}

func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
