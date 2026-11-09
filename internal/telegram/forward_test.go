package telegram

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/gotd/td/tg"
	"github.com/sleroq/zefxi/internal/bridge"
)

func TestBotForwardSource(t *testing.T) {
	cases := []struct {
		origin *models.MessageOrigin
		want   *bridge.ForwardSource
	}{
		{&models.MessageOrigin{Type: models.MessageOriginTypeUser, MessageOriginUser: &models.MessageOriginUser{SenderUser: models.User{ID: 42, FirstName: "Alice", Username: "alice"}}}, &bridge.ForwardSource{User: bridge.User{ID: "42", Name: "Alice", Username: "alice"}}},
		{&models.MessageOrigin{Type: models.MessageOriginTypeHiddenUser, MessageOriginHiddenUser: &models.MessageOriginHiddenUser{SenderUserName: "Hidden"}}, &bridge.ForwardSource{Name: "Hidden"}},
		{&models.MessageOrigin{Type: models.MessageOriginTypeChat, MessageOriginChat: &models.MessageOriginChat{SenderChat: models.Chat{Title: "Group", Username: "group"}}}, &bridge.ForwardSource{Name: "Group"}},
		{&models.MessageOrigin{Type: models.MessageOriginTypeChannel, MessageOriginChannel: &models.MessageOriginChannel{Chat: models.Chat{Title: "News", Username: "news"}}}, &bridge.ForwardSource{Name: "News", URL: "https://t.me/news"}},
		{&models.MessageOrigin{Type: models.MessageOriginTypeChannel, MessageOriginChannel: &models.MessageOriginChannel{Chat: models.Chat{Title: "Private"}}}, &bridge.ForwardSource{Name: "Private"}},
	}
	for _, tc := range cases {
		if got := botForwardSource(tc.origin); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("got %+v want %+v", got, tc.want)
		}
	}
}

func TestUserForwardSource(t *testing.T) {
	entities := tg.Entities{
		Users:    map[int64]*tg.User{42: {ID: 42, FirstName: "Alice", Username: "alice"}},
		Chats:    map[int64]*tg.Chat{1: {ID: 1, Title: "Group"}},
		Channels: map[int64]*tg.Channel{2: {ID: 2, Title: "News", Username: "news"}},
	}
	cases := []struct {
		header *tg.MessageFwdHeader
		want   *bridge.ForwardSource
	}{
		{&tg.MessageFwdHeader{FromID: &tg.PeerUser{UserID: 42}}, &bridge.ForwardSource{User: bridge.User{ID: "42", Name: "Alice", Username: "alice"}}},
		{&tg.MessageFwdHeader{FromName: "Hidden"}, &bridge.ForwardSource{Name: "Hidden"}},
		{&tg.MessageFwdHeader{FromID: &tg.PeerChat{ChatID: 1}}, &bridge.ForwardSource{Name: "Group"}},
		{&tg.MessageFwdHeader{FromID: &tg.PeerChannel{ChannelID: 2}}, &bridge.ForwardSource{Name: "News", URL: "https://t.me/news"}},
	}
	for _, tc := range cases {
		if got := userForwardSource(context.Background(), nil, entities, tc.header); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("got %+v want %+v", got, tc.want)
		}
	}
	peers := testPeerStorage{user: &tg.User{ID: 42, FirstName: "Cached"}}
	header := &tg.MessageFwdHeader{FromID: &tg.PeerUser{UserID: 42}}
	got := userForwardSource(context.Background(), peers, tg.Entities{}, header)
	if got.User.Name != "Cached" {
		t.Fatalf("cached: %+v", got)
	}
}
