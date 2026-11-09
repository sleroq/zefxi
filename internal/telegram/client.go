package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/sleroq/zefxi/internal/bridge"
)

// Client transports bridge messages to one Telegram group.
type Client struct {
	bot        *bot.Bot
	chatID     int64
	handler    bridge.Handler
	links      map[string]linkChoice
	nextChoice uint64
}
type linkChoice struct {
	messageID string
	fallback  bridge.User
	query     string
	requester int64
	locale    locale
	view      bridge.LinkView
}

var _ bridge.Messenger = (*Client)(nil)

func New(token string, chatID int64) (*Client, error) {
	c := &Client{chatID: chatID, links: make(map[string]linkChoice)}
	b, err := bot.New(token,
		bot.WithHTTPClient(time.Minute, &retryClient{client: &http.Client{Timeout: time.Minute}}),
		bot.WithAllowedUpdates(bot.AllowedUpdates{"message", "edited_message", "message_reaction", "callback_query"}),
		bot.WithWorkers(1), bot.WithNotAsyncHandlers(),
		bot.WithDefaultHandler(c.update),
		bot.WithErrorsHandler(func(err error) { slog.Error("Telegram polling failed", "error", err) }),
	)
	if err != nil {
		return nil, err
	}
	c.bot = b
	return c, nil
}

func (c *Client) Run(ctx context.Context, h bridge.Handler) error {
	c.handler = h
	c.bot.Start(ctx)
	return ctx.Err()
}

func (c *Client) update(ctx context.Context, _ *bot.Bot, u *models.Update) {
	var err error
	switch {
	case u.CallbackQuery != nil:
		err = c.callback(ctx, u.CallbackQuery)
	case u.Message != nil:
		m := u.Message
		if m.Chat.ID != c.chatID {
			return
		}
		if m.PinnedMessage != nil {
			err = c.pin(ctx, m.PinnedMessage)
			break
		}
		if event, ok := systemMessage(m); ok {
			if !m.IsTopicMessage && m.MessageThreadID == 0 {
				err = c.handler.HandleMessage(ctx, bridge.Telegram, event)
			}
			break
		}
		if m.From == nil || m.From.IsBot {
			return
		}
		var handled bool
		handled, err = c.command(ctx, m)
		if !handled && err == nil && !strings.HasPrefix(m.Text, "/") {
			err = c.message(ctx, m)
		}
	case u.EditedMessage != nil:
		m := u.EditedMessage
		if m.Chat.ID != c.chatID || m.From == nil || m.From.IsBot || strings.HasPrefix(m.Text, "/") {
			return
		}
		err = c.editedMessage(ctx, m)
	case u.MessageReaction != nil:
		r := u.MessageReaction
		if r.Chat.ID != c.chatID || (r.User == nil && r.ActorChat == nil) || (r.User != nil && (r.User.IsBot || r.User.ID == c.bot.ID())) {
			return
		}
		err = c.reactions(ctx, r)
	}

	if err != nil {
		slog.Error("Telegram update failed", "update_id", u.ID, "error", err)
	}
}

func (c *Client) pin(ctx context.Context, message *models.MaybeInaccessibleMessage) error {
	var id int
	if message.Message != nil {
		id = message.Message.ID
	} else if message.InaccessibleMessage != nil {
		id = message.InaccessibleMessage.MessageID
	}
	if id == 0 {
		return nil
	}
	return c.handler.HandleTelegramPin(ctx, strconv.Itoa(id))
}

func user(u *models.User) bridge.User {
	return bridge.User{ID: strconv.FormatInt(u.ID, 10), Username: u.Username, Name: joinName(u.FirstName, u.LastName)}
}
func joinName(first, last string) string {
	if last == "" {
		return first
	}
	return first + " " + last
}

func (c *Client) reactions(ctx context.Context, r *models.MessageReactionUpdated) error {
	actor := ""
	if r.User != nil {
		actor = strconv.FormatInt(r.User.ID, 10)
	} else {
		actor = "chat:" + strconv.FormatInt(r.ActorChat.ID, 10)
	}
	old, err := c.emojis(ctx, r.OldReaction)
	if err != nil {
		return err
	}
	next, err := c.emojis(ctx, r.NewReaction)
	if err != nil {
		return err
	}
	for _, change := range []struct {
		from, to map[string]bool
		added    bool
	}{{old, next, false}, {next, old, true}} {
		for emoji := range change.from {
			if !change.to[emoji] {
				reaction := bridge.Reaction{
					MessageID: strconv.Itoa(r.MessageID),
					UserID:    actor,
					Emoji:     emoji,
					Added:     change.added,
				}
				if err := c.handler.HandleReaction(ctx, bridge.Telegram, reaction); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (c *Client) emojis(ctx context.Context, rs []models.ReactionType) (map[string]bool, error) {
	result := make(map[string]bool)
	var customIDs []string
	for _, r := range rs {
		switch {
		case r.Type == models.ReactionTypeTypeEmoji && r.ReactionTypeEmoji != nil:
			result[r.ReactionTypeEmoji.Emoji] = true
		case r.Type == models.ReactionTypeTypeCustomEmoji && r.ReactionTypeCustomEmoji != nil:
			customIDs = append(customIDs, r.ReactionTypeCustomEmoji.CustomEmojiID)
		}
	}
	if len(customIDs) > 0 {
		stickers, err := c.bot.GetCustomEmojiStickers(ctx, &bot.GetCustomEmojiStickersParams{CustomEmojiIDs: customIDs})
		if err != nil {
			return nil, fmt.Errorf("resolve custom reaction emojis: %w", err)
		}
		for _, sticker := range stickers {
			if sticker.Emoji != "" {
				result[sticker.Emoji] = true
			}
		}
	}
	return result, nil
}
func (c *Client) SetReaction(ctx context.Context, id, emoji string, added bool) error {
	n, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("telegram message ID: %w", err)
	}
	// Keep this non-nil: the SDK's multipart encoder sends an explicit []
	// despite the JSON omitempty tag, clearing the bot's reaction.
	rs := []models.ReactionType{}
	if added {
		rs = []models.ReactionType{{
			Type:              models.ReactionTypeTypeEmoji,
			ReactionTypeEmoji: &models.ReactionTypeEmoji{Emoji: emoji},
		}}
	}
	_, err = c.bot.SetMessageReaction(ctx, &bot.SetMessageReactionParams{ChatID: c.chatID, MessageID: n, Reaction: rs})
	return err
}

func (c *Client) Delete(ctx context.Context, id string) error {
	n, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("telegram message ID: %w", err)
	}
	_, err = c.bot.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: c.chatID, MessageID: n})
	return err
}

func (c *Client) SetPin(ctx context.Context, id string, pinned bool) error {
	n, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("telegram message ID: %w", err)
	}
	if pinned {
		params := &bot.PinChatMessageParams{ChatID: c.chatID, MessageID: n, DisableNotification: true}
		_, err = c.bot.PinChatMessage(ctx, params)
	} else {
		_, err = c.bot.UnpinChatMessage(ctx, &bot.UnpinChatMessageParams{ChatID: c.chatID, MessageID: n})
	}
	return err
}
