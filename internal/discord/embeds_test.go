package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestBotRichEmbedsForwarded(t *testing.T) {
	for _, tc := range []struct {
		name             string
		author           *discordgo.User
		webhook, content string
		embeds           []*discordgo.MessageEmbed
		want             string
	}{
		{
			name: "embed only bot", author: &discordgo.User{ID: "bot", Bot: true},
			embeds: []*discordgo.MessageEmbed{{
				Type: "rich", Title: "Title", Description: "Description", URL: "https://example.com/article",
				Author:    &discordgo.MessageEmbedAuthor{Name: "Author", URL: "https://example.com/author"},
				Fields:    []*discordgo.MessageEmbedField{{Name: "Field", Value: "Value"}},
				Image:     &discordgo.MessageEmbedImage{URL: "https://example.com/image.png"},
				Thumbnail: &discordgo.MessageEmbedThumbnail{URL: "http://127.0.0.1/private"},
				Footer:    &discordgo.MessageEmbedFooter{Text: "Footer"},
			}},
			want: "Author\nhttps://example.com/author\nTitle\nhttps://example.com/article\nDescription\nField\nValue\nhttps://example.com/image.png\nhttp://127.0.0.1/private\nFooter",
		},
		{name: "webhook", author: &discordgo.User{ID: "hook"}, webhook: "other", embeds: []*discordgo.MessageEmbed{{Type: "rich", Description: "Webhook embed"}}, want: "Webhook embed"},
		{name: "bot text and automatic preview", author: &discordgo.User{ID: "bot", Bot: true}, content: "See https://example.com", embeds: []*discordgo.MessageEmbed{{Type: "link", Title: "Preview", URL: "https://example.com"}}, want: "See https://example.com"},
		{name: "human rich embed", author: &discordgo.User{ID: "human"}, content: "human", embeds: []*discordgo.MessageEmbed{{Type: "rich", Description: "ignored"}}, want: "human"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New("token", "guild", "channel", 100)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			queue := newEventQueue(ctx)
			h := &messageHandler{}
			c.queueMessage(ctx, h, queue, newGIFLookups(time.Millisecond), &discordgo.Message{
				ID: "42", GuildID: "guild", ChannelID: "channel", Author: tc.author,
				WebhookID: tc.webhook, Content: tc.content, Embeds: tc.embeds,
			})

			done := make(chan struct{})
			queue.enqueue(ctx, func() { close(done) })
			<-done
			cancel()
			<-queue.done

			if h.message.ID != "42" || h.message.Text.Value != tc.want {
				t.Fatalf("message: %+v", h.message)
			}
		})
	}
}

func TestEmbedOnlyGatewayEdits(t *testing.T) {
	c, err := New("token", "guild", "channel", 100)
	if err != nil {
		t.Fatal(err)
	}

	c.session.State.User = &discordgo.User{ID: "own"}
	c.webhookID = "own-hook"
	var full *discordgo.Message
	c.session.Client = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		data, err := json.Marshal(full)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(string(data))),
		}, nil
	})}

	for _, tc := range []struct {
		name    string
		author  *discordgo.User
		webhook string
		embeds  []*discordgo.MessageEmbed
		want    string
		forward bool
	}{
		{"bot update", &discordgo.User{ID: "bot", Bot: true}, "", []*discordgo.MessageEmbed{{Type: "rich", Description: "updated"}}, "updated", true},
		{"bot removal", &discordgo.User{ID: "bot", Bot: true}, "", nil, "", true},
		{"webhook update", &discordgo.User{ID: "hook", Bot: true}, "other-hook", []*discordgo.MessageEmbed{{Type: "rich", Description: "updated"}}, "updated", true},
		{"human GIF preview", &discordgo.User{ID: "human"}, "", []*discordgo.MessageEmbed{gifEmbed()}, "", false},
		{"own bot", &discordgo.User{ID: "own", Bot: true}, "", nil, "", false},
		{"own webhook", &discordgo.User{ID: "hook", Bot: true}, "own-hook", nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			full = &discordgo.Message{ID: "42", Author: tc.author, WebhookID: tc.webhook, Embeds: tc.embeds}

			ctx, cancel := context.WithCancel(context.Background())
			queue := newEventQueue(ctx)
			h := &editHandler{}
			message := &discordgo.Message{ID: "42", Embeds: tc.embeds}
			event := &discordgo.Event{RawData: []byte(`{"id":"42","embeds":[]}`)}
			c.queueEdit(ctx, h, queue, newGIFLookups(time.Millisecond), event, message)

			done := make(chan struct{})
			queue.enqueue(ctx, func() { close(done) })
			<-done
			cancel()
			<-queue.done

			if (len(h.edits) != 0) != tc.forward {
				t.Fatalf("edits: %+v", h.edits)
			}
			if tc.forward && (h.edits[0].Text.Value != tc.want || !h.edits[0].PreserveAttachments) {
				t.Fatalf("edit: %+v", h.edits[0])
			}
		})
	}
}
