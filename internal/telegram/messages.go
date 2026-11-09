package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/sleroq/zefxi/internal/animation"
	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/richtext"
)

const downloadLimit = 20 * 1024 * 1024
const uploadLimit = 50 * 1024 * 1024

var fileHTTP = &http.Client{Timeout: 2 * time.Minute}
var errFileLookup = errors.New("telegram file lookup failed")

type incomingFile struct {
	id         string
	attachment bridge.Attachment
}

func (c *Client) message(ctx context.Context, m *models.Message) error {
	return c.forwardMessage(ctx, m, false)
}

func (c *Client) editedMessage(ctx context.Context, m *models.Message) error {
	return c.forwardMessage(ctx, m, true)
}

func (c *Client) forwardMessage(ctx context.Context, m *models.Message, edit bool) error {
	value, entities := m.Text, m.Entities
	if value == "" {
		value, entities = m.Caption, m.CaptionEntities
	}

	converted := make([]richtext.TelegramEntity, 0, len(entities))
	for _, e := range entities {
		entity := richtext.TelegramEntity{Kind: string(e.Type), Offset: e.Offset, Length: e.Length, URL: e.URL}
		if e.User != nil {
			entity.UserID = strconv.FormatInt(e.User.ID, 10)
		}
		converted = append(converted, entity)
	}

	msg := bridge.Message{
		ID: strconv.Itoa(m.ID), Author: user(m.From),
		Forward: botForwardSource(m.ForwardOrigin), Text: richtext.FromTelegram(value, converted),
	}
	if m.ReplyToMessage != nil {
		msg.ReplyTo = strconv.Itoa(m.ReplyToMessage.ID)
	}

	files := messageFiles(m)
	if m.Text != "" && len(files) == 0 {
		if id := soleCustomEmoji(m.Text, m.Entities); id != "" {
			a, err := c.receiveEmoji(ctx, id)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				slog.Warn("Telegram custom emoji processing failed", "error", err)
			} else {
				msg.Text = richtext.Text{}
				msg.Attachments = append(msg.Attachments, a)
			}
		}
	}

	for _, f := range files {
		a, err := c.receiveFile(ctx, f)
		if err != nil {
			// Do not surface transport errors containing a token-bearing download URL.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("Telegram attachment processing failed", "filename", a.Name, "error", err)
			if errors.Is(err, errFileLookup) {
				notice := "\n[Attachment unavailable: %s; could not retrieve it due to a Telegram API error.]"
				msg.Text.Value += fmt.Sprintf(notice, a.Name)
				continue
			}
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
		return c.handler.HandleEdit(ctx, bridge.Telegram, msg)
	}
	return c.handler.HandleMessage(ctx, bridge.Telegram, msg)
}

func botForwardSource(origin *models.MessageOrigin) *bridge.ForwardSource {
	if origin == nil {
		return nil
	}
	switch origin.Type {
	case models.MessageOriginTypeUser:
		if origin.MessageOriginUser != nil {
			u := origin.MessageOriginUser.SenderUser
			return &bridge.ForwardSource{User: user(&u)}
		}
	case models.MessageOriginTypeHiddenUser:
		if origin.MessageOriginHiddenUser != nil {
			return &bridge.ForwardSource{Name: origin.MessageOriginHiddenUser.SenderUserName}
		}
	case models.MessageOriginTypeChat:
		if origin.MessageOriginChat != nil {
			return &bridge.ForwardSource{Name: origin.MessageOriginChat.SenderChat.Title}
		}
	case models.MessageOriginTypeChannel:
		if origin.MessageOriginChannel != nil {
			ch := origin.MessageOriginChannel.Chat
			return &bridge.ForwardSource{Name: ch.Title, URL: publicChannelURL(ch.Username)}
		}
	}
	return nil
}

func publicChannelURL(username string) string {
	if username == "" {
		return ""
	}
	for _, r := range username {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return ""
		}
	}
	return "https://t.me/" + username
}

func soleCustomEmoji(text string, entities []models.MessageEntity) string {
	var emoji *models.MessageEntity
	for i := range entities {
		if entities[i].Type == models.MessageEntityTypeCustomEmoji {
			if emoji != nil {
				return ""
			}
			emoji = &entities[i]
		}
	}
	if emoji == nil || emoji.CustomEmojiID == "" || emoji.Length <= 0 {
		return ""
	}
	start, end, offset := emoji.Offset, emoji.Offset+emoji.Length, 0
	foundStart, foundEnd := false, false
	for _, r := range text {
		if offset == start {
			foundStart = true
		}
		if offset == end {
			foundEnd = true
		}
		if (offset < start || offset >= end) && !unicode.IsSpace(r) {
			return ""
		}
		if r > 0xffff {
			offset += 2
		} else {
			offset++
		}
	}
	foundEnd = foundEnd || offset == end
	if !foundStart || !foundEnd {
		return ""
	}
	return emoji.CustomEmojiID
}

func (c *Client) receiveEmoji(ctx context.Context, id string) (bridge.Attachment, error) {
	stickers, err := c.bot.GetCustomEmojiStickers(ctx, &bot.GetCustomEmojiStickersParams{CustomEmojiIDs: []string{id}})
	if err != nil || len(stickers) != 1 || stickers[0] == nil {
		return bridge.Attachment{}, fmt.Errorf("custom emoji lookup failed")
	}
	sticker := stickers[0]
	if sticker.FileSize > downloadLimit {
		return bridge.Attachment{}, fmt.Errorf("custom emoji exceeds download limit")
	}
	file, err := c.bot.GetFile(ctx, &bot.GetFileParams{FileID: sticker.FileID})
	if err != nil || file.FilePath == "" {
		return bridge.Attachment{}, fmt.Errorf("custom emoji file lookup failed")
	}
	data, err := download(ctx, c.bot.FileDownloadLink(file), downloadLimit)
	if err != nil {
		return bridge.Attachment{}, err
	}
	name, mime := "emoji.png", "image/png"
	switch {
	case sticker.IsAnimated:
		data, err = animation.EmojiFromTGS(ctx, data)
		name, mime = "emoji.gif", "image/gif"
	case sticker.IsVideo:
		data, err = animation.EmojiFromVideo(ctx, data)
		name, mime = "emoji.gif", "image/gif"
	default:
		data, err = animation.EmojiFromWebP(ctx, data)
	}
	if err != nil {
		return bridge.Attachment{}, fmt.Errorf("custom emoji conversion failed: %w", err)
	}
	return bridge.Attachment{Name: name, ContentType: mime, Data: data, Size: int64(len(data))}, nil
}

func (c *Client) receiveFile(ctx context.Context, f incomingFile) (bridge.Attachment, error) {
	a := f.attachment
	if a.Size > downloadLimit {
		return a, fmt.Errorf("exceeds Telegram's 20 MB download limit")
	}
	file, err := c.bot.GetFile(ctx, &bot.GetFileParams{FileID: f.id})
	if err != nil {
		return a, errFileLookup
	}
	if file.FilePath == "" {
		return a, fmt.Errorf("file is unavailable")
	}
	a.Data, err = download(ctx, c.bot.FileDownloadLink(file), downloadLimit)
	if err != nil {
		return a, err
	}
	a.Size = int64(len(a.Data))
	return convertIncomingAnimation(ctx, a)
}

func convertIncomingAnimation(ctx context.Context, a bridge.Attachment) (bridge.Attachment, error) {
	if !a.Animated {
		return a, nil
	}

	gifData := a.Data
	if !bytes.HasPrefix(gifData, []byte("GIF87a")) && !bytes.HasPrefix(gifData, []byte("GIF89a")) {
		var err error
		if a.ContentType == "application/x-tgsticker" {
			gifData, err = animation.FromTGS(ctx, a.Data)
		} else {
			gifData, err = animation.FromVideo(ctx, a.Data)
		}
		if err != nil {
			return a, fmt.Errorf("convert animation to GIF: %w", err)
		}
	}
	a.Data = gifData
	a.Size = int64(len(gifData))
	a.Name = strings.TrimSuffix(a.Name, filepath.Ext(a.Name)) + ".gif"
	a.ContentType = "image/gif"
	return a, nil
}

func messageFiles(m *models.Message) []incomingFile {
	var files []incomingFile
	add := func(id, name, mime string, size int64) {
		files = append(files, incomingFile{id, bridge.Attachment{Name: name, ContentType: mime, Size: size}})
	}

	if f := m.Document; f != nil {
		name := f.FileName
		if name == "" {
			name = "document"
		}
		add(f.FileID, name, f.MimeType, int64(f.FileSize))
	}
	if len(m.Photo) > 0 {
		f := m.Photo[len(m.Photo)-1]
		add(f.FileID, "photo.jpg", "image/jpeg", int64(f.FileSize))
	}
	if f := m.Video; f != nil {
		name := f.FileName
		if name == "" {
			name = "video.mp4"
		}
		add(f.FileID, name, f.MimeType, int64(f.FileSize))
	}
	if f := m.Audio; f != nil {
		name := f.FileName
		if name == "" {
			name = "audio.mp3"
		}
		add(f.FileID, name, f.MimeType, int64(f.FileSize))
	}

	if f := m.Voice; f != nil {
		add(f.FileID, "voice.ogg", f.MimeType, int64(f.FileSize))
		files[len(files)-1].attachment.Voice = &bridge.Voice{Duration: f.Duration}
	}
	if f := m.VideoNote; f != nil {
		add(f.FileID, "video-note.mp4", "video/mp4", int64(f.FileSize))
	}
	if f := m.Sticker; f != nil {
		name, mime := "sticker.webp", "image/webp"
		if f.IsAnimated {
			name, mime = "sticker.tgs", "application/x-tgsticker"
		}
		if f.IsVideo {
			name, mime = "sticker.webm", "video/webm"
		}
		add(f.FileID, name, mime, int64(f.FileSize))
		files[len(files)-1].attachment.Animated = f.IsAnimated || f.IsVideo
	}

	// Telegram also populates Document for animations.
	if f := m.Animation; f != nil {
		files = nil
		name := f.FileName
		if name == "" {
			name = "animation.mp4"
		}
		add(f.FileID, name, f.MimeType, int64(f.FileSize))
		files[len(files)-1].attachment.Animated = true
	}
	return files
}

func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid file request")
	}
	res, err := fileHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("file download failed")
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			slog.Warn("File response close failed")
		}
	}()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file download HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("file read failed")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds download limit")
	}
	return data, nil
}

func reply(id string) (*models.ReplyParameters, error) {
	if id == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("telegram reply ID: %w", err)
	}
	return &models.ReplyParameters{MessageID: n}, nil
}
