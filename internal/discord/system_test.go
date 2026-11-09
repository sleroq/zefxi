package discord

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestSystemMessage(t *testing.T) {
	c := &Client{guildID: "guild", channelID: "channel"}
	for _, typ := range []discordgo.MessageType{discordgo.MessageTypeGuildMemberJoin, discordgo.MessageTypeChannelPinnedMessage} {
		m := &discordgo.Message{
			ID: "message", GuildID: "guild", ChannelID: "channel", Type: typ,
			Author: &discordgo.User{ID: "user", Username: "Ada", Bot: true},
		}
		got, ok := c.systemMessage(m)
		if !ok || got.ID != m.ID || got.Author.ID != "user" || got.Text.Value == "" {
			t.Fatalf("event: %+v %v", got, ok)
		}
		m.ChannelID = "thread"
		if _, ok := c.systemMessage(m); ok {
			t.Fatal("forwarded thread")
		}
		m.ChannelID, m.GuildID = "channel", "other"
		if _, ok := c.systemMessage(m); ok {
			t.Fatal("forwarded other guild")
		}
	}
	ordinary := &discordgo.Message{GuildID: "guild", ChannelID: "channel", Type: discordgo.MessageTypeDefault}
	if _, ok := c.systemMessage(ordinary); ok {
		t.Fatal("ordinary message treated as system")
	}
}

func TestGatewaySystemEvents(t *testing.T) {
	c := &Client{guildID: "guild", channelID: "channel"}
	leave := &discordgo.GuildMemberRemove{Member: &discordgo.Member{
		GuildID: "guild", User: &discordgo.User{ID: "user", Username: "Ada"},
	}}
	msg, ok := c.memberLeave(leave)
	if !ok || msg.ID != "" || msg.Author.ID != "user" {
		t.Fatalf("leave: %+v %v", msg, ok)
	}
	leave.GuildID = "other"
	if _, ok := c.memberLeave(leave); ok {
		t.Fatal("forwarded other guild leave")
	}
	update := &discordgo.ChannelUpdate{
		Channel:      &discordgo.Channel{ID: "channel", GuildID: "guild", Name: "new"},
		BeforeUpdate: &discordgo.Channel{Name: "old"},
	}
	msg, ok = c.channelRename(update)
	if !ok || msg.ID != "" || !strings.Contains(msg.Text.Value, "old") || !strings.Contains(msg.Text.Value, "new") {
		t.Fatalf("rename: %+v %v", msg, ok)
	}
	update.ID = "thread"
	if _, ok := c.channelRename(update); ok {
		t.Fatal("forwarded thread rename")
	}
	update.ID = "channel"
	update.BeforeUpdate.Name = "new"
	if _, ok := c.channelRename(update); ok {
		t.Fatal("forwarded non-rename")
	}
}
