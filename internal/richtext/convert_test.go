package richtext

import (
	"strings"
	"testing"
)

func TestTelegramDiscordRoundtrip(t *testing.T) {
	original := FromTelegram("😀 bold é @alice", []TelegramEntity{
		{Kind: "bold", Offset: 3, Length: 6},
		{Kind: "italic", Offset: 8, Length: 1},
		{Kind: "mention", Offset: 10, Length: 6},
	})
	discord := ToDiscord(original, func(e Entity, visible string) string {
		if visible != "@alice" {
			t.Fatalf("mention source: %q", visible)
		}
		return "<@123>"
	})
	if discord != "😀 **bold *é*** <@123>" {
		t.Fatalf("discord: %q", discord)
	}
	parsed := FromDiscord(discord)
	telegram := ToTelegram(parsed, func(e Entity, visible string) string {
		if e.UserID != "123" || visible != "<@123>" {
			t.Fatalf("mention: %+v %q", e, visible)
		}
		return `<a href="tg://user?id=456">Alice</a>`
	})
	want := `😀 <b>bold <i>é</i></b> <a href="tg://user?id=456">Alice</a>`
	if telegram != want {
		t.Fatalf("telegram: %q; want %q", telegram, want)
	}
}

func TestDiscordFormatting(t *testing.T) {
	input := "***nested*** __under__ ~~gone~~ ||secret|| [é](https://example.com/a) `a < b`\n```go\nx & y\n```"
	parsed := FromDiscord(input)
	want := "<b><i>nested</i></b> <u>under</u> <s>gone</s> <tg-spoiler>secret</tg-spoiler> " +
		"<a href=\"https://example.com/a\">é</a> <code>a &lt; b</code>\n<pre>x &amp; y\n</pre>"
	if got := ToTelegram(parsed, nil); got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	again := FromDiscord(ToDiscord(parsed, nil))
	if again.Value != parsed.Value {
		t.Fatalf("roundtrip text: %q != %q", again.Value, parsed.Value)
	}
}

func TestDiscordCustomEmoji(t *testing.T) {
	input := "Hi <:funnyface1:123456> **<a:dancing_cat:987654>** `<:literal:123>` <:bad:notanid>"
	want := "Hi :funnyface1: <b>:dancing_cat:</b> <code>&lt;:literal:123&gt;</code> &lt;:bad:notanid&gt;"
	if got := ToTelegram(FromDiscord(input), nil); got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
}

func TestEscapingAndInvalidRanges(t *testing.T) {
	text := FromTelegram("😀 <& @everyone *raw*", []TelegramEntity{
		{Kind: "bold", Offset: 1, Length: 1},
		{Kind: "text_link", Offset: 3, Length: 2, URL: `https://example.com/?a="&b=1`},
	})
	if len(text.Entities) != 1 {
		t.Fatalf("invalid UTF-16 range accepted: %+v", text)
	}
	if got := ToTelegram(text, nil); !strings.Contains(got, `&#34;&amp;b=1`) || !strings.Contains(got, "&lt;&amp;") {
		t.Fatalf("HTML escaping: %q", got)
	}
	discord := ToDiscord(text, nil)
	if strings.Contains(discord, "@everyone") || !strings.Contains(discord, `\*raw\*`) {
		t.Fatalf("unsafe discord text: %q", discord)
	}
	mention := Text{Value: "@alice", Entities: []Entity{{Kind: "mention", Start: 0, End: 6}}}
	if got := ToDiscord(mention, func(Entity, string) string { return "" }); got != "@alice" {
		t.Fatalf("fallback: %q", got)
	}
}

func TestTelegramEntityKinds(t *testing.T) {
	text := FromTelegram("😀 https://example.com Alice", []TelegramEntity{
		{Kind: "url", Offset: 3, Length: 19},
		{Kind: "text_mention", Offset: 23, Length: 5, UserID: "9"},
	})
	if len(text.Entities) != 2 || text.Entities[0].URL != "https://example.com" || text.Entities[1].UserID != "9" {
		t.Fatalf("entities: %+v", text.Entities)
	}
}

func TestBlockquote(t *testing.T) {
	parsed := FromDiscord("> **hello**\n> world\nafter")
	if got := ToTelegram(parsed, nil); got != "<blockquote><b>hello</b>\nworld</blockquote>\nafter" {
		t.Fatalf("quote: %q", got)
	}
}
