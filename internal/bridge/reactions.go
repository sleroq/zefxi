package bridge

import (
	"context"
	"errors"
	"strings"
)

// Telegram bots can set only one reaction per message, from Telegram's emoji set.
var telegramReactions = func() map[string]string {
	emojis := strings.Fields(`
		👍 👎 ❤️ 🔥 🥰 👏 😁 🤔 🤯 😱 🤬 😢 🎉 🤩 🤮 💩 🙏 👌 🕊 🤡
		🥱 🥴 😍 🐳 ❤️‍🔥 🌚 🌭 💯 🤣 ⚡ 🍌 🏆 💔 🤨 😐 🍓 🍾 💋 🖕 😈
		😴 😭 🤓 👻 👨‍💻 👀 🎃 🙈 😇 😨 🤝 ✍️ 🤗 🫡 🎅 🎄 ☃️ 💅 🤪 🗿
		🆒 💘 🙉 🦄 😘 💊 🙊 😎 👾 🤷‍♂️ 🤷 🤷‍♀️ 😡
	`)
	result := make(map[string]string, len(emojis))
	for _, emoji := range emojis {
		result[strings.ReplaceAll(emoji, "\ufe0f", "")] = emoji
	}
	return result
}()

func (b *Bridge) HandleReaction(ctx context.Context, source Platform, reaction Reaction) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	link, found, err := b.store.MessageLink(ctx, source, reaction.MessageID)
	if err != nil || !found {
		return err
	}
	// Discord reactions are forwarded to the first Telegram part.
	canonical, _, err := b.store.MessageLink(ctx, Discord, link.DiscordID)
	if err != nil {
		return err
	}
	beforeDiscord, err := b.store.BridgedReactionCounts(ctx, Telegram, link.DiscordID)
	if err != nil {
		return err
	}
	canonicalizeTelegramHearts(beforeDiscord)
	beforeTelegram, err := b.store.BridgedReactionCounts(ctx, Discord, canonical.TelegramID)
	if err != nil {
		return err
	}
	sourceMessageID := reaction.MessageID
	if source == Telegram {
		if reaction.Emoji == "❤" {
			reaction.Emoji = "❤️"
		}
		reaction.MessageID = link.DiscordID
	} else {
		reaction.MessageID = link.TelegramID
	}
	reaction.UserID = sourceMessageID + ":" + reaction.UserID
	if source == Telegram && reaction.Emoji == "❤️" && !reaction.Added {
		legacy := reaction
		legacy.Emoji = "❤"
		if _, err := b.store.ChangeReaction(ctx, source, legacy); err != nil {
			return err
		}
	}
	if _, err := b.store.ChangeReaction(ctx, source, reaction); err != nil {
		return err
	}
	afterDiscord, err := b.store.BridgedReactionCounts(ctx, Telegram, link.DiscordID)
	if err != nil {
		return err
	}
	canonicalizeTelegramHearts(afterDiscord)
	afterTelegram, err := b.store.BridgedReactionCounts(ctx, Discord, canonical.TelegramID)
	if err != nil {
		return err
	}

	var deliveryErr error
	emojis := make(map[string]bool)
	for _, count := range append(beforeDiscord, afterDiscord...) {
		emojis[count.Emoji] = true
	}
	if source == Telegram {
		emojis[reaction.Emoji] = true
	}
	prefix := string(Telegram) + ":" + link.DiscordID + ":"
	for key := range b.failedReactions {
		if emoji, ok := strings.CutPrefix(key, prefix); ok {
			emojis[emoji] = true
		}
	}
	for emoji := range emojis {
		key := string(Telegram) + ":" + link.DiscordID + ":" + emoji
		had, has := hasReaction(beforeDiscord, emoji), hasReaction(afterDiscord, emoji)
		if had != has || b.failedReactions[key] {
			err := b.discord.SetReaction(ctx, link.DiscordID, emoji, has)
			if err != nil {
				b.failedReactions[key] = true
			} else {
				delete(b.failedReactions, key)
			}
			deliveryErr = errors.Join(deliveryErr, err)
		}
	}
	key := string(Discord) + ":" + canonical.TelegramID
	previous, current := telegramReaction(beforeTelegram), telegramReaction(afterTelegram)
	if previous != current || b.failedReactions[key] {
		err := b.telegram.SetReaction(ctx, canonical.TelegramID, current, current != "")
		if err != nil {
			b.failedReactions[key] = true
		} else {
			delete(b.failedReactions, key)
		}
		deliveryErr = errors.Join(deliveryErr, err)
	}
	return deliveryErr
}

func canonicalizeTelegramHearts(counts []ReactionCount) {
	for i := range counts {
		if counts[i].Emoji == "❤" {
			counts[i].Emoji = "❤️"
		}
	}
}

func hasReaction(counts []ReactionCount, emoji string) bool {
	for _, count := range counts {
		if count.Emoji == emoji && count.Count > 0 {
			return true
		}
	}
	return false
}

func telegramReaction(counts []ReactionCount) string {
	for _, count := range counts {
		if emoji := telegramReactions[strings.ReplaceAll(count.Emoji, "\ufe0f", "")]; count.Count > 0 && emoji != "" {
			return emoji
		}
	}
	return ""
}

// ReactionSummary reads Discord directly so /reacts also shows pre-existing reactions.
func (b *Bridge) ReactionSummary(ctx context.Context, telegramMessageID string) (ReactionSummary, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	link, found, err := b.store.MessageLink(ctx, Telegram, telegramMessageID)
	if err != nil || !found {
		return ReactionSummary{}, err
	}
	reactions, err := b.discord.ReactionUsers(ctx, link.DiscordID)
	if err != nil {
		return ReactionSummary{}, err
	}
	return ReactionSummary{LinkFound: true, Reactions: reactions}, nil
}
