package telegram

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
	"github.com/gotd/contrib/storage"
	gotdtelegram "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/richtext"
)

func userEntity(ctx context.Context, peers storage.PeerStorage, e tg.Entities, id int64) *tg.User {
	if u := e.Users[id]; u != nil {
		return u
	}
	if peers != nil {
		if p, err := peers.Find(ctx, storage.PeerKey{Kind: dialogs.User, ID: id}); err == nil {
			return p.User
		}
	}
	return nil
}
func userAuthor(ctx context.Context, peers storage.PeerStorage, e tg.Entities, p tg.PeerClass) bridge.User {
	switch p := p.(type) {
	case *tg.PeerUser:
		u := userEntity(ctx, peers, e, p.UserID)
		if u != nil {
			return bridge.User{ID: strconv.FormatInt(u.ID, 10), Username: u.Username, Name: joinName(u.FirstName, u.LastName)}
		}
		return bridge.User{ID: strconv.FormatInt(p.UserID, 10)}
	case *tg.PeerChannel:
		if ch := e.Channels[p.ChannelID]; ch != nil {
			return bridge.User{Name: ch.Title}
		}
	case *tg.PeerChat:
		if ch := e.Chats[p.ChatID]; ch != nil {
			return bridge.User{Name: ch.Title}
		}
	}
	return bridge.User{Name: "Telegram"}
}
func (c *Client) forwardUserMessage(ctx context.Context, client *gotdtelegram.Client, peers storage.PeerStorage, h bridge.Handler, e tg.Entities, value tg.MessageClass, edit bool) error {
	switch m := value.(type) {
	case *tg.MessageService:
		if !c.matchesUserPeer(m.PeerID) || c.isOwnBot(m.FromID) {
			return nil
		}
		if reply, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok && (reply.ForumTopic || reply.ReplyToTopID != 0) {
			return nil
		}

		event, ok := userSystemMessage(ctx, peers, e, m)
		if !ok {
			return nil
		}
		return h.HandleMessage(ctx, bridge.Telegram, event)
	case *tg.Message:
		if !c.matchesUserPeer(m.PeerID) || c.isOwnBot(m.FromID) {
			return nil
		}
		commandText := m.Media == nil
		switch m.Media.(type) {
		case *tg.MessageMediaEmpty, *tg.MessageMediaWebPage:
			commandText = true
		}
		if commandText && strings.HasPrefix(m.Message, "/") {
			fromBot := false
			if sender, ok := m.FromID.(*tg.PeerUser); ok {
				if u := userEntity(ctx, peers, e, sender.UserID); u != nil {
					fromBot = u.Bot
				}
			}
			if _, addressed := c.bridgeCommand(m.Message, fromBot); addressed {
				return nil
			}
		}
		entities, emojis := userEntities(m.Entities)
		msg := bridge.Message{
			ID:     strconv.Itoa(m.ID),
			Author: userAuthor(ctx, peers, e, m.FromID),
			Text:   richtext.FromTelegram(m.Message, entities),
		}
		if r, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok && r.ReplyToMsgID != 0 {
			msg.ReplyTo = strconv.Itoa(r.ReplyToMsgID)
		}

		file, loc := userFile(m.Media)
		if loc == nil && m.Message != "" {
			if id := soleCustomEmoji(m.Message, emojis); id != "" {
				a, err := c.receiveEmoji(ctx, id)
				if err != nil {
					slog.Warn("Telegram custom emoji processing failed", "error", err)
				} else {
					msg.Text = richtext.Text{}
					msg.Attachments = append(msg.Attachments, a)
				}
			}
		}

		if loc != nil {
			a, err := c.receiveUserFile(ctx, client, loc, file)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				slog.Warn("Telegram attachment processing failed", "filename", a.Name, "error", err)
				if a.Animated && len(a.Data) > 0 {
					msg.Text.Value += "\n[Animation could not be converted to GIF; sending original file.]"
				}
			}
			msg.Attachments = append(msg.Attachments, a)
		}

		if msg.Text.Value == "" && len(msg.Attachments) == 0 {
			return nil
		}
		if edit {
			return h.HandleEdit(ctx, bridge.Telegram, msg)
		}
		return h.HandleMessage(ctx, bridge.Telegram, msg)
	}
	return nil
}
func (c *Client) isOwnBot(p tg.PeerClass) bool {
	u, ok := p.(*tg.PeerUser)
	return ok && c.bot != nil && u.UserID == c.bot.ID()
}

func userEntities(es []tg.MessageEntityClass) ([]richtext.TelegramEntity, []models.MessageEntity) {
	var out []richtext.TelegramEntity
	var emojis []models.MessageEntity

	for _, e := range es {
		r, ok := e.(interface {
			GetOffset() int
			GetLength() int
		})
		if !ok {
			continue
		}

		v := richtext.TelegramEntity{Offset: r.GetOffset(), Length: r.GetLength()}
		switch x := e.(type) {
		case *tg.MessageEntityBold:
			v.Kind = "bold"
		case *tg.MessageEntityItalic:
			v.Kind = "italic"
		case *tg.MessageEntityUnderline:
			v.Kind = "underline"
		case *tg.MessageEntityStrike:
			v.Kind = "strike"
		case *tg.MessageEntitySpoiler:
			v.Kind = "spoiler"
		case *tg.MessageEntityCode:
			v.Kind = "code"
		case *tg.MessageEntityPre:
			v.Kind = "pre"
		case *tg.MessageEntityURL:
			v.Kind = "url"
		case *tg.MessageEntityMention:
			v.Kind = "mention"
		case *tg.MessageEntityTextURL:
			v.Kind = "text_link"
			v.URL = x.URL
		case *tg.MessageEntityMentionName:
			v.Kind = "text_mention"
			v.UserID = strconv.FormatInt(x.UserID, 10)
		case *tg.MessageEntityBlockquote:
			v.Kind = "blockquote"
		case *tg.MessageEntityCustomEmoji:
			emoji := models.MessageEntity{
				Type: models.MessageEntityTypeCustomEmoji, Offset: v.Offset,
				Length: v.Length, CustomEmojiID: strconv.FormatInt(x.DocumentID, 10),
			}
			emojis = append(emojis, emoji)
		}

		if v.Kind != "" {
			out = append(out, v)
		}
	}

	return out, emojis
}

func userSystemMessage(ctx context.Context, peers storage.PeerStorage, e tg.Entities, m *tg.MessageService) (bridge.Message, bool) {
	model := &models.Message{ID: m.ID}
	toUser := func(id int64) models.User {
		u := userAuthor(ctx, peers, e, &tg.PeerUser{UserID: id})
		return models.User{ID: id, FirstName: u.Name, Username: u.Username}
	}
	if p, ok := m.FromID.(*tg.PeerUser); ok {
		u := toUser(p.UserID)
		model.From = &u
	}
	switch a := m.Action.(type) {
	case *tg.MessageActionChatAddUser:
		for _, id := range a.Users {
			model.NewChatMembers = append(model.NewChatMembers, toUser(id))
		}
	case *tg.MessageActionChatDeleteUser:
		u := toUser(a.UserID)
		model.LeftChatMember = &u
	case *tg.MessageActionChatJoinedByLink, *tg.MessageActionChatJoinedByRequest:
		if model.From != nil {
			model.NewChatMembers = []models.User{*model.From}
		}
	case *tg.MessageActionChatEditTitle:
		model.NewChatTitle = a.Title
	case *tg.MessageActionChatEditPhoto:
		model.NewChatPhoto = []models.PhotoSize{{}}
	case *tg.MessageActionChatDeletePhoto:
		model.DeleteChatPhoto = true
	case *tg.MessageActionChatMigrateTo:
		model.MigrateToChatID = -1000000000000 - a.ChannelID
	case *tg.MessageActionChannelMigrateFrom:
		model.MigrateFromChatID = -a.ChatID
	default:
		return bridge.Message{}, false
	}
	return systemMessage(model)
}

func userFile(media tg.MessageMediaClass) (bridge.Attachment, tg.InputFileLocationClass) {
	switch m := media.(type) {
	case *tg.MessageMediaDocument:
		d, ok := m.Document.(*tg.Document)
		if !ok {
			break
		}

		a := bridge.Attachment{Name: "document", ContentType: d.MimeType, Size: d.Size}
		animated := false
		sticker := false
		video := m.Video
		voice := m.Voice
		var audio *tg.DocumentAttributeAudio
		round := m.Round

		for _, attr := range d.Attributes {
			switch x := attr.(type) {
			case *tg.DocumentAttributeFilename:
				a.Name = x.FileName
			case *tg.DocumentAttributeAnimated:
				animated = true
			case *tg.DocumentAttributeSticker:
				sticker = true
			case *tg.DocumentAttributeVideo:
				video = true
				round = round || x.RoundMessage
			case *tg.DocumentAttributeAudio:
				voice = voice || x.Voice
				audio = x
			}
		}

		if sticker {
			a.Name = "sticker.webp"
			if d.MimeType == "application/x-tgsticker" {
				a.Name = "sticker.tgs"
				animated = true
			}
			if d.MimeType == "video/webm" {
				a.Name = "sticker.webm"
				animated = true
			}
		} else if a.Name == "document" {
			switch {
			case animated:
				a.Name = "animation.mp4"
			case round:
				a.Name = "video-note.mp4"
			case video:
				a.Name = "video.mp4"
			case voice:
				a.Name = "voice.ogg"
			case audio != nil:
				a.Name = "audio.mp3"
			}
		}

		if voice {
			a.Voice = &bridge.Voice{}
			if audio != nil {
				a.Voice.Duration = audio.Duration
				a.Voice.Waveform = unpackWaveform(audio.Waveform)
			}
		}
		a.Animated = animated
		return a, &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference}
	case *tg.MessageMediaPhoto:
		p, ok := m.Photo.(*tg.Photo)
		if !ok {
			break
		}

		var typ string
		var size, area int

		for _, s := range p.Sizes {
			switch x := s.(type) {
			case *tg.PhotoSize:
				if x.W*x.H > area {
					typ = x.Type
					size = x.Size
					area = x.W * x.H
				}
			case *tg.PhotoSizeProgressive:
				if x.W*x.H > area && len(x.Sizes) > 0 {
					typ = x.Type
					size = x.Sizes[len(x.Sizes)-1]
					area = x.W * x.H
				}
			}
		}

		if typ != "" {
			a := bridge.Attachment{Name: "photo.jpg", ContentType: "image/jpeg", Size: int64(size)}
			loc := &tg.InputPhotoFileLocation{ID: p.ID, AccessHash: p.AccessHash, FileReference: p.FileReference, ThumbSize: typ}
			return a, loc
		}
	}
	return bridge.Attachment{}, nil
}

// Telegram stores little-endian 5-bit amplitudes; Discord expects one byte per sample.
func unpackWaveform(packed []byte) []byte {
	samples := make([]byte, len(packed)*8/5)
	for i := range samples {
		bit := i * 5
		value := uint16(packed[bit/8]) >> (bit % 8)
		if bit%8 > 3 {
			value |= uint16(packed[bit/8+1]) << (8 - bit%8)
		}
		samples[i] = byte((value & 31) * 255 / 31)
	}
	return samples
}

type limitedMediaBuffer struct {
	bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *limitedMediaBuffer) Write(p []byte) (int, error) {
	if int64(b.Len())+int64(len(p)) > b.limit {
		b.exceeded = true
		return 0, fmt.Errorf("file exceeds download limit")
	}
	return b.Buffer.Write(p)
}

func (c *Client) receiveUserFile(ctx context.Context, client interface {
	Download(tg.InputFileLocationClass) *downloader.Builder
}, loc tg.InputFileLocationClass, a bridge.Attachment) (bridge.Attachment, error) {
	if a.Size > c.maxDownloadBytes {
		return a, fmt.Errorf("file exceeds download limit")
	}
	data := &limitedMediaBuffer{limit: c.maxDownloadBytes}
	_, err := client.Download(loc).Stream(ctx, data)
	if err != nil {
		if data.exceeded {
			a.Size = c.maxDownloadBytes + 1
		}
		return a, fmt.Errorf("download Telegram media: %w", err)
	}
	a.Data = data.Bytes()
	a.Size = int64(len(a.Data))
	return convertIncomingAnimation(ctx, a)
}
