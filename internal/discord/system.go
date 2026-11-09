package discord

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/richtext"
)

func (c *Client) systemMessage(m *discordgo.Message) (bridge.Message, bool) {
	if m.GuildID != c.guildID || m.ChannelID != c.channelID || m.WebhookID != "" {
		return bridge.Message{}, false
	}
	var text string
	switch m.Type {
	case discordgo.MessageTypeGuildMemberJoin:
		text = "joined the server"
	case discordgo.MessageTypeChannelPinnedMessage:
		text = "pinned a message"
	default:
		return bridge.Message{}, false
	}
	author := bridge.User{Name: "Discord"}
	if m.Author != nil {
		author = memberUser(m.Author, m.Member, c.guildID)
	}
	return bridge.Message{ID: m.ID, Author: author, Text: richtext.Text{Value: text}}, true
}

func (c *Client) memberLeave(e *discordgo.GuildMemberRemove) (bridge.Message, bool) {
	if e.Member == nil || e.GuildID != c.guildID || e.User == nil {
		return bridge.Message{}, false
	}
	return bridge.Message{
		Author: memberUser(e.User, e.Member, c.guildID),
		Text:   richtext.Text{Value: "left or was removed from the server"},
	}, true
}

func (c *Client) channelRename(e *discordgo.ChannelUpdate) (bridge.Message, bool) {
	if e.Channel == nil || e.GuildID != c.guildID || e.ID != c.channelID || e.BeforeUpdate == nil || e.Name == e.BeforeUpdate.Name {
		return bridge.Message{}, false
	}
	return bridge.Message{
		Author: bridge.User{Name: "Discord"},
		Text:   richtext.Text{Value: fmt.Sprintf("channel renamed from %q to %q", e.BeforeUpdate.Name, e.Name)},
	}, true
}

func forwardSystem(ctx context.Context, h bridge.Handler, message bridge.Message) {
	if err := h.HandleMessage(ctx, bridge.Discord, message); err != nil {
		slog.Error("discord system event handler failed", "error", err)
	}
}
