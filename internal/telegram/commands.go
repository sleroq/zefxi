package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/sleroq/zefxi/internal/bridge"
)

func (c *Client) control(ctx context.Context, m *models.Message, text string, markup models.ReplyMarkup) error {
	_, err := c.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          c.chatID,
		Text:            text,
		ReplyParameters: &models.ReplyParameters{MessageID: m.ID},
		ReplyMarkup:     markup,
	})
	return err
}

func (c *Client) command(ctx context.Context, m *models.Message) (bool, error) {
	fields := strings.Fields(m.Text)
	if len(fields) == 0 {
		return false, nil
	}

	l := language(m.From.LanguageCode)
	command, _, _ := strings.Cut(fields[0], "@")
	switch command {
	case "/whois":
		return true, c.whois(ctx, m)
	case "/reacts":
		if m.ReplyToMessage == nil {
			return true, c.control(ctx, m, l.reactsReply, nil)
		}
		summary, err := c.handler.ReactionSummary(ctx, strconv.Itoa(m.ReplyToMessage.ID))
		if err != nil {
			return true, err
		}
		return true, c.control(ctx, m, renderReactions(summary, l), nil)
	case "/link":
		if m.ReplyToMessage == nil {
			text := l.linkReply
			return true, c.control(ctx, m, text, nil)
		}
		target := m.ReplyToMessage
		var fallback bridge.User
		if target.From != nil && !target.From.IsBot && target.SenderChat == nil {
			fallback = user(target.From)
		}
		query := strings.TrimSpace(strings.TrimPrefix(m.Text, fields[0]))
		view, err := c.handler.LinkView(ctx, strconv.Itoa(target.ID), fallback, query)
		if err != nil {
			return true, errors.Join(err, c.control(ctx, m, l.linkLoadError, nil))
		}
		if view.Author.ID == "" {
			return true, c.control(ctx, m, l.authorUnknown, nil)
		}
		c.nextChoice++
		key := "link:" + strconv.FormatUint(c.nextChoice, 10)
		c.links[key] = linkChoice{
			messageID: strconv.Itoa(target.ID), fallback: fallback, query: query,
			requester: m.From.ID, view: view, locale: l,
		}
		return true, c.control(ctx, m, linkText(view, l), linkKeyboard(key, view, 0, l))
	}
	return false, nil
}

func linkText(view bridge.LinkView, l locale) string {
	discord := view.Linked
	telegram := []bridge.User{view.Author}
	if view.Platform == bridge.Discord {
		discord = []bridge.User{view.Author}
		telegram = view.Linked
	}

	var text strings.Builder
	if len(discord) == 0 {
		text.WriteString(l.discordUser)
		text.WriteString(l.notLinked)
		text.WriteByte('\n')
	} else {
		for _, u := range discord {
			fmt.Fprintf(&text, "%s%s\n", l.discordUser, linkUserLabel(u))
		}
	}
	text.WriteByte('\n')
	text.WriteString(l.linkedTelegram)
	text.WriteByte('\n')
	if len(telegram) == 0 {
		text.WriteString(l.none)
		text.WriteByte('\n')
	} else {
		for _, u := range telegram {
			fmt.Fprintf(&text, "%s%s\n", l.telegramUser, linkUserLabel(u))
		}
	}
	text.WriteByte('\n')
	text.WriteString(l.selectAccount)
	if len(discord) == 1 {
		fmt.Fprintf(&text, l.linkedMessages,
			linkUserLabel(discord[0]))
	}
	return text.String()
}

func linkUserLabel(u bridge.User) string {
	identifier := u.ID
	if u.Username != "" {
		identifier = "@" + u.Username
	}
	if u.Name == "" {
		return identifier
	}
	return u.Name + " (" + identifier + ")"
}

const linkPageSize = 5

func linkKeyboard(key string, view bridge.LinkView, page int, l locale) *models.InlineKeyboardMarkup {
	total := len(view.Linked) + len(view.Candidates)
	start, end := page*linkPageSize, min((page+1)*linkPageSize, total)
	var rows [][]models.InlineKeyboardButton
	for i := start; i < end; i++ {
		var u bridge.User
		var label, data string
		if i < len(view.Linked) {
			u = view.Linked[i]
			label = l.unlink
			data = key + ":u:" + strconv.Itoa(i)
		} else {
			index := i - len(view.Linked)
			u = view.Candidates[index].User
			label = l.add
			data = key + ":a:" + strconv.Itoa(index)
		}
		label += linkUserLabel(u)
		rows = append(rows, []models.InlineKeyboardButton{{Text: label, CallbackData: data}})
	}
	var nav []models.InlineKeyboardButton
	if page > 0 {
		nav = append(nav, models.InlineKeyboardButton{Text: l.previous, CallbackData: key + ":p:" + strconv.Itoa(page-1)})
	}
	if end < total {
		nav = append(nav, models.InlineKeyboardButton{Text: l.next, CallbackData: key + ":p:" + strconv.Itoa(page+1)})
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (c *Client) whois(ctx context.Context, m *models.Message) error {
	l := language(m.From.LanguageCode)
	text := l.whoisReply
	if target := m.ReplyToMessage; target != nil {
		var fallback bridge.User
		if target.From != nil && !target.From.IsBot && target.SenderChat == nil {
			fallback = user(target.From)
		}
		identity, err := c.handler.Whois(ctx, bridge.Telegram, strconv.Itoa(target.ID), fallback)
		if err != nil {
			return err
		}
		text = renderIdentity(identity, l)
	}
	_, err := c.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: c.chatID, Text: text, ParseMode: models.ParseModeHTML,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
		ReplyParameters:    &models.ReplyParameters{MessageID: m.ID},
	})
	return err
}

func renderIdentity(identity bridge.Identity, l locale) string {
	if identity.Telegram.ID == "" && identity.Discord.ID == "" {
		return l.authorUnknown
	}
	profile := func(u bridge.User, url string) string {
		if u.ID == "" {
			return l.notLinked
		}
		name := u.Name
		if name == "" {
			name = u.Username
		}
		if name == "" {
			name = u.ID
		}
		return `<a href="` + url + html.EscapeString(u.ID) + `">` + html.EscapeString(name) + `</a>`
	}
	return "Telegram: " + profile(identity.Telegram, "tg://user?id=") +
		"\nDiscord: " + profile(identity.Discord, "https://discord.com/users/")
}

func (c *Client) callback(ctx context.Context, q *models.CallbackQuery) (result error) {
	l := language(q.From.LanguageCode)
	answer := l.unavailable
	defer func() {
		// Acknowledge even unknown, unauthorized, or failed callbacks.
		_, err := c.bot.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: q.ID, Text: answer})
		if err != nil {
			result = errors.Join(result, fmt.Errorf("acknowledge Telegram callback: %w", err))
		}
	}()

	m := q.Message.Message
	if m == nil || m.Chat.ID != c.chatID {
		return nil
	}
	parts := strings.Split(q.Data, ":")
	if len(parts) < 2 || parts[0] != "link" {
		return nil
	}
	key := strings.Join(parts[:2], ":")
	choice, ok := c.links[key]
	if !ok {
		return nil
	}
	if choice.requester != q.From.ID {
		answer = l.unauthorized
		return nil
	}

	if len(parts) != 4 {
		return nil
	}
	index, err := strconv.Atoi(parts[3])
	if err != nil || index < 0 {
		return nil
	}
	if parts[2] == "p" {
		if index >= (len(choice.view.Linked)+len(choice.view.Candidates)+linkPageSize-1)/linkPageSize {
			return nil
		}
		_, err = c.bot.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
			ChatID: c.chatID, MessageID: m.ID, ReplyMarkup: linkKeyboard(key, choice.view, index, choice.locale),
		})
		if err != nil {
			answer = l.pageError
			return err
		}
		answer = ""
		return nil
	}
	answer, err = c.changeLink(ctx, key, choice, m.ID, parts[2], index, l)
	return err
}

func (c *Client) changeLink(ctx context.Context, key string, choice linkChoice, messageID int, action string, index int, l locale) (string, error) {
	var counterpart bridge.User
	switch action {
	case "u":
		if index >= len(choice.view.Linked) {
			return l.unavailable, nil
		}
		counterpart = choice.view.Linked[index]
	case "a":
		if index >= len(choice.view.Candidates) {
			return l.unavailable, nil
		}
		counterpart = choice.view.Candidates[index].User
	default:
		return l.unavailable, nil
	}

	telegramID, discordID := choice.view.Author.ID, counterpart.ID
	if choice.view.Platform == bridge.Discord {
		telegramID, discordID = counterpart.ID, choice.view.Author.ID
	}
	var err error
	var answer string
	if action == "u" {
		err = c.handler.UnlinkPair(ctx, telegramID, discordID)
		answer = l.unlinked
	} else {
		err = c.handler.LinkUsers(ctx, telegramID, discordID)
		answer = l.linked
	}
	if err != nil {
		return l.changeError, err
	}

	view, err := c.handler.LinkView(ctx, choice.messageID, choice.fallback, choice.query)
	if err != nil {
		delete(c.links, key)
		return l.refreshError, err
	}
	choice.view = view
	c.links[key] = choice
	_, err = c.bot.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: c.chatID, MessageID: messageID, Text: linkText(view, choice.locale), ReplyMarkup: linkKeyboard(key, view, 0, choice.locale),
	})
	if err != nil {
		delete(c.links, key)
		answer = l.displayError
	}
	return answer, err
}

func renderReactions(summary bridge.ReactionSummary, l locale) string {
	if !summary.LinkFound {
		return l.notDiscordMessage
	}
	if len(summary.Reactions) == 0 {
		return l.noDiscordReactions
	}
	var text strings.Builder
	text.WriteString(l.discordReactions)
	for _, reaction := range summary.Reactions {
		names := make([]string, 0, len(reaction.Users))
		for _, user := range reaction.Users {
			names = append(names, user.Name)
		}
		fmt.Fprintf(&text, "\n%s - %s", reaction.Emoji, strings.Join(names, ", "))
	}
	return text.String()
}
