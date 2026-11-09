package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/sleroq/zefxi/internal/bridge"
)

type whoisHandler struct {
	messageHandler
	identity  bridge.Identity
	calls     int
	platform  bridge.Platform
	messageID string
	fallback  bridge.User
}

func (h *whoisHandler) Whois(_ context.Context, platform bridge.Platform, messageID string, fallback bridge.User) (bridge.Identity, error) {
	h.calls++
	h.platform, h.messageID, h.fallback = platform, messageID, fallback
	return h.identity, nil
}

func TestWhoisReplies(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reference  bool
		target     *discordgo.Message
		fetched    string
		identity   bridge.Identity
		fallbackID string
		want       []string
	}{
		{name: "guidance", want: []string{"Reply to a message", "/whois"}},
		{name: "bridged webhook", reference: true, target: &discordgo.Message{WebhookID: "webhook", Author: &discordgo.User{ID: "webhook-user"}}, identity: bridge.Identity{Discord: bridge.User{ID: "42", Name: "A[lice]"}, Telegram: bridge.User{ID: "7", Name: "Bob", Username: "bob"}}, want: []string{`[A\[lice\]](https://discord.com/users/42)`, "[Bob](https://t.me/bob)"}},
		{name: "human fallback fetched", reference: true, fetched: `{"id":"original","author":{"id":"42","username":"alice"},"member":{"nick":"Ali"}}`, identity: bridge.Identity{Discord: bridge.User{ID: "42", Name: "Ali"}}, fallbackID: "42", want: []string{"[Ali](https://discord.com/users/42)", "Telegram: Not linked"}},
		{name: "bot is not a person", reference: true, target: &discordgo.Message{Author: &discordgo.User{ID: "bot", Bot: true}}, want: []string{"Could not identify"}},
		{name: "telegram without username", reference: true, target: &discordgo.Message{WebhookID: "webhook"}, identity: bridge.Identity{Telegram: bridge.User{ID: "7", Name: "@everyone"}}, want: []string{"Discord: Not linked", "no public Telegram username", "@\u200beveryone"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New("token", "guild", "channel", 1024)
			if err != nil {
				t.Fatal(err)
			}

			var reply string
			fetches := 0
			c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"id":"sent"}`
				if r.Method == http.MethodGet {
					fetches++
					if !strings.HasSuffix(r.URL.Path, "/channels/channel/messages/original") || tc.fetched == "" {
						t.Fatalf("unexpected fetch: %s", r.URL)
					}
					body = tc.fetched
				} else {
					var payload struct {
						Content   string                 `json:"content"`
						Flags     discordgo.MessageFlags `json:"flags"`
						Reference struct {
							MessageID string `json:"message_id"`
						} `json:"message_reference"`
						Mentions struct {
							Parse       []string `json:"parse"`
							RepliedUser bool     `json:"replied_user"`
						} `json:"allowed_mentions"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if payload.Reference.MessageID != "command" || payload.Mentions.Parse == nil || len(payload.Mentions.Parse) != 0 || payload.Mentions.RepliedUser {
						t.Fatalf("unsafe reply: %+v", payload)
					}
					if payload.Flags&discordgo.MessageFlagsSuppressEmbeds == 0 {
						t.Error("link previews not disabled")
					}
					reply = payload.Content
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}

			h := &whoisHandler{identity: tc.identity}
			e := &discordgo.MessageCreate{Message: &discordgo.Message{
				ID: "command", Content: "  /whois \t", Author: &discordgo.User{ID: "sender"},
				Attachments: []*discordgo.MessageAttachment{{URL: ":invalid"}},
			}}
			if tc.reference {
				e.MessageReference = &discordgo.MessageReference{MessageID: "original"}
				e.ReferencedMessage = tc.target
			}
			c.handleMessage(context.Background(), h, e)

			if h.message.ID != "" {
				t.Fatal("command forwarded")
			}
			if tc.reference && (h.calls != 1 || h.platform != bridge.Discord || h.messageID != "original" || h.fallback.ID != tc.fallbackID) {
				t.Fatalf("whois call: %+v", h)
			}
			if !tc.reference && h.calls != 0 {
				t.Fatal("unexpected lookup")
			}
			if (fetches == 1) != (tc.fetched != "") {
				t.Fatalf("fetches: %d", fetches)
			}
			for _, want := range tc.want {
				if !strings.Contains(reply, want) {
					t.Errorf("reply %q missing %q", reply, want)
				}
			}
			if strings.Contains(reply, "tg://") {
				t.Fatalf("invalid Telegram link: %s", reply)
			}
		})
	}
}

func TestWhoisCommandRequiresExactToken(t *testing.T) {
	c := &Client{}
	h := &messageHandler{}
	e := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "message", Content: "/whoiselse", Author: &discordgo.User{ID: "author"},
	}}
	c.handleMessage(context.Background(), h, e)
	if h.message.ID != "message" {
		t.Fatal("non-command not forwarded")
	}
}

func TestWhoisFromBotsAndWebhooksIsForwarded(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bot     bool
		webhook string
	}{
		{"bot", true, ""},
		{"webhook", true, "other-webhook"},
		{"webhook without bot flag", false, "other-webhook"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{}
			h := &messageHandler{}
			c.handleMessage(context.Background(), h, &discordgo.MessageCreate{Message: &discordgo.Message{
				ID: "command", Content: "/whois", Author: &discordgo.User{ID: "author", Bot: tc.bot}, WebhookID: tc.webhook,
			}})
			if h.message.ID != "command" || h.message.Text.Value != "/whois" {
				t.Fatalf("message: %+v", h.message)
			}
		})
	}
}
