package discord

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
func TestEditFetchesFullMessageAndFiltersAuthors(t *testing.T) {
	c, err := New("token", "guild", "channel", 100)
	if err != nil {
		t.Fatal(err)
	}
	h := &editHandler{}
	payload := `{"id":"42","content":"full content","author":{"id":"5","username":"Alice"},"type":0}`
	c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/channels/channel/messages/42") {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	c.handleEdit(context.Background(), h, "42", true)
	if len(h.edits) != 1 || h.edits[0].Text.Value != "full content" || !h.edits[0].PreserveAttachments {
		t.Fatalf("edits: %+v", h.edits)
	}
	payload = `{"id":"42","content":"ignored","author":{"id":"5","bot":true},"type":0}`
	c.handleEdit(context.Background(), h, "42", false)
	if len(h.edits) != 1 {
		t.Fatal("bot edit forwarded")
	}
}
