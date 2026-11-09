package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/richtext"
)

func systemMessage(m *models.Message) (bridge.Message, bool) {
	text := systemText(m)
	if text == "" {
		return bridge.Message{}, false
	}
	author := bridge.User{Name: "Telegram"}
	if m.From != nil {
		author = user(m.From)
	} else if m.SenderChat != nil {
		author.Name = m.SenderChat.Title
	}
	return bridge.Message{ID: strconv.Itoa(m.ID), Author: author, Text: richtext.Text{Value: text}}, true
}

func systemText(m *models.Message) string {
	var text string
	switch {
	case len(m.NewChatMembers) > 0:
		names := make([]string, 0, len(m.NewChatMembers))
		for _, member := range m.NewChatMembers {
			names = append(names, systemUserName(&member))
		}
		if len(m.NewChatMembers) == 1 && m.From != nil && m.From.ID == m.NewChatMembers[0].ID {
			text = "joined the chat"
		} else {
			text = "added " + strings.Join(names, ", ") + " to the chat"
		}
	case m.LeftChatMember != nil:
		if m.From != nil && m.From.ID == m.LeftChatMember.ID {
			text = "left the chat"
		} else {
			text = "removed " + systemUserName(m.LeftChatMember) + " from the chat"
		}
	case m.PinnedMessage != nil:
		text = "pinned a message"
	case m.NewChatTitle != "":
		text = fmt.Sprintf("changed the chat title to %q", m.NewChatTitle)
	case len(m.NewChatPhoto) > 0:
		text = "changed the chat photo"
	case m.DeleteChatPhoto:
		text = "removed the chat photo"
	case m.MigrateToChatID != 0:
		text = fmt.Sprintf("the chat migrated to supergroup %d", m.MigrateToChatID)
	case m.MigrateFromChatID != 0:
		text = fmt.Sprintf("the chat migrated from group %d", m.MigrateFromChatID)
	}
	return text
}

func systemUserName(u *models.User) string {
	name := joinName(u.FirstName, u.LastName)
	if name == "" {
		name = u.Username
	}
	if name == "" {
		name = strconv.FormatInt(u.ID, 10)
	}
	return name
}
