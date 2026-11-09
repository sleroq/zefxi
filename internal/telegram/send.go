package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"mime"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/sleroq/zefxi/internal/animation"
	"github.com/sleroq/zefxi/internal/bridge"
)

const photoLimit = 10 * 1024 * 1024

type outgoingFile struct {
	bridge.Attachment
	kind string
	err  error
}

func prepareFile(ctx context.Context, a bridge.Attachment) outgoingFile {
	if len(a.Data) == 0 && a.URL != "" && a.Size <= uploadLimit {
		var err error
		a.Data, err = download(ctx, a.URL, uploadLimit)
		if err != nil {
			slog.Warn("Discord attachment download failed; sending source link", "filename", a.Name, "error", err)
		}
	}
	f := outgoingFile{Attachment: a}
	if len(a.Data) == 0 || len(a.Data) > uploadLimit {
		return f
	}
	if a.StickerFormat != "" {
		return prepareSticker(ctx, f)
	}

	mediaType, _, _ := mime.ParseMediaType(a.ContentType)
	if mediaType == "" {
		mediaType, _, _ = mime.ParseMediaType(mime.TypeByExtension(strings.ToLower(filepath.Ext(a.Name))))
	}
	f.kind = "document"
	switch {
	case a.Animated || mediaType == "image/gif":
		f.kind = "animation"
	case (mediaType == "image/jpeg" || mediaType == "image/png" || mediaType == "image/webp") && len(a.Data) <= photoLimit:
		f.kind = "photo"
	case mediaType == "video/mp4":
		f.kind = "video"
	}
	return f
}

func prepareSticker(ctx context.Context, f outgoingFile) outgoingFile {
	switch f.StickerFormat {
	case bridge.StickerPNG:
		f.Data, f.err = animation.StickerPNG(ctx, f.Data)
		if f.err == nil {
			f.Name = strings.TrimSuffix(f.Name, filepath.Ext(f.Name)) + ".webp"
		}
	case bridge.StickerAPNG, bridge.StickerGIF, bridge.StickerLottie:
		f.Data, f.err = animation.StickerVideo(ctx, f.Data, f.StickerFormat == bridge.StickerLottie)
		if f.err == nil {
			f.Name = strings.TrimSuffix(f.Name, filepath.Ext(f.Name)) + ".webm"
		}
	default:
		f.err = fmt.Errorf("unknown sticker format %q", f.StickerFormat)
	}
	if f.err == nil {
		f.kind = "sticker"
	}
	return f
}

func (c *Client) Send(ctx context.Context, m bridge.OutboundMessage) ([]string, error) {
	rp, err := reply(m.ReplyTo)
	if err != nil {
		return nil, err
	}

	name := m.Author.Name
	if name == "" {
		name = m.Author.Username
	}
	name = html.EscapeString(name)
	if m.TelegramUserID != "" {
		name = `<a href="tg://user?id=` + html.EscapeString(m.TelegramUserID) + `">` + name + `</a>`
	}
	author := "<b>" + name + "</b>"
	caption := author
	if m.Text != "" {
		caption += ": " + m.Text
	}

	var ids []string
	sendText := func(text string) error {
		sent, e := c.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          c.chatID,
			Text:            text,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: rp,
		})
		if e == nil {
			ids = append(ids, strconv.Itoa(sent.ID))
		}
		return e
	}
	if len(m.Attachments) == 0 {
		err = sendText(caption)
		return ids, err
	}
	// Counting markup too is conservative and never splits an HTML entity or tag.
	var overflowText string
	if len(utf16.Encode([]rune(caption))) > 1024 {
		overflowText = caption
		caption = author
	}

	files := make([]outgoingFile, len(m.Attachments))
	for i, a := range m.Attachments {
		files[i] = prepareFile(ctx, a)
	}

	var conversionErr error
	for i := 0; i < len(files); {
		f := files[i]
		if f.err != nil {
			conversionErr = errors.Join(conversionErr, fmt.Errorf("convert sticker %s: %w", f.Name, f.err))
		}
		end := i + 1
		if f.kind == "photo" || f.kind == "video" {
			for end < len(files) && end-i < 10 && (files[end].kind == "photo" || files[end].kind == "video") {
				end++
			}
		}
		switch {
		case end-i > 1:
			media := albumMedia(files[i:end], i, caption)
			sent, e := c.bot.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
				ChatID:          c.chatID,
				Media:           media,
				ReplyParameters: rp,
			})
			for _, message := range sent {
				ids = append(ids, strconv.Itoa(message.ID))
			}
			if e != nil {
				return ids, errors.Join(conversionErr, e)
			}
		case f.kind == "sticker":
			if err = sendText(caption); err != nil {
				return ids, errors.Join(conversionErr, err)
			}
			sent, e := c.sendFile(ctx, f, "", rp)
			if e != nil {
				return ids, errors.Join(conversionErr, e)
			}
			ids = append(ids, strconv.Itoa(sent.ID))
		case f.kind == "":
			notice := "[Attachment unavailable: " + f.Name + "]"
			if f.URL != "" {
				notice = f.Name + ": " + f.URL
			}
			if err = sendText(caption + "\n" + html.EscapeString(notice)); err != nil {
				return ids, errors.Join(conversionErr, err)
			}
		default:
			sent, e := c.sendFile(ctx, f, caption, rp)
			if e != nil {
				return ids, errors.Join(conversionErr, e)
			}
			ids = append(ids, strconv.Itoa(sent.ID))
		}
		caption = author
		i = end
	}
	if overflowText != "" {
		if err = sendText(overflowText); err != nil {
			return ids, errors.Join(conversionErr, err)
		}
	}
	return ids, conversionErr
}

func (c *Client) sendFile(ctx context.Context, f outgoingFile, caption string, rp *models.ReplyParameters) (*models.Message, error) {
	upload := &models.InputFileUpload{Filename: f.Name, Data: bytes.NewReader(f.Data)}
	switch f.kind {
	case "sticker":
		return c.bot.SendSticker(ctx, &bot.SendStickerParams{ChatID: c.chatID, Sticker: upload, ReplyParameters: rp})
	case "animation":
		return c.bot.SendAnimation(ctx, &bot.SendAnimationParams{
			ChatID:          c.chatID,
			Animation:       upload,
			Caption:         caption,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: rp,
		})
	case "photo":
		return c.bot.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:          c.chatID,
			Photo:           upload,
			Caption:         caption,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: rp,
		})
	case "video":
		return c.bot.SendVideo(ctx, &bot.SendVideoParams{
			ChatID:          c.chatID,
			Video:           upload,
			Caption:         caption,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: rp,
		})
	default:
		return c.bot.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:          c.chatID,
			Document:        upload,
			Caption:         caption,
			ParseMode:       models.ParseModeHTML,
			ReplyParameters: rp,
		})
	}
}

func albumMedia(files []outgoingFile, offset int, caption string) []models.InputMedia {
	media := make([]models.InputMedia, 0, len(files))
	for j, item := range files {
		ref := fmt.Sprintf("attach://media%d", offset+j)
		if item.kind == "photo" {
			media = append(media, &models.InputMediaPhoto{
				Media:           ref,
				MediaAttachment: bytes.NewReader(item.Data),
				Caption:         caption,
				ParseMode:       models.ParseModeHTML,
			})
		} else {
			media = append(media, &models.InputMediaVideo{
				Media:           ref,
				MediaAttachment: bytes.NewReader(item.Data),
				Caption:         caption,
				ParseMode:       models.ParseModeHTML,
			})
		}
		caption = ""
	}
	return media
}

func (c *Client) Edit(ctx context.Context, id string, m bridge.OutboundMessage) error {
	n, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("telegram message ID: %w", err)
	}

	name := m.Author.Name
	if name == "" {
		name = m.Author.Username
	}
	name = html.EscapeString(name)
	if m.TelegramUserID != "" {
		name = `<a href="tg://user?id=` + html.EscapeString(m.TelegramUserID) + `">` + name + `</a>`
	}
	caption := "<b>" + name + "</b>"
	if m.Text != "" {
		caption += ": " + m.Text
	}
	if len(m.Attachments) == 0 {
		params := &bot.EditMessageTextParams{ChatID: c.chatID, MessageID: n, Text: caption, ParseMode: models.ParseModeHTML}
		_, err = c.bot.EditMessageText(ctx, params)
		return err
	}
	if len(m.Attachments) != 1 || len(utf16.Encode([]rune(caption))) > 1024 {
		return fmt.Errorf("telegram: edit requires multiple messages")
	}
	if m.Attachments[0].StickerFormat != "" {
		return fmt.Errorf("telegram: sticker edit requires multiple messages")
	}
	if m.PreserveAttachments {
		params := &bot.EditMessageCaptionParams{
			ChatID: c.chatID, MessageID: n, Caption: caption, ParseMode: models.ParseModeHTML,
		}
		_, err = c.bot.EditMessageCaption(ctx, params)
		return err
	}
	f := prepareFile(ctx, m.Attachments[0])
	if f.kind == "" {
		return fmt.Errorf("telegram: edited attachment unavailable")
	}
	var media models.InputMedia
	switch f.kind {
	case "photo":
		media = &models.InputMediaPhoto{
			Media: "attach://edited_file", MediaAttachment: bytes.NewReader(f.Data),
			Caption: caption, ParseMode: models.ParseModeHTML,
		}
	case "video":
		media = &models.InputMediaVideo{
			Media: "attach://edited_file", MediaAttachment: bytes.NewReader(f.Data),
			Caption: caption, ParseMode: models.ParseModeHTML,
		}
	case "animation":
		media = &models.InputMediaAnimation{
			Media: "attach://edited_file", MediaAttachment: bytes.NewReader(f.Data),
			Caption: caption, ParseMode: models.ParseModeHTML,
		}
	default:
		media = &models.InputMediaDocument{
			Media: "attach://edited_file", MediaAttachment: bytes.NewReader(f.Data),
			Caption: caption, ParseMode: models.ParseModeHTML,
		}
	}
	_, err = c.bot.EditMessageMedia(ctx, &bot.EditMessageMediaParams{ChatID: c.chatID, MessageID: n, Media: media})
	return err
}
