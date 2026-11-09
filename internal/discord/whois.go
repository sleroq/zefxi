package discord

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/bwmarrin/discordgo"
	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/richtext"
)

func (c *Client) handleWhois(ctx context.Context, h bridge.Handler, e *discordgo.MessageCreate) {
	if e.MessageReference == nil || e.MessageReference.MessageID == "" {
		c.replyWhois(ctx, e.ID, "Reply to a message with /whois to identify its author.")
		return
	}

	target := e.ReferencedMessage
	if target == nil {
		var err error
		target, err = c.session.ChannelMessage(c.channelID, e.MessageReference.MessageID, discordgo.WithContext(ctx))
		if err != nil {
			slog.Error("discord whois message fetch failed", "message", e.MessageReference.MessageID, "error", err)
			c.replyWhois(ctx, e.ID, "Could not retrieve the referenced message.")
			return
		}
	}
	var fallback bridge.User
	if target.Author != nil && !target.Author.Bot && target.WebhookID == "" {
		fallback = memberUser(target.Author, target.Member, c.guildID)
	}
	identity, err := h.Whois(ctx, bridge.Discord, e.MessageReference.MessageID, fallback)
	if err != nil {
		slog.Error("discord whois handler failed", "error", err)
		c.replyWhois(ctx, e.ID, "Could not identify the message author. Please try again.")
		return
	}
	c.replyWhois(ctx, e.ID, formatWhois(identity))
}

func (c *Client) replyWhois(ctx context.Context, messageID, content string) {
	_, err := c.session.ChannelMessageSendComplex(c.channelID, &discordgo.MessageSend{
		Content:         content,
		Flags:           discordgo.MessageFlagsSuppressEmbeds,
		Reference:       &discordgo.MessageReference{MessageID: messageID, ChannelID: c.channelID, GuildID: c.guildID},
		AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}},
	}, discordgo.WithContext(ctx))
	if err != nil {
		slog.Error("discord whois reply failed", "error", err)
	}
}

func formatWhois(identity bridge.Identity) string {
	if identity.Discord.ID == "" && identity.Telegram.ID == "" {
		return "Could not identify the author of that message."
	}
	discord := "Not linked"
	if identity.Discord.ID != "" {
		profile := "https://discord.com/users/" + url.PathEscape(identity.Discord.ID)
		discord = fmt.Sprintf("[%s](%s)", whoisName(identity.Discord), profile)
	}
	telegram := "Not linked"
	if identity.Telegram.ID != "" {
		telegram = whoisName(identity.Telegram) + " (no public Telegram username)"
		if identity.Telegram.Username != "" {
			profile := "https://t.me/" + url.PathEscape(identity.Telegram.Username)
			telegram = fmt.Sprintf("[%s](%s)", whoisName(identity.Telegram), profile)
		}
	}
	return "Discord: " + discord + "\nTelegram: " + telegram
}

func whoisName(user bridge.User) string {
	name := user.Name
	if name == "" {
		name = user.Username
	}
	if name == "" {
		name = user.ID
	}
	return richtext.ToDiscord(richtext.Text{Value: name}, nil)
}
