package telegram

import (
	"context"
	"reflect"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/sleroq/zefxi/internal/bridge"
)

type userEventRecorder struct {
	bridge.Handler
	events   []string
	messages []bridge.Message
}

func (r *userEventRecorder) HandleMessage(_ context.Context, _ bridge.Platform, m bridge.Message) error {
	r.events = append(r.events, "message")
	r.messages = append(r.messages, m)
	return nil
}
func (r *userEventRecorder) HandleEdit(_ context.Context, _ bridge.Platform, m bridge.Message) error {
	r.events = append(r.events, "edit")
	r.messages = append(r.messages, m)
	return nil
}
func (r *userEventRecorder) HandleTelegramDelete(_ context.Context, id string) error {
	r.events = append(r.events, "delete:"+id)
	return nil
}
func (r *userEventRecorder) HandleTelegramPin(_ context.Context, id string, pinned bool) error {
	if pinned {
		r.events = append(r.events, "pin:"+id)
	} else {
		r.events = append(r.events, "unpin:"+id)
	}
	return nil
}
func TestUserUpdates(t *testing.T) {
	c, _ := testClient(t, nil)
	c.chatID = -1000000000042
	h := &userEventRecorder{}
	d := tg.NewUpdateDispatcher()
	c.registerUserUpdates(d, nil, nil, h)
	msg := func(id int, from int64, text string) *tg.Message {
		return &tg.Message{ID: id, PeerID: &tg.PeerChannel{ChannelID: 42}, FromID: &tg.PeerUser{UserID: from}, Message: text}
	}
	rich := msg(1, 2, "😀 hello")
	rich.Entities = []tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 3, Length: 5}}
	rich.ReplyTo = &tg.MessageReplyHeader{ReplyToMsgID: 9}
	updates := &tg.Updates{Users: []tg.UserClass{&tg.User{ID: 2, FirstName: "Ada"}, &tg.User{ID: 4, FirstName: "Other bot", Bot: true}}, Updates: []tg.UpdateClass{
		&tg.UpdateNewChannelMessage{Message: rich}, &tg.UpdateEditChannelMessage{Message: msg(1, 2, "edited")},
		&tg.UpdateNewChannelMessage{Message: msg(2, 123, "bot")}, &tg.UpdateNewChannelMessage{Message: msg(5, 4, "other bot")}, &tg.UpdateNewChannelMessage{Message: msg(3, 3, "/command@mybot")},

		&tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 4, PeerID: &tg.PeerChat{ChatID: 42}, Message: "wrong"}},
		&tg.UpdateNewChannelMessage{Message: &tg.MessageService{ID: 6, PeerID: &tg.PeerChannel{ChannelID: 42}, ReplyTo: &tg.MessageReplyHeader{ForumTopic: true}, Action: &tg.MessageActionChatEditTitle{Title: "topic"}}},
		&tg.UpdatePinnedChannelMessages{ChannelID: 42, Messages: []int{1, 2}, Pinned: true}, &tg.UpdatePinnedChannelMessages{ChannelID: 42, Messages: []int{1}},
		&tg.UpdateDeleteChannelMessages{ChannelID: 99, Messages: []int{4}}, &tg.UpdateDeleteChannelMessages{ChannelID: 42, Messages: []int{1, 2}},
	}}
	if err := d.Handle(context.Background(), updates); err != nil {
		t.Fatal(err)
	}
	want := []string{"message", "edit", "message", "pin:1", "pin:2", "unpin:1", "delete:1", "delete:2"}
	if !reflect.DeepEqual(h.events, want) {
		t.Fatalf("events %v", h.events)
	}
	m := h.messages[0]
	if m.Author.Name != "Ada" || m.ReplyTo != "9" || len(m.Text.Entities) != 1 || m.Text.Entities[0].Start != 5 || m.Text.Entities[0].End != 10 {
		t.Fatalf("message %+v", m)
	}
}
func TestUserForwardsOtherBotsAndCommands(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		sender     int64
		edit, want bool
	}{
		{"bridge command", "/link", 2, false, false},
		{"addressed command", "/whois@MyBot", 2, false, false},
		{"unknown addressed command", "/unknown@mybot", 2, false, false},
		{"other bot command", "/link@other_bot Ada", 2, false, true},
		{"unqualified other command", "/dance", 2, false, true},
		{"other bot message", "hello", 4, false, true},
		{"other bot command text", "/whois", 4, false, true},
		{"other bot explicitly addressing bridge", "/unknown@mybot", 4, false, false},
		{"own bot reply", "bridge response", 123, false, false},
		{"edited other command", "/link@other_bot Ada", 2, true, true},
		{"edited bridge command", "/whois@mybot", 2, true, false},
		{"edited other bot response", "edited", 4, true, true},
		{"edited own bot reply", "edited", 123, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, nil)
			c.chatID = -1000000000042
			h := &userEventRecorder{}
			d := tg.NewUpdateDispatcher()
			c.registerUserUpdates(d, nil, nil, h)

			message := &tg.Message{
				ID: 1, PeerID: &tg.PeerChannel{ChannelID: 42},
				FromID: &tg.PeerUser{UserID: tc.sender}, Message: tc.text,
			}
			var event tg.UpdateClass = &tg.UpdateNewChannelMessage{Message: message}
			if tc.edit {
				event = &tg.UpdateEditChannelMessage{Message: message}
			}
			updates := &tg.Updates{
				Users:   []tg.UserClass{&tg.User{ID: 2}, &tg.User{ID: 4, Bot: true}},
				Updates: []tg.UpdateClass{event},
			}
			if err := d.Handle(context.Background(), updates); err != nil {
				t.Fatal(err)
			}

			if (len(h.messages) == 1) != tc.want {
				t.Fatalf("events=%v messages=%+v", h.events, h.messages)
			}
		})
	}
}

func TestUserForwardsMediaCaptionThatLooksLikeCommand(t *testing.T) {
	c, _ := testClient(t, nil)
	c.chatID = -1000000000042
	h := &userEventRecorder{}
	d := tg.NewUpdateDispatcher()
	c.registerUserUpdates(d, nil, nil, h)
	message := &tg.Message{
		ID: 1, PeerID: &tg.PeerChannel{ChannelID: 42}, FromID: &tg.PeerUser{UserID: 2},
		Message: "/whois@mybot", Media: &tg.MessageMediaPhoto{Photo: &tg.PhotoEmpty{}},
	}
	updates := &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewChannelMessage{Message: message}}}
	if err := d.Handle(context.Background(), updates); err != nil {
		t.Fatal(err)
	}
	if len(h.messages) != 1 || h.messages[0].Text.Value != message.Message {
		t.Fatalf("messages=%+v", h.messages)
	}
}

func TestUserIgnoresNonChannelUpdates(t *testing.T) {
	c, _ := testClient(t, nil)
	c.chatID = -1000000000042
	h := &userEventRecorder{}
	d := tg.NewUpdateDispatcher()
	c.registerUserUpdates(d, nil, nil, h)
	updates := &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateNewMessage{Message: &tg.Message{ID: 1, PeerID: &tg.PeerChat{ChatID: 42}, FromID: &tg.PeerUser{UserID: 77}, Message: "hello"}},
		&tg.UpdatePinnedMessages{Peer: &tg.PeerChat{ChatID: 42}, Messages: []int{1}, Pinned: true},
		&tg.UpdateDeleteMessages{Messages: []int{1}},
	}}
	if err := d.Handle(context.Background(), updates); err != nil {
		t.Fatal(err)
	}
	if len(h.events) != 0 {
		t.Fatalf("events %v", h.events)
	}
}
