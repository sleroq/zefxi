package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/richtext"
)

const telegramDownloadLimit int64 = 50 * 1024 * 1024

type Client struct {
	session                                     *discordgo.Session
	guildID, channelID, webhookID, webhookToken string
	maxUploadBytes                              int64
	http                                        *http.Client
}

var _ bridge.DiscordClient = (*Client)(nil)

func New(token, guildID, channelID string, maxUploadBytes int64) (*Client, error) {
	if token == "" || guildID == "" || channelID == "" || maxUploadBytes <= 0 {
		return nil, errors.New("discord: token, guild, channel and positive upload limit required")
	}
	s, err := discordgo.New("Bot " + strings.TrimPrefix(token, "Bot "))
	if err != nil {
		return nil, errors.New("discord: create session failed")
	}
	s.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions | discordgo.IntentsGuildMembers
	// Keep gateway event order: a reply or reaction needs the preceding message mapping.
	s.SyncEvents = true
	s.State.MaxMessageCount = 1000
	return &Client{
		session: s, guildID: guildID, channelID: channelID,
		maxUploadBytes: maxUploadBytes, http: &http.Client{Timeout: time.Minute},
	}, nil
}

func (c *Client) Run(ctx context.Context, h bridge.Handler) error {
	ctx, cancel := context.WithCancel(ctx)
	queue := newEventQueue(ctx)
	defer func() {
		cancel()
		<-queue.done
	}()

	pending := newGIFLookups(3 * time.Second)
	removeMessage := c.session.AddHandler(func(_ *discordgo.Session, event *discordgo.Event) {
		if event.Type != "MESSAGE_CREATE" && event.Type != "MESSAGE_UPDATE" {
			return
		}
		message, err := decodeGIFMessage(event.RawData)
		if err != nil || message.GuildID != c.guildID || message.ChannelID != c.channelID {
			return
		}
		if event.Type == "MESSAGE_UPDATE" {
			c.queueEdit(ctx, h, queue, pending, event, message)
			return
		}
		c.queueMessage(ctx, h, queue, pending, message)
	})
	defer removeMessage()

	defer c.registerDeletes(ctx, queue, h)()

	removeMember := c.session.AddHandler(func(_ *discordgo.Session, e *discordgo.GuildMemberRemove) {
		if message, ok := c.memberLeave(e); ok {
			queue.enqueue(ctx, func() { forwardSystem(ctx, h, message) })
		}
	})
	defer removeMember()
	removeChannel := c.session.AddHandler(func(_ *discordgo.Session, e *discordgo.ChannelUpdate) {
		if message, ok := c.channelRename(e); ok {
			queue.enqueue(ctx, func() { forwardSystem(ctx, h, message) })
		}
	})
	defer removeChannel()

	reaction := func(e *discordgo.MessageReaction, added bool) {
		if e.GuildID != c.guildID || e.ChannelID != c.channelID || (c.session.State.User != nil && e.UserID == c.session.State.User.ID) {
			return
		}
		event := bridge.Reaction{MessageID: e.MessageID, UserID: e.UserID, Emoji: e.Emoji.APIName(), Added: added}
		queue.enqueue(ctx, func() {
			if err := h.HandleReaction(ctx, bridge.Discord, event); err != nil {
				slog.Error("discord reaction handler failed", "error", err)
			}
		})
	}

	removeAdd := c.session.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageReactionAdd) {
		reaction(e.MessageReaction, true)
	})
	defer removeAdd()
	removeRemove := c.session.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageReactionRemove) {
		reaction(e.MessageReaction, false)
	})
	defer removeRemove()

	removePins := c.session.AddHandler(func(_ *discordgo.Session, e *discordgo.ChannelPinsUpdate) {
		if e.GuildID != c.guildID || e.ChannelID != c.channelID {
			return
		}
		queue.enqueue(ctx, func() {
			if err := h.RefreshDiscordPins(ctx); err != nil {
				slog.Error("discord pins update failed", "error", err)
			}
		})
	})
	defer removePins()

	if err := h.RefreshDiscordPins(ctx); err != nil {
		slog.Error("discord pins baseline failed", "error", err)
	}
	if err := c.session.Open(); err != nil {
		return errors.New("discord: gateway connection failed")
	}
	<-ctx.Done()
	return c.session.Close()
}

func (c *Client) ownSender(m *discordgo.Message) bool {
	return (m.Author != nil && c.session.State.User != nil && m.Author.ID == c.session.State.User.ID) ||
		(c.webhookID != "" && m.WebhookID == c.webhookID)
}

func (c *Client) acceptsMessage(m *discordgo.Message) bool {
	if m.Author == nil || c.ownSender(m) {
		return false
	}
	switch m.Type {
	case discordgo.MessageTypeDefault, discordgo.MessageTypeReply, discordgo.MessageTypeChatInputCommand, discordgo.MessageTypeContextMenuCommand:
		return true
	default:
		return false
	}
}

func (c *Client) queueMessage(ctx context.Context, h bridge.Handler, queue *eventQueue, pending *gifLookups, message *discordgo.Message) {
	if c.ownSender(message) {
		return
	}
	if system, ok := c.systemMessage(message); ok {
		queue.enqueue(ctx, func() { forwardSystem(ctx, h, system) })
		return
	}
	if !c.acceptsMessage(message) {
		return
	}
	e := &discordgo.MessageCreate{Message: message}
	lookup := pending.register(e)
	queue.enqueue(ctx, func() {
		pending.await(ctx, e, lookup)
		c.handleMessage(ctx, h, e)
	})
}

func (c *Client) queueEdit(ctx context.Context, h bridge.Handler, queue *eventQueue, pending *gifLookups, event *discordgo.Event, message *discordgo.Message) {
	pending.deliver(&discordgo.MessageUpdate{Message: message})
	embedOnly := !isContentUpdate(event.RawData)
	if embedOnly && !hasUpdateField(event.RawData, "embeds") {
		return
	}
	preserve := preserveEditedMedia(event, message)
	queue.enqueue(ctx, func() {
		c.handleEdit(ctx, h, message.ID, preserve, embedOnly)
	})
}

func (c *Client) handleEdit(ctx context.Context, h bridge.Handler, id string, preserveAttachments, embedOnly bool) {
	full, err := c.session.ChannelMessage(c.channelID, id, discordgo.WithContext(ctx))
	if err != nil {
		slog.Error("discord edited message fetch failed", "error", err)
		return
	}
	if !c.acceptsMessage(full) || (embedOnly && !full.Author.Bot && full.WebhookID == "") {
		return
	}
	c.forwardMessage(ctx, h, &discordgo.MessageCreate{Message: full}, true, preserveAttachments)
}

func (c *Client) handleMessage(ctx context.Context, h bridge.Handler, e *discordgo.MessageCreate) {
	c.forwardMessage(ctx, h, e, false, false)
}

func hasUpdateField(raw []byte, name string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, ok := fields[name]
	return ok
}

func preserveEditedMedia(event *discordgo.Event, message *discordgo.Message) bool {
	update, ok := event.Struct.(*discordgo.MessageUpdate)
	if !ok || update.BeforeUpdate == nil {
		return !hasUpdateField(event.RawData, "attachments")
	}
	before := update.BeforeUpdate
	if hasUpdateField(event.RawData, "attachments") {
		if !slices.EqualFunc(before.Attachments, message.Attachments, func(a, b *discordgo.MessageAttachment) bool {
			return a.ID == b.ID
		}) {
			return false
		}
	}
	if hasUpdateField(event.RawData, "content") {
		return slices.Equal(animationLinks(before.Content), animationLinks(message.Content))
	}
	return true
}

func animationLinks(content string) []string {
	var links []string
	for _, raw := range gifLinks.FindAllString(content, -1) {
		link := strings.TrimRight(raw, ".,!?;:)")
		if pickerLink(link) || animationURL(link) {
			links = append(links, link)
		}
	}
	return links
}

func isContentUpdate(raw []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, content := fields["content"]
	_, attachments := fields["attachments"]
	return content || attachments
}

func (c *Client) forwardMessage(ctx context.Context, h bridge.Handler, e *discordgo.MessageCreate, edit bool, preserveAttachments bool) {
	if fields := strings.Fields(e.Content); !e.Author.Bot && e.WebhookID == "" && len(fields) > 0 && fields[0] == "/whois" {
		c.handleWhois(ctx, h, e)
		return
	}
	content, animations := c.downloadGIFs(ctx, e.Message)
	if e.Author.Bot || e.WebhookID != "" {
		content = appendRichEmbeds(content, e.Embeds)
	}
	m := bridge.Message{
		ID: e.ID, Author: memberUser(e.Author, e.Member, c.guildID),
		Text: richtext.FromDiscord(content), Attachments: animations, PreserveAttachments: preserveAttachments,
	}
	if e.MessageReference != nil {
		m.ReplyTo = e.MessageReference.MessageID
	}

	for _, a := range e.Attachments {
		attachment, err := c.download(ctx, a)
		if err != nil {
			slog.Error("discord attachment download failed", "message", e.ID, "error", err)
		}
		m.Attachments = append(m.Attachments, attachment)
	}
	for _, sticker := range e.StickerItems {
		var extension, contentType string
		var format bridge.StickerFormat
		switch sticker.FormatType {
		case discordgo.StickerFormatTypePNG:
			extension, contentType, format = ".png", "image/png", bridge.StickerPNG
		case discordgo.StickerFormatTypeAPNG:
			extension, contentType, format = ".png", "image/png", bridge.StickerAPNG
		case discordgo.StickerFormatTypeGIF:
			extension, contentType, format = ".gif", "image/gif", bridge.StickerGIF
		case discordgo.StickerFormatTypeLottie:
			extension, contentType, format = ".json", "application/json", bridge.StickerLottie
		default:
			continue
		}
		attachment, err := c.download(ctx, &discordgo.MessageAttachment{
			Filename: sticker.Name + extension, ContentType: contentType,
			URL: "https://cdn.discordapp.com/stickers/" + sticker.ID + extension,
		})
		attachment.StickerFormat = format
		if err != nil {
			slog.Error("discord sticker download failed", "message", e.ID, "error", err)
		}
		m.Attachments = append(m.Attachments, attachment)
	}

	var err error
	if edit {
		err = h.HandleEdit(ctx, bridge.Discord, m)
	} else {
		err = h.HandleMessage(ctx, bridge.Discord, m)
	}
	if err != nil {
		slog.Error("discord message handler failed", "error", err)
	}
}

func memberUser(u *discordgo.User, m *discordgo.Member, guildID string) bridge.User {
	result := bridge.User{ID: u.ID, Username: u.Username, Name: u.DisplayName(), AvatarURL: u.AvatarURL("256")}
	if m != nil {
		copyMember := *m
		copyMember.User = u
		copyMember.GuildID = guildID
		result.Name = copyMember.DisplayName()
		result.AvatarURL = copyMember.AvatarURL("256")
	}
	return result
}

func (c *Client) download(ctx context.Context, a *discordgo.MessageAttachment) (bridge.Attachment, error) {
	result := bridge.Attachment{
		Name: a.Filename, ContentType: a.ContentType, Size: int64(a.Size), URL: a.URL,
		Animated: strings.EqualFold(strings.Split(a.ContentType, ";")[0], "image/gif"),
	}
	if result.Size > telegramDownloadLimit {
		return result, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return result, errors.New("invalid attachment URL")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return result, errors.New("attachment request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("attachment HTTP status %d", resp.StatusCode)
	}
	if resp.ContentLength > telegramDownloadLimit {
		return result, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, telegramDownloadLimit+1))
	if err != nil {
		return result, errors.New("attachment read failed")
	}
	if int64(len(data)) > telegramDownloadLimit {
		return result, nil
	}
	result.Data = data
	result.Size = int64(len(data))
	return result, nil
}

func shareableURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return ""
	}
	if strings.EqualFold(u.Hostname(), "api.telegram.org") || strings.Contains(u.Path, "/webhooks/") {
		return ""
	}
	return raw
}

func (c *Client) prepareMessage(m bridge.OutboundMessage, bot bool) ([]string, []*discordgo.File, string, error) {

	var content strings.Builder
	content.WriteString(m.Text)
	var files []*discordgo.File

	for _, a := range m.Attachments {
		if len(a.Data) > 0 && int64(len(a.Data)) <= c.maxUploadBytes && a.Size <= c.maxUploadBytes {
			files = append(files, &discordgo.File{Name: a.Name, ContentType: a.ContentType, Reader: bytes.NewReader(a.Data)})
			continue
		}
		if link := shareableURL(a.URL); link != "" {
			content.WriteByte('\n')
			content.WriteString(link)
		} else {
			size := max(a.Size, int64(len(a.Data)))
			if size > c.maxUploadBytes {
				notice := "\n[Attachment unavailable: %s (%d bytes); upload limit %d bytes]"
				fmt.Fprintf(&content, notice, a.Name, size, c.maxUploadBytes)
			} else {
				fmt.Fprintf(&content, "\n[Attachment unavailable: %s (%d bytes)]", a.Name, size)
			}
		}
	}

	name := m.Author.Name
	if name == "" {
		name = m.Author.Username
	}
	username := webhookName(name)
	prefix := ""
	if bot || username == "" {
		prefix = richtext.ToDiscord(richtext.Text{Value: name}, nil) + ": "
	}
	chunks, err := splitContent(content.String(), 2000-utf16Units(prefix))
	if err != nil {
		return nil, nil, "", err
	}
	for i := range chunks {
		chunks[i] = prefix + chunks[i]
	}
	return chunks, files, username, nil
}

func (c *Client) splitVoice(m *bridge.OutboundMessage) []bridge.Attachment {
	var voices, other []bridge.Attachment
	for _, a := range m.Attachments {
		if a.Voice != nil && len(a.Data) > 0 && int64(len(a.Data)) <= c.maxUploadBytes && a.Size <= c.maxUploadBytes {
			voices = append(voices, a)
		} else {
			other = append(other, a)
		}
	}
	m.Attachments = other
	return voices
}

func (c *Client) Send(ctx context.Context, m bridge.OutboundMessage) ([]string, error) {
	voices := c.splitVoice(&m)
	if len(voices) > 0 && m.Text == "" && len(m.Attachments) == 0 {
		m.Text = "[Voice message]"
	}
	chunks, files, username, err := c.prepareMessage(m, m.ReplyTo != "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		id, err := c.sendChunk(ctx, m, username, chunk, files)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
		files = nil
	}
	for _, voice := range voices {
		id, err := c.sendVoice(ctx, voice)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (c *Client) sendVoice(ctx context.Context, a bridge.Attachment) (string, error) {
	waveform := a.Voice.Waveform
	if len(waveform) == 0 {
		waveform = []byte{32, 64, 96, 128, 96, 64, 32, 64, 96, 128, 96, 64}
	}
	payload := struct {
		Flags       discordgo.MessageFlags `json:"flags"`
		Attachments []struct {
			ID       int    `json:"id"`
			Filename string `json:"filename"`
			Duration int    `json:"duration_secs"`
			Waveform []byte `json:"waveform"`
		} `json:"attachments"`
	}{Flags: discordgo.MessageFlagsIsVoiceMessage}
	payload.Attachments = append(payload.Attachments, struct {
		ID       int    `json:"id"`
		Filename string `json:"filename"`
		Duration int    `json:"duration_secs"`
		Waveform []byte `json:"waveform"`
	}{0, a.Name, a.Voice.Duration, waveform})
	file := &discordgo.File{Name: a.Name, ContentType: a.ContentType, Reader: bytes.NewReader(a.Data)}
	contentType, body, err := discordgo.MultipartBodyWithJSON(payload, []*discordgo.File{file})
	if err != nil {
		return "", err
	}
	endpoint := discordgo.EndpointChannelMessages(c.channelID)
	response, err := c.session.RequestRaw(
		http.MethodPost, endpoint, contentType, body, endpoint, 0, discordgo.WithContext(ctx),
	)
	if err != nil {
		return "", errors.New("discord: send voice message failed")
	}
	var sent discordgo.Message
	if err := json.Unmarshal(response, &sent); err != nil {
		return "", err
	}
	return sent.ID, nil
}

func (c *Client) sendChunk(ctx context.Context, m bridge.OutboundMessage, name, content string, files []*discordgo.File) (string, error) {
	mentions := &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}, Users: m.MentionIDs}
	var sent *discordgo.Message
	var err error
	if m.ReplyTo != "" {
		params := &discordgo.MessageSend{
			Content: content, Files: files, AllowedMentions: mentions,
			Reference: &discordgo.MessageReference{MessageID: m.ReplyTo, ChannelID: c.channelID, GuildID: c.guildID},
		}
		sent, err = c.session.ChannelMessageSendComplex(c.channelID, params, discordgo.WithContext(ctx))
	} else {
		params := &discordgo.WebhookParams{
			Content: content, Username: name, AvatarURL: shareableURL(m.Author.AvatarURL),
			Files: files, AllowedMentions: mentions,
		}
		sent, err = c.session.WebhookExecute(c.webhookID, c.webhookToken, true, params, discordgo.WithContext(ctx))
	}
	if err != nil {
		if apiErr, ok := errors.AsType[*discordgo.RESTError](err); ok {
			return "", fmt.Errorf("discord: send message failed: HTTP %d", apiErr.Response.StatusCode)
		}
		// Transport errors may contain the secret webhook URL.
		return "", errors.New("discord: send message failed")
	}
	return sent.ID, nil
}

func (c *Client) GuildMembers(ctx context.Context) ([]bridge.User, error) {
	var users []bridge.User
	after := ""
	for {
		members, err := c.session.GuildMembers(c.guildID, after, 1000, discordgo.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if member.User != nil && !member.User.Bot {
				users = append(users, memberUser(member.User, member, c.guildID))
			}
		}
		if len(members) < 1000 {
			return users, nil
		}
		after = members[len(members)-1].User.ID
	}
}

func (c *Client) FindUsers(ctx context.Context, query string) ([]bridge.User, error) {
	query = strings.TrimSpace(query)
	id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(query, "<@"), "!"), ">")
	numeric := id != ""
	for _, r := range id {
		if r < '0' || r > '9' {
			numeric = false
			break
		}
	}
	var members []*discordgo.Member
	if numeric {
		member, err := c.session.GuildMember(c.guildID, id, discordgo.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		members = []*discordgo.Member{member}
	} else {
		if query == "" {
			return nil, nil
		}
		var err error
		search := strings.TrimPrefix(query, "@")
		members, err = c.session.GuildMembersSearch(c.guildID, search, 1000, discordgo.WithContext(ctx))
		if err != nil {
			return nil, err
		}
	}
	users := make([]bridge.User, 0, len(members))
	for _, m := range members {
		if m.User != nil && !m.User.Bot {
			users = append(users, memberUser(m.User, m, c.guildID))
		}
	}
	return users, nil
}

func (c *Client) SetReaction(ctx context.Context, messageID, emoji string, added bool) error {
	if strings.HasPrefix(emoji, "<:") || strings.HasPrefix(emoji, "<a:") {
		emoji = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(emoji, "<a:"), "<:"), ">")
	}
	if added {
		return c.session.MessageReactionAdd(c.channelID, messageID, emoji, discordgo.WithContext(ctx))
	}
	return c.session.MessageReactionRemove(c.channelID, messageID, emoji, "@me", discordgo.WithContext(ctx))
}

func (c *Client) Edit(ctx context.Context, id string, m bridge.OutboundMessage) error {
	_, err := c.EditParts(ctx, []string{id}, m)
	return err
}

// EditParts returns the IDs that still exist even when an operation fails.
func (c *Client) EditParts(ctx context.Context, ids []string, m bridge.OutboundMessage) ([]string, error) {
	voices := c.splitVoice(&m)

	var ordinary, existingVoice []string
	for i, id := range ids {
		if i == 0 {
			ordinary = append(ordinary, id)
			continue
		}
		message, err := c.session.ChannelMessage(c.channelID, id, discordgo.WithContext(ctx))
		if err != nil {
			return ids, err
		}
		if message.Flags&discordgo.MessageFlagsIsVoiceMessage != 0 {
			existingVoice = append(existingVoice, id)
		} else {
			ordinary = append(ordinary, id)
		}
	}
	keepVoice := m.PreserveAttachments || len(voices) > 0
	if keepVoice && len(existingVoice) > 0 && m.Text == "" && len(m.Attachments) == 0 {
		m.Text = "[Voice message]"
	}
	ordinary, err := c.editOrdinaryParts(ctx, ordinary, m)
	ids = slices.Concat(ordinary, existingVoice)
	if err != nil {
		return ids, err
	}
	if !keepVoice {
		for len(ids) > len(ordinary) {
			if err := c.Delete(ctx, ids[len(ids)-1]); err != nil {
				return ids, err
			}
			ids = ids[:len(ids)-1]
		}
	} else if len(existingVoice) == 0 {
		for _, voice := range voices {
			id, err := c.sendVoice(ctx, voice)
			if err != nil {
				return ids, err
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *Client) editOrdinaryParts(ctx context.Context, ids []string, m bridge.OutboundMessage) ([]string, error) {
	ids = slices.Clone(ids)

	first, err := c.session.ChannelMessage(c.channelID, ids[0], discordgo.WithContext(ctx))
	if err != nil {
		return ids, err
	}
	if first.WebhookID == "" && first.MessageReference != nil {
		m.ReplyTo = first.MessageReference.MessageID
	} else if first.WebhookID != "" {
		m.ReplyTo = ""
	}
	chunks, files, username, err := c.prepareMessage(m, first.WebhookID == "")
	if err != nil {
		return ids, err
	}
	mentions := &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}, Users: m.MentionIDs}
	for i := 0; i < min(len(ids), len(chunks)); i++ {
		attachments := []*discordgo.MessageAttachment{}
		if first.WebhookID != "" {
			params := &discordgo.WebhookEdit{
				Content: &chunks[i], Files: files, Attachments: &attachments, AllowedMentions: mentions,
			}
			_, err = c.session.WebhookMessageEdit(c.webhookID, c.webhookToken, ids[i], params, discordgo.WithContext(ctx))
		} else {
			params := &discordgo.MessageEdit{
				Channel: c.channelID, ID: ids[i], Content: &chunks[i], Files: files,
				Attachments: &attachments, AllowedMentions: mentions,
			}
			_, err = c.session.ChannelMessageEditComplex(params, discordgo.WithContext(ctx))
		}
		if err != nil {
			return ids, errors.New("discord: edit message failed")
		}
		files = nil
	}
	for len(ids) < len(chunks) {
		id, err := c.sendChunk(ctx, m, username, chunks[len(ids)], nil)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	for len(ids) > len(chunks) {
		id := ids[len(ids)-1]
		if first.WebhookID != "" {
			err = c.session.WebhookMessageDelete(c.webhookID, c.webhookToken, id, discordgo.WithContext(ctx))
		} else {
			err = c.session.ChannelMessageDelete(c.channelID, id, discordgo.WithContext(ctx))
		}
		if err != nil {
			return ids, errors.New("discord: delete message failed")
		}
		ids = ids[:len(ids)-1]
	}
	return ids, nil
}

func (c *Client) Delete(ctx context.Context, id string) error {
	if err := c.session.ChannelMessageDelete(c.channelID, id, discordgo.WithContext(ctx)); err != nil {
		return errors.New("discord: delete message failed")
	}
	return nil
}

func (c *Client) PinnedMessages(ctx context.Context) ([]string, error) {
	messages, err := c.session.ChannelMessagesPinned(c.channelID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(messages))
	for _, m := range messages {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func (c *Client) SetPin(ctx context.Context, id string, pinned bool) error {
	if pinned {
		return c.session.ChannelMessagePin(c.channelID, id, discordgo.WithContext(ctx))
	}
	return c.session.ChannelMessageUnpin(c.channelID, id, discordgo.WithContext(ctx))
}

func (c *Client) registerDeletes(ctx context.Context, queue *eventQueue, h bridge.Handler) func() {
	deleteMessages := func(guildID, channelID string, ids []string) {
		if guildID != c.guildID || channelID != c.channelID {
			return
		}
		queue.enqueue(ctx, func() {
			for _, id := range ids {
				if err := h.HandleDiscordDelete(ctx, id); err != nil {
					slog.Error("discord delete handler failed", "message", id, "error", err)
				}
			}
		})
	}
	removeDelete := c.session.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageDelete) {
		deleteMessages(e.GuildID, e.ChannelID, []string{e.ID})
	})
	removeBulkDelete := c.session.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageDeleteBulk) {
		deleteMessages(e.GuildID, e.ChannelID, e.Messages)
	})

	return func() {
		removeDelete()
		removeBulkDelete()
	}
}
