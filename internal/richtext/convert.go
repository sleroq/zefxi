package richtext

import (
	"unicode/utf8"
)

// FromTelegram translates Telegram's UTF-16 ranges into UTF-8 byte ranges.
func FromTelegram(value string, entities []TelegramEntity) Text {
	t := Text{Value: value}
	offsets := telegramOffsets(value)

	for _, e := range entities {
		start, ok := offsets[e.Offset]
		end, okEnd := offsets[e.Offset+e.Length]
		if !ok || !okEnd || end <= start {
			continue
		}
		kind := e.Kind
		switch kind {
		case "strikethrough":
			kind = "strike"
		case "text_link", "url":
			kind = "link"
		case "text_mention":
			kind = "mention"
		case "expandable_blockquote":
			kind = "blockquote"
		}
		if !supported(kind) {
			continue
		}
		url := e.URL
		if e.Kind == "url" {
			url = value[start:end]
		}
		entity := Entity{Kind: kind, Start: start, End: end, URL: url, UserID: e.UserID}
		t.Entities = append(t.Entities, entity)
	}
	return t
}

func supported(kind string) bool {
	switch kind {
	case "bold", "italic", "underline", "strike", "spoiler", "code", "pre", "link", "mention", "blockquote":
		return true
	}
	return false
}

func telegramOffsets(value string) map[int]int {
	offsets := map[int]int{0: 0}
	units := 0
	for i, r := range value {
		units++
		if r > 0xffff {
			units++
		}
		offsets[units] = i + utf8.RuneLen(r)
	}
	return offsets
}
