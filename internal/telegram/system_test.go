package telegram

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestSystemEvents(t *testing.T) {
	ada := models.User{ID: 7, FirstName: "Ada"}
	bob := models.User{ID: 8, FirstName: "Bob"}
	for _, tc := range []struct {
		name    string
		message models.Message
		want    string
	}{
		{"join", models.Message{From: &ada, NewChatMembers: []models.User{ada}}, "joined the chat"},
		{"add", models.Message{From: &ada, NewChatMembers: []models.User{bob}}, "added Bob"},
		{"leave", models.Message{From: &ada, LeftChatMember: &ada}, "left the chat"},
		{"remove", models.Message{From: &ada, LeftChatMember: &bob}, "removed Bob"},
		{"pin", models.Message{PinnedMessage: &models.MaybeInaccessibleMessage{}}, "pinned"},
		{"title", models.Message{NewChatTitle: "New"}, "New"},
		{"photo", models.Message{NewChatPhoto: []models.PhotoSize{{}}}, "changed the chat photo"},
		{"photo removed", models.Message{DeleteChatPhoto: true}, "removed the chat photo"},
		{"migrate to", models.Message{MigrateToChatID: -123}, "-123"},
		{"migrate from", models.Message{MigrateFromChatID: -456}, "-456"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.message.ID = 42
			got, ok := systemMessage(&tc.message)
			if !ok || got.ID != "42" || got.Author.Name == "" || !strings.Contains(got.Text.Value, tc.want) {
				t.Fatalf("event: %+v, %v", got, ok)
			}
		})
	}
}

func TestBotCatchupSystemEventFiltering(t *testing.T) {
	c, h := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") })
	bot := &models.User{ID: 2, FirstName: "Bot", IsBot: true}
	for _, m := range []*models.Message{
		{ID: 1, Chat: models.Chat{ID: -100}, From: bot, NewChatTitle: "forwarded"},
		{ID: 2, Chat: models.Chat{ID: -100}, NewChatTitle: "forwarded"},
		{ID: 3, Chat: models.Chat{ID: -101}, NewChatTitle: "wrong chat"},
		{ID: 4, Chat: models.Chat{ID: -100}, IsTopicMessage: true, NewChatTitle: "topic"},
		{ID: 5, Chat: models.Chat{ID: -100}, MessageThreadID: 5, NewChatTitle: "thread"},
		{ID: 6, Chat: models.Chat{ID: -100}, From: bot, Text: "ordinary"},
	} {
		c.update(context.Background(), c.bot, &models.Update{Message: m})
	}
	if len(h.messages) != 3 || h.messages[0].ID != "1" || h.messages[1].ID != "2" || h.messages[2].ID != "6" {
		t.Fatalf("messages: %+v", h.messages)
	}
}
