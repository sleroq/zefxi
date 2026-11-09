package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/sleroq/zefxi/internal/bridge"
)

type recordingHandler struct {
	messages      []bridge.Message
	pins          []string
	reactions     []bridge.Reaction
	linked        []string
	identity      bridge.Identity
	whoisCalls    int
	whoisSource   bridge.Platform
	whoisID       string
	whoisFallback bridge.User
	candidates    []bridge.LinkCandidate
	unlinked      string
	view          *bridge.LinkView
	viewErr       error
	viewFallback  bridge.User
}

func (h *recordingHandler) HandleTelegramPin(_ context.Context, id string) error {
	h.pins = append(h.pins, id)
	return nil
}
func (h *recordingHandler) RefreshDiscordPins(context.Context) error { return nil }

func (h *recordingHandler) HandleMessage(_ context.Context, _ bridge.Platform, m bridge.Message) error {
	h.messages = append(h.messages, m)
	return nil
}
func (h *recordingHandler) HandleDiscordDelete(context.Context, string) error { return nil }
func (h *recordingHandler) HandleEdit(_ context.Context, _ bridge.Platform, m bridge.Message) error {
	h.messages = append(h.messages, m)
	return nil
}
func (h *recordingHandler) HandleReaction(_ context.Context, _ bridge.Platform, r bridge.Reaction) error {
	h.reactions = append(h.reactions, r)
	return nil
}
func (h *recordingHandler) LinkView(ctx context.Context, _ string, fallback bridge.User, _ string) (bridge.LinkView, error) {
	h.viewFallback = fallback
	if h.viewErr != nil {
		return bridge.LinkView{}, h.viewErr
	}
	if h.view != nil {
		return *h.view, nil
	}
	candidates, _ := h.LinkCandidates(ctx, fallback, "")
	return bridge.LinkView{
		Platform: bridge.Telegram, Author: fallback, Candidates: candidates, Linked: []bridge.User{{ID: "existing"}},
	}, nil
}
func (h *recordingHandler) UnlinkPair(_ context.Context, tg, discord string) error {
	h.unlinked = tg + ":" + discord
	return nil
}
func (h *recordingHandler) LinkCandidates(context.Context, bridge.User, string) ([]bridge.LinkCandidate, error) {
	if h.candidates != nil {
		return h.candidates, nil
	}
	return []bridge.LinkCandidate{{ID: "discord-7", Name: "Ada"}}, nil
}
func (h *recordingHandler) UnlinkUser(_ context.Context, id string) error {
	h.unlinked = id
	return nil
}

func (h *recordingHandler) LinkUsers(_ context.Context, tg, discord string) error {
	h.linked = []string{tg, discord}
	return nil
}
func (h *recordingHandler) ReactionSummary(context.Context, string) (bridge.ReactionSummary, error) {
	return bridge.ReactionSummary{
		LinkFound: true,
		Reactions: []bridge.ReactionUsers{{Emoji: "👍", Users: []bridge.User{{Name: "Alice"}, {Name: "Bob"}}}},
	}, nil
}

func (h *recordingHandler) Whois(_ context.Context, source bridge.Platform, id string, fallback bridge.User) (bridge.Identity, error) {
	h.whoisCalls++
	h.whoisSource, h.whoisID, h.whoisFallback = source, id, fallback
	return h.identity, nil
}

func testClient(t *testing.T, serve http.HandlerFunc) (*Client, *recordingHandler) {
	t.Helper()
	server := httptest.NewServer(serve)
	t.Cleanup(server.Close)
	b, err := bot.New("123:secret", bot.WithSkipGetMe(), bot.WithServerURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	h := &recordingHandler{}
	return &Client{bot: b, chatID: -100, handler: h, links: make(map[string]linkChoice)}, h
}
func apiOK(t *testing.T, w http.ResponseWriter, result string) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"ok":true,"result":%s}`, result); err != nil {
		t.Error(err)
	}
}

func TestSlashCommandsAreNotForwarded(t *testing.T) {
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API request: %s", r.URL.Path)
	})
	for _, text := range []string{"/unknown", "/unknown@other_bot argument", "hello /unknown"} {
		c.update(context.Background(), c.bot, &models.Update{Message: &models.Message{
			Chat: models.Chat{ID: -100}, From: &models.User{ID: 7}, Text: text,
		}})
	}
	if len(h.messages) != 1 || h.messages[0].Text.Value != "hello /unknown" {
		t.Fatalf("forwarded messages: %+v", h.messages)
	}
}

func TestIncomingOversizeKeepsNoticeAndUTF16Entities(t *testing.T) {
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("oversized file must not be requested: %s", r.URL.Path)
		apiOK(t, w, `true`)
	})

	c.update(context.Background(), c.bot, &models.Update{
		Message: &models.Message{
			ID:             9,
			Chat:           models.Chat{ID: -100},
			From:           &models.User{ID: 7, FirstName: "Ada"},
			Text:           "😀bold",
			Entities:       []models.MessageEntity{{Type: "bold", Offset: 2, Length: 4}},
			ReplyToMessage: &models.Message{ID: 8},
			Document:       &models.Document{FileID: "large", FileName: "large.zip", FileSize: downloadLimit + 1},
		},
	})
	if len(h.messages) != 1 {
		t.Fatalf("messages: %+v", h.messages)
	}
	m := h.messages[0]
	if m.ReplyTo != "8" || m.Author.ID != "7" {
		t.Fatalf("lost message details: %+v", m)
	}
	if len(m.Text.Entities) != 1 || m.Text.Entities[0].Start != 4 || m.Text.Entities[0].End != 8 {
		t.Fatalf("UTF-16 entity conversion: %+v", m.Text.Entities)
	}
	if m.Text.Value != "😀bold" {
		t.Fatalf("duplicate inline attachment notice: %s", m.Text.Value)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].URL != "" || len(m.Attachments[0].Data) != 0 || m.Attachments[0].Name != "large.zip" || m.Attachments[0].Size != downloadLimit+1 {
		t.Fatalf("unsafe placeholder: %+v", m.Attachments)
	}
}

func TestSendReturnsTextUploadAndOversizeLinkIDs(t *testing.T) {
	var text map[string]any
	var paths []string

	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "sendDocument") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			if r.FormValue("caption") != "<b>A&amp;B</b>: <b>hello</b>" || r.FormValue("parse_mode") != "HTML" {
				t.Errorf("caption: %q", r.FormValue("caption"))
			}
			file, _, err := r.FormFile("document")
			if err != nil {
				t.Error(err)
			} else {
				defer func() {
					if err := file.Close(); err != nil {
						t.Error(err)
					}
				}()
				data, _ := io.ReadAll(file)
				if string(data) != "hello" {
					t.Errorf("upload: %q", data)
				}
			}
		} else if text == nil {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			var rp map[string]any
			if err := json.Unmarshal([]byte(r.FormValue("reply_parameters")), &rp); err != nil {
				t.Error(err)
			}
			text = map[string]any{"text": r.FormValue("text"), "parse_mode": r.FormValue("parse_mode"), "reply_parameters": rp}
		}
		apiOK(t, w, fmt.Sprintf(`{"message_id":%d}`, len(paths)))
	})

	outbound := bridge.OutboundMessage{
		Author:  bridge.User{Name: "A&B"},
		Text:    "<b>hello</b>",
		ReplyTo: "42",
		Attachments: []bridge.Attachment{
			{Name: "a.txt", Data: []byte("hello")},
			{
				Name: "large.mp4",
				Size: uploadLimit + 1,
				URL:  "https://cdn.discordapp.com/large.mp4",
			},
		},
	}
	ids, err := c.Send(context.Background(), outbound)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(ids, ",") != "1,2" {
		t.Fatalf("IDs: %v", ids)
	}
	if text["text"] != "<b>A&amp;B</b>\nlarge.mp4: https://cdn.discordapp.com/large.mp4" || text["parse_mode"] != "HTML" {
		t.Fatalf("text: %+v", text)
	}
	rp, ok := text["reply_parameters"].(map[string]any)
	if !ok || rp["message_id"] != float64(42) {
		t.Fatalf("reply: %+v", text)
	}
}

func TestCustomReactionChanges(t *testing.T) {
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "getCustomEmojiStickers") {
			t.Errorf("unexpected API request: %s", r.URL.Path)
		}
		apiOK(t, w, `[{"custom_emoji_id":"premium","emoji":"👍"}]`)
	})
	custom := models.ReactionType{
		Type:                    models.ReactionTypeTypeCustomEmoji,
		ReactionTypeCustomEmoji: &models.ReactionTypeCustomEmoji{CustomEmojiID: "premium"},
	}
	ordinary := models.ReactionType{
		Type:              models.ReactionTypeTypeEmoji,
		ReactionTypeEmoji: &models.ReactionTypeEmoji{Emoji: "👍"},
	}
	event := &models.MessageReactionUpdated{
		Chat: models.Chat{ID: -100}, MessageID: 8, User: &models.User{ID: 7},
		NewReaction: []models.ReactionType{custom},
	}
	if err := c.reactions(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	event.OldReaction, event.NewReaction = event.NewReaction, []models.ReactionType{ordinary}
	if err := c.reactions(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	event.OldReaction, event.NewReaction = []models.ReactionType{custom}, nil
	if err := c.reactions(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(h.reactions) != 2 || h.reactions[0].Emoji != "👍" || !h.reactions[0].Added || h.reactions[1].Emoji != "👍" || h.reactions[1].Added {
		t.Fatalf("reaction changes: %+v", h.reactions)
	}
}

func TestReactionChangesAndLinkCallback(t *testing.T) {
	acknowledgements := 0
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "getChatMember") {
			apiOK(t, w, `{"status":"administrator","user":{"id":1}}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "answerCallbackQuery") {
			acknowledgements++
			apiOK(t, w, `true`)
			return
		}
		apiOK(t, w, `{"message_id":10}`)
	})

	emoji := func(s string) models.ReactionType {
		return models.ReactionType{Type: models.ReactionTypeTypeEmoji, ReactionTypeEmoji: &models.ReactionTypeEmoji{Emoji: s}}
	}
	c.update(context.Background(), c.bot, &models.Update{
		MessageReaction: &models.MessageReactionUpdated{
			Chat:        models.Chat{ID: -100},
			MessageID:   8,
			User:        &models.User{ID: 7},
			OldReaction: []models.ReactionType{emoji("👍")},
			NewReaction: []models.ReactionType{emoji("❤")},
		},
	})
	if len(h.reactions) != 2 || h.reactions[0].Added || !h.reactions[1].Added {
		t.Fatalf("reaction changes: %+v", h.reactions)
	}
	c.update(context.Background(), c.bot, &models.Update{
		MessageReaction: &models.MessageReactionUpdated{
			Chat: models.Chat{ID: -100}, MessageID: 8, ActorChat: &models.Chat{ID: -200},
			NewReaction: []models.ReactionType{emoji("👍")},
		},
	})
	if len(h.reactions) != 3 || h.reactions[2].UserID != "chat:-200" || !h.reactions[2].Added {
		t.Fatalf("anonymous chat reaction: %+v", h.reactions)
	}

	c.update(context.Background(), c.bot, &models.Update{
		Message: &models.Message{
			ID:             9,
			Chat:           models.Chat{ID: -100},
			From:           &models.User{ID: 1},
			Text:           "/link Ada",
			ReplyToMessage: &models.Message{ID: 8, From: &models.User{ID: 7}},
		},
	})
	if len(h.messages) != 0 {
		t.Fatal("control message was bridged")
	}

	q := &models.CallbackQuery{
		ID:      "query",
		From:    models.User{ID: 1},
		Data:    "link:1:a:0",
		Message: models.MaybeInaccessibleMessage{Message: &models.Message{Chat: models.Chat{ID: -100}}},
	}
	if err := c.callback(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	q.Data = "unknown"
	if err := c.callback(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if acknowledgements != 2 || strings.Join(h.linked, ",") != "7,discord-7" {
		t.Fatalf("acks=%d linked=%v", acknowledgements, h.linked)
	}
}

func TestEditedMessageIsForwarded(t *testing.T) {
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected API request %s", r.URL.Path)
	})
	message := &models.Message{
		ID: 17, Chat: models.Chat{ID: -100}, From: &models.User{ID: 7, FirstName: "Alice"}, Text: "edited",
	}
	c.update(context.Background(), c.bot, &models.Update{EditedMessage: message})
	if len(h.messages) != 1 || h.messages[0].ID != "17" || h.messages[0].Text.Value != "edited" {
		t.Fatalf("edited messages: %+v", h.messages)
	}
}

func TestPinServiceMessageBeforeSenderFilter(t *testing.T) {
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request: %s", r.URL.Path) })
	for _, chat := range []int64{-100, -200} {
		c.update(context.Background(), c.bot, &models.Update{Message: &models.Message{
			Chat: models.Chat{ID: chat}, From: &models.User{IsBot: true},
			PinnedMessage: &models.MaybeInaccessibleMessage{InaccessibleMessage: &models.InaccessibleMessage{MessageID: 42}},
		}})
	}
	if len(h.pins) != 1 || h.pins[0] != "42" || len(h.messages) != 0 {
		t.Fatalf("pins=%v messages=%v", h.pins, h.messages)
	}
}
