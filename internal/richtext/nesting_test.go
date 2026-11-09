package richtext

import "testing"

func TestNestedMarkdownRoundtrip(t *testing.T) {
	cases := []struct{ markdown, plain, html string }{
		{"**bold *italic*** and *italic **bold***", "bold italic and italic bold", "<b>bold <i>italic</i></b> and <i>italic <b>bold</b></i>"},
		{"**bold *italic***", "bold italic", "<b>bold <i>italic</i></b>"},
		{"*italic **bold***", "italic bold", "<i>italic <b>bold</b></i>"},
		{"**😀 *é*** after", "😀 é after", "<b>😀 <i>é</i></b> after"},
		{"*outer **😀 bold** tail*", "outer 😀 bold tail", "<i>outer <b>😀 bold</b> tail</i>"},
		{"**outer ||secret _é_|| tail**", "outer secret é tail", "<b>outer <tg-spoiler>secret <i>é</i></tg-spoiler> tail</b>"},
		{"**a `*` b**", "a * b", "<b>a <code>*</code> b</b>"},
		{"[**😀**](https://example.com/a(b))", "😀", `<a href="https://example.com/a(b)"><b>😀</b></a>`},
		{"``a ` b``", "a ` b", "<code>a ` b</code>"},
		{"___é___", "é", "<i><u>é</u></i>"},
		{`\*literal\* @username`, `*literal* @username`, `*literal* @username`},
	}
	for _, tt := range cases {
		t.Run(tt.markdown, func(t *testing.T) {
			parsed := FromDiscord(tt.markdown)
			if parsed.Value != tt.plain {
				t.Fatalf("plain: %q", parsed.Value)
			}
			if got := ToTelegram(parsed, nil); got != tt.html {
				t.Fatalf("HTML: %q; want %q", got, tt.html)
			}
			roundtrip := FromDiscord(ToDiscord(parsed, nil))
			if roundtrip.Value != tt.plain {
				t.Fatalf("roundtrip plain: %q", roundtrip.Value)
			}
			if got := ToTelegram(roundtrip, nil); got != tt.html {
				t.Fatalf("roundtrip HTML: %q; want %q", got, tt.html)
			}
		})
	}
}
