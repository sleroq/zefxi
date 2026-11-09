package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sleroq/zefxi/internal/bridge"
)

func TestShareableURL(t *testing.T) {
	for _, raw := range []string{"https://api.telegram.org/file/botSECRET/file.jpg", "https://discord.com/api/webhooks/123/SECRET", "https://user:secret@example.com/file", "file:///secret"} {
		if got := shareableURL(raw); got != "" {
			t.Errorf("shareableURL(%q) = %q", raw, got)
		}
	}
	const public = "https://cdn.discordapp.com/attachments/123/file.png?ex=123"
	if shareableURL(public) != public {
		t.Fatal("public signed attachment URL lost")
	}
}

func TestDownloadBounds(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()
	c := &Client{http: server.Client()}
	attachment := &discordgo.MessageAttachment{URL: server.URL, Filename: "test.txt", Size: 5}
	a, err := c.download(context.Background(), attachment)
	if err != nil || string(a.Data) != "hello" || a.Size != 5 {
		t.Fatalf("download: %+v, %v", a, err)
	}
	attachment.Size = int(telegramDownloadLimit + 1)
	a, err = c.download(context.Background(), attachment)
	if err != nil || len(a.Data) != 0 || a.URL != server.URL || calls != 1 {
		t.Fatalf("large download: %+v, %v; calls %d", a, err, calls)
	}
}

type messageHandler struct {
	bridge.Handler
	message  bridge.Message
	platform bridge.Platform
}

func (h *messageHandler) HandleMessage(_ context.Context, platform bridge.Platform, message bridge.Message) error {
	h.platform, h.message = platform, message
	return nil
}

type stickerTransport struct {
	target string
	base   http.RoundTripper
}

func (s stickerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme = "http"
	r.URL.Host = s.target
	return s.base.RoundTrip(r)
}

func TestHandleStickerOnlyMessage(t *testing.T) {
	for _, tc := range []struct {
		name          string
		format        discordgo.StickerFormat
		extension     string
		contentType   string
		status        int
		stickerFormat bridge.StickerFormat
	}{
		{"PNG", discordgo.StickerFormatTypePNG, ".png", "image/png", http.StatusOK, bridge.StickerPNG},
		{"APNG", discordgo.StickerFormatTypeAPNG, ".png", "image/png", http.StatusOK, bridge.StickerAPNG},
		{"GIF", discordgo.StickerFormatTypeGIF, ".gif", "image/gif", http.StatusOK, bridge.StickerGIF},
		{"Lottie", discordgo.StickerFormatTypeLottie, ".json", "application/json", http.StatusOK, bridge.StickerLottie},
		{"download failure", discordgo.StickerFormatTypePNG, ".png", "image/png", http.StatusNotFound, bridge.StickerPNG},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/stickers/123"+tc.extension {
					t.Errorf("unexpected sticker path: %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("sticker data"))
			}))
			defer server.Close()
			client := server.Client()
			client.Transport = stickerTransport{target: server.Listener.Addr().String(), base: client.Transport}
			c := &Client{http: client}

			h := &messageHandler{}
			c.handleMessage(context.Background(), h, &discordgo.MessageCreate{Message: &discordgo.Message{
				ID: "message", Author: &discordgo.User{ID: "author"},
				StickerItems: []*discordgo.StickerItem{{ID: "123", Name: "wave", FormatType: tc.format}},
			}})

			if calls != 1 || h.platform != bridge.Discord || len(h.message.Attachments) != 1 {
				t.Fatalf("calls=%d, platform=%s, message=%+v", calls, h.platform, h.message)
			}
			a := h.message.Attachments[0]
			if a.StickerFormat != tc.stickerFormat || a.Name != "wave"+tc.extension || a.ContentType != tc.contentType || a.URL != "https://cdn.discordapp.com/stickers/123"+tc.extension {
				t.Fatalf("sticker metadata: %+v", a)
			}
			if tc.status == http.StatusOK {
				if string(a.Data) != "sticker data" || a.Size != int64(len(a.Data)) {
					t.Fatalf("sticker download: %+v", a)
				}
			} else if len(a.Data) != 0 {
				t.Fatalf("failed download retained data: %+v", a)
			}
		})
	}
}

func TestNew(t *testing.T) {
	c, err := New("token", "guild", "channel", 1024)
	if err != nil {
		t.Fatal(err)
	}
	want := discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions | discordgo.IntentsGuildMembers
	if c.session.Identify.Intents != want {
		t.Fatal("missing gateway intents")
	}

}

func TestGuildMembersPaginatesAndExcludesBots(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("limit") != "1000" {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			if r.URL.Query().Get("after") != "" {
				t.Error("unexpected cursor")
			}
			var body strings.Builder
			body.WriteByte('[')
			for i := 1; i <= 1000; i++ {
				if i > 1 {
					body.WriteByte(',')
				}
				fmt.Fprintf(&body, `{"user":{"id":"%d","username":"user","bot":%t}}`, i, i == 1000)
			}
			body.WriteByte(']')
			if _, err := fmt.Fprint(w, body.String()); err != nil {
				t.Error(err)
			}
		} else {
			if r.URL.Query().Get("after") != "1000" {
				t.Errorf("cursor: %s", r.URL.RawQuery)
			}
			if _, err := fmt.Fprint(w, `[{"user":{"id":"1001","username":"last"}}]`); err != nil {
				t.Error(err)
			}
		}
	}))
	defer server.Close()

	session, err := discordgo.New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client = server.Client()
	session.Client.Transport = stickerTransport{target: server.Listener.Addr().String(), base: http.DefaultTransport}
	c := &Client{session: session, guildID: "guild"}
	users, err := c.GuildMembers(context.Background())
	if err != nil || calls != 2 || len(users) != 1000 || users[len(users)-1].ID != "1001" {
		t.Fatalf("users=%d calls=%d err=%v", len(users), calls, err)
	}
}

type editHandler struct {
	bridge.Handler
	edits []bridge.Message
}

func (h *editHandler) HandleEdit(_ context.Context, _ bridge.Platform, m bridge.Message) error {
	h.edits = append(h.edits, m)
	return nil
}
func TestCreateAndEditFilterMessages(t *testing.T) {
	c, err := New("token", "guild", "channel", 100)
	if err != nil {
		t.Fatal(err)
	}
	c.session.State.User = &discordgo.User{ID: "own-bot"}
	c.webhookID = "own-webhook"
	var payload []byte
	c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(string(payload))),
		}, nil
	})}

	for _, tc := range []struct {
		name    string
		author  *discordgo.User
		webhook string
		typ     discordgo.MessageType
		accept  bool
	}{
		{"human", &discordgo.User{ID: "human"}, "", discordgo.MessageTypeDefault, true},
		{"own bot reply", &discordgo.User{ID: "own-bot", Bot: true}, "", discordgo.MessageTypeReply, false},
		{"own webhook", &discordgo.User{ID: "webhook-user", Bot: true}, "own-webhook", discordgo.MessageTypeDefault, false},
		{"other bot reply", &discordgo.User{ID: "other-bot", Bot: true}, "", discordgo.MessageTypeReply, true},
		{"other webhook", &discordgo.User{ID: "other-webhook-user", Bot: true}, "other-webhook", discordgo.MessageTypeDefault, true},
		{"chat input response", &discordgo.User{ID: "other-bot", Bot: true}, "", discordgo.MessageTypeChatInputCommand, true},
		{"context menu response", &discordgo.User{ID: "other-bot", Bot: true}, "", discordgo.MessageTypeContextMenuCommand, true},
		{"missing author", nil, "", discordgo.MessageTypeDefault, false},
		{"unsupported", &discordgo.User{ID: "human"}, "", discordgo.MessageTypeChannelNameChange, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := &discordgo.Message{
				ID: "42", GuildID: "guild", ChannelID: "channel", Content: "full content",
				Author: tc.author, WebhookID: tc.webhook, Type: tc.typ,
			}
			payload, err = json.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			queue := newEventQueue(ctx)
			h := &messageHandler{}
			c.queueMessage(ctx, h, queue, newGIFLookups(time.Millisecond), message)
			done := make(chan struct{})
			queue.enqueue(ctx, func() { close(done) })
			<-done
			cancel()
			<-queue.done
			if (h.message.ID != "") != tc.accept {
				t.Errorf("create forwarded: %+v", h.message)
			}
			edits := &editHandler{}
			c.handleEdit(context.Background(), edits, "42", true, false)
			if (len(edits.edits) != 0) != tc.accept {
				t.Fatalf("edits: %+v", edits.edits)
			}
			if tc.accept && (edits.edits[0].Text.Value != "full content" || !edits.edits[0].PreserveAttachments) {
				t.Fatalf("edit content: %+v", edits.edits[0])
			}
		})
	}
}

func TestMessageFilterWithoutOwnIDs(t *testing.T) {
	c, err := New("token", "guild", "channel", 100)
	if err != nil {
		t.Fatal(err)
	}
	message := &discordgo.Message{Author: &discordgo.User{ID: "other", Bot: true}}
	if !c.acceptsMessage(message) {
		t.Fatal("other bot rejected before own IDs are configured")
	}
}

func TestOwnBotSystemMessageNotForwarded(t *testing.T) {
	c, err := New("token", "guild", "channel", 100)
	if err != nil {
		t.Fatal(err)
	}
	c.session.State.User = &discordgo.User{ID: "own-bot"}
	ctx, cancel := context.WithCancel(context.Background())
	queue := newEventQueue(ctx)
	h := &messageHandler{}
	c.queueMessage(ctx, h, queue, newGIFLookups(time.Millisecond), &discordgo.Message{
		ID: "pin", GuildID: "guild", ChannelID: "channel", Type: discordgo.MessageTypeChannelPinnedMessage,
		Author: &discordgo.User{ID: "own-bot", Bot: true},
	})
	done := make(chan struct{})
	queue.enqueue(ctx, func() { close(done) })
	<-done
	cancel()
	<-queue.done
	if h.message.ID != "" {
		t.Fatal("own pin service message forwarded")
	}
}
