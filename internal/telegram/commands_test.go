package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/sleroq/zefxi/internal/bridge"
)

func TestLinkText(t *testing.T) {
	discord := bridge.User{ID: "1", Name: "elek", Username: "elek"}
	telegram := bridge.User{ID: "2", Name: "елек", Username: "elek_chan"}
	want := "Discord user: elek (@elek)\n\nLinked telegram users:\n- telegram: елек (@elek_chan)\n\n" +
		"Select an account to add or remove a link. " +
		"Messages sent from linked accounts will show up on discord as messages sent by elek (@elek)"
	for _, view := range []bridge.LinkView{
		{Platform: bridge.Telegram, Author: telegram, Linked: []bridge.User{discord}},
		{Platform: bridge.Discord, Author: discord, Linked: []bridge.User{telegram}},
	} {
		if got := linkText(view, english); got != want {
			t.Errorf("%s: got %q, want %q", view.Platform, got, want)
		}
	}
}

func TestLinkDoesNotRequireAdministrator(t *testing.T) {
	var answer string
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "sendMessage"), strings.HasSuffix(r.URL.Path, "editMessageText"):
			apiOK(t, w, `{"message_id":10}`)
		case strings.HasSuffix(r.URL.Path, "answerCallbackQuery"):
			answer = r.FormValue("text")
			apiOK(t, w, `true`)
		default:
			t.Errorf("unexpected method: %s", r.URL.Path)
		}
	})
	m := &models.Message{
		ID: 9, Chat: models.Chat{ID: -100}, From: &models.User{ID: 1}, Text: "/link Ada",
		ReplyToMessage: &models.Message{ID: 8, From: &models.User{ID: 7}},
	}
	if handled, err := c.command(context.Background(), m); !handled || err != nil {
		t.Fatalf("command: handled=%v err=%v", handled, err)
	}
	q := &models.CallbackQuery{
		ID: "query", From: models.User{ID: 1}, Data: "link:1:a:0",
		Message: models.MaybeInaccessibleMessage{Message: &models.Message{Chat: models.Chat{ID: -100}}},
	}
	if err := c.callback(context.Background(), q); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if !strings.Contains(answer, "Users linked.") || len(h.linked) == 0 {
		t.Fatalf("feedback=%q links=%v", answer, h.linked)
	}
	if len(h.messages) != 0 {
		t.Fatal("control response was bridged")
	}
}

func TestReactsDoesNotRequireAdministrator(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "sendMessage") {
			t.Errorf("unexpected authorization request: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if text := r.FormValue("text"); text != "Discord reactions:\n👍 - Alice, Bob" {
			t.Errorf("reaction summary: %q", text)
		}
		apiOK(t, w, `{"message_id":10}`)
	})
	m := &models.Message{ID: 9, From: &models.User{ID: 1}, Text: "/reacts", ReplyToMessage: &models.Message{ID: 8}}
	handled, err := c.command(context.Background(), m)
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

func TestWhois(t *testing.T) {
	human := &models.User{ID: 7, FirstName: "Ada"}
	cases := []struct {
		name     string
		target   *models.Message
		identity bridge.Identity
		want     string
		fallback bridge.User
		calls    int
	}{
		{"human linked", &models.Message{ID: 8, From: human}, bridge.Identity{Telegram: bridge.User{ID: "7", Name: "A&B"}, Discord: bridge.User{ID: "9", Name: "<Discord>"}}, `Telegram: <a href="tg://user?id=7">A&amp;B</a>` + "\n" + `Discord: <a href="https://discord.com/users/9">&lt;Discord&gt;</a>`, bridge.User{ID: "7", Name: "Ada"}, 1},
		{"bot unlinked", &models.Message{ID: 8, From: &models.User{ID: 1, IsBot: true}}, bridge.Identity{Discord: bridge.User{ID: "9", Username: "discord"}}, "Telegram: Not linked\n" + `Discord: <a href="https://discord.com/users/9">discord</a>`, bridge.User{}, 1},
		{"human unlinked", &models.Message{ID: 8, From: human}, bridge.Identity{Telegram: bridge.User{ID: "7", Name: "Ada"}}, `Telegram: <a href="tg://user?id=7">Ada</a>` + "\nDiscord: Not linked", bridge.User{ID: "7", Name: "Ada"}, 1},
		{"anonymous unknown", &models.Message{ID: 8, From: human, SenderChat: &models.Chat{ID: -1}}, bridge.Identity{}, "The author could not be identified.", bridge.User{}, 1},
		{"no reply", nil, bridge.Identity{}, "Reply to a message with /whois to identify its author.", bridge.User{}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				if r.FormValue("text") != tc.want || r.FormValue("parse_mode") != "HTML" {
					t.Errorf("text=%q parse_mode=%q", r.FormValue("text"), r.FormValue("parse_mode"))
				}
				var preview models.LinkPreviewOptions
				if err := json.Unmarshal([]byte(r.FormValue("link_preview_options")), &preview); err != nil {
					t.Fatal(err)
				}
				if preview.IsDisabled == nil || !*preview.IsDisabled {
					t.Error("link previews not disabled")
				}
				apiOK(t, w, `{"message_id":10}`)
			})
			h.identity = tc.identity
			update := &models.Update{Message: &models.Message{
				ID: 9, Chat: models.Chat{ID: -100}, From: &models.User{ID: 1},
				Text: "/whois@mybot", ReplyToMessage: tc.target,
			}}
			c.update(context.Background(), c.bot, update)
			if h.whoisCalls != tc.calls || h.whoisFallback != tc.fallback || (tc.calls > 0 && (h.whoisSource != bridge.Telegram || h.whoisID != "8")) {
				t.Errorf("whois call: %+v", h)
			}
			if len(h.messages) != 0 {
				t.Error("command was bridged")
			}
		})
	}
}

func TestLinkPaginationAndUnlink(t *testing.T) {
	var keyboard models.InlineKeyboardMarkup
	var answer string
	edits := 0
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "getChatMember"):
			apiOK(t, w, `{"status":"administrator","user":{"id":1}}`)
		case strings.HasSuffix(r.URL.Path, "sendMessage"), strings.HasSuffix(r.URL.Path, "editMessageReplyMarkup"), strings.HasSuffix(r.URL.Path, "editMessageText"):
			if err := json.Unmarshal([]byte(r.FormValue("reply_markup")), &keyboard); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(r.URL.Path, "editMessageReplyMarkup") {
				edits++
			}
			apiOK(t, w, `{"message_id":10}`)
		case strings.HasSuffix(r.URL.Path, "answerCallbackQuery"):
			answer = r.FormValue("text")
			apiOK(t, w, `true`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	})

	for i := range 11 {
		candidate := bridge.LinkCandidate{ID: fmt.Sprint(i), Username: fmt.Sprint(i), MatchPercent: 90}
		h.candidates = append(h.candidates, candidate)
	}
	m := &models.Message{
		ID: 9, From: &models.User{ID: 1}, Text: "/link",
		ReplyToMessage: &models.Message{From: &models.User{ID: 7}},
	}
	if _, err := c.command(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if len(keyboard.InlineKeyboard) != 6 || strings.Contains(keyboard.InlineKeyboard[1][0].Text, "%") {
		t.Fatalf("first page: %+v", keyboard)
	}

	q := &models.CallbackQuery{
		ID: "q", From: models.User{ID: 2}, Data: "link:1:p:1",
		Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 10, Chat: models.Chat{ID: -100}}},
	}
	if err := c.callback(context.Background(), q); err != nil || edits != 0 || !strings.Contains(answer, "Only the person") {
		t.Fatalf("authorization: %d %q %v", edits, answer, err)
	}

	q.From.ID = 1
	if err := c.callback(context.Background(), q); err != nil || edits != 1 || len(keyboard.InlineKeyboard) != 6 {
		t.Fatalf("second page: %d %+v %v", edits, keyboard, err)
	}
	q.Data = "link:1:p:2"
	if err := c.callback(context.Background(), q); err != nil || edits != 2 || len(keyboard.InlineKeyboard) != 3 {
		t.Fatalf("third page: %d %+v %v", edits, keyboard, err)
	}
	q.Data = "link:1:u:0"
	if err := c.callback(context.Background(), q); err != nil || h.unlinked != "7:existing" || len(c.links) != 1 {
		t.Fatalf("unlink: %q %v %v", h.unlinked, c.links, err)
	}

}

func TestLinkBridgedAndFetchErrorFeedback(t *testing.T) {
	for _, tc := range []struct {
		name string
		view bridge.LinkView
		fail bool
		want string
	}{
		{"discord origin", bridge.LinkView{Platform: bridge.Discord, Author: bridge.User{ID: "42", Name: "Discord author"}}, false, "Discord author"},
		{"telegram origin", bridge.LinkView{Platform: bridge.Telegram, Author: bridge.User{ID: "7", Name: "Telegram author"}}, false, "Telegram author"},
		{"unknown", bridge.LinkView{}, false, "could not be identified"},
		{"discord fetch failure", bridge.LinkView{}, true, "Unable to load link choices"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var text string
			c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "getChatMember") {
					apiOK(t, w, `{"status":"administrator","user":{"id":1}}`)
					return
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				text = r.FormValue("text")
				apiOK(t, w, `{"message_id":10}`)
			})
			h.view = &tc.view
			if tc.fail {
				h.viewErr = fmt.Errorf("Discord unavailable")
			}
			message := &models.Message{ID: 9, From: &models.User{ID: 1}, Text: "/link",
				ReplyToMessage: &models.Message{ID: 8, From: &models.User{ID: 99, IsBot: true}},
			}
			_, err := c.command(context.Background(), message)
			if (err != nil) != tc.fail || !strings.Contains(text, tc.want) || h.viewFallback.ID != "" {
				t.Fatalf("text=%q fallback=%+v err=%v", text, h.viewFallback, err)
			}
		})
	}
}

func TestLanguageSelection(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"en", english.notLinked}, {"US-en", english.notLinked}, {"fr", english.notLinked}, {"", english.notLinked},
		{"RU-ru", russian.notLinked}, {"pl_PL", polish.notLinked}, {"uk-UA", ukrainian.notLinked}, {"ua", ukrainian.notLinked},
	} {
		if got := language(tc.code).notLinked; got != tc.want {
			t.Errorf("language(%q) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestLocalizedCommandsAndReactions(t *testing.T) {
	for _, tc := range []struct{ code, whois, reacts, missing, empty string }{
		{"en", "Reply to a message with /whois to identify its author.", "Discord reactions:\n👍 - Alice, Bob", "This message is not linked to a Discord message.", "No Discord reactions."},
		{"ru", "Ответьте на сообщение командой /whois, чтобы узнать его автора.", "Реакции Discord:\n👍 - Alice, Bob", "Это сообщение не связано с сообщением Discord.", "Нет реакций Discord."},
		{"pl", "Odpowiedz na wiadomość poleceniem /whois, aby poznać jej autora.", "Reakcje na Discordzie:\n👍 - Alice, Bob", "Ta wiadomość nie jest powiązana z wiadomością na Discordzie.", "Brak reakcji na Discordzie."},
		{"uk", "Відповідайте на повідомлення командою /whois, щоб дізнатися його автора.", "Реакції Discord:\n👍 - Alice, Bob", "Це повідомлення не пов'язане з повідомленням Discord.", "Немає реакцій Discord."},
	} {
		t.Run(tc.code, func(t *testing.T) {
			var text string
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				text = r.FormValue("text")
				apiOK(t, w, `{"message_id":10}`)
			})
			m := &models.Message{ID: 1, From: &models.User{ID: 1, LanguageCode: tc.code}, Text: "/whois"}
			if _, err := c.command(context.Background(), m); err != nil || text != tc.whois {
				t.Fatalf("whois: %q %v", text, err)
			}
			m.Text, m.ReplyToMessage = "/reacts", &models.Message{ID: 8}
			if _, err := c.command(context.Background(), m); err != nil || text != tc.reacts {
				t.Fatalf("reacts: %q %v", text, err)
			}
			if got := renderReactions(bridge.ReactionSummary{}, language(tc.code)); got != tc.missing {
				t.Errorf("missing: %q", got)
			}
			if got := renderReactions(bridge.ReactionSummary{LinkFound: true}, language(tc.code)); got != tc.empty {
				t.Errorf("empty: %q", got)
			}
		})
	}
}

func TestLinkLocalePreservedAcrossCallbacks(t *testing.T) {
	var text, answer string
	var keyboard models.InlineKeyboardMarkup
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "answerCallbackQuery"):
			answer = r.FormValue("text")
			apiOK(t, w, `true`)
		default:
			text = r.FormValue("text")
			if err := json.Unmarshal([]byte(r.FormValue("reply_markup")), &keyboard); err != nil {
				t.Fatal(err)
			}
			apiOK(t, w, `{"message_id":10}`)
		}
	})

	for i := range 6 {
		h.candidates = append(h.candidates, bridge.LinkCandidate{ID: fmt.Sprint(i)})
	}
	m := &models.Message{
		ID: 9, From: &models.User{ID: 1, LanguageCode: "ru"}, Text: "/link",
		ReplyToMessage: &models.Message{ID: 8, From: &models.User{ID: 7}},
	}
	if _, err := c.command(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Пользователь Discord:") || keyboard.InlineKeyboard[0][0].Text != "Отвязать existing" || keyboard.InlineKeyboard[5][0].Text != "Далее →" {
		t.Fatalf("initial: %q %+v", text, keyboard)
	}
	q := &models.CallbackQuery{
		ID: "q", From: models.User{ID: 2, LanguageCode: "pl"}, Data: "link:1:p:1",
		Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 10, Chat: models.Chat{ID: -100}}},
	}
	if err := c.callback(context.Background(), q); err != nil || answer != polish.unauthorized {
		t.Fatalf("unauthorized: %q %v", answer, err)
	}
	q.From.ID = 1
	if err := c.callback(context.Background(), q); err != nil || keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-1][0].Text != "← Назад" {
		t.Fatalf("pagination: %+v %v", keyboard, err)
	}
	q.Data = "link:1:a:0"
	if err := c.callback(context.Background(), q); err != nil || answer != polish.linked || !strings.Contains(text, "Пользователь Discord:") || keyboard.InlineKeyboard[0][0].Text != "Отвязать existing" {
		t.Fatalf("change: %q %q %+v %v", answer, text, keyboard, err)
	}
}
