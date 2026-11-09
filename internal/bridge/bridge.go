package bridge

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/sleroq/zefxi/internal/richtext"
)

// Bridge connects one Telegram chat to one Discord channel.
type Bridge struct {
	store           Store
	telegram        TelegramClient
	discord         DiscordClient
	failedReactions map[string]bool
	discordPins     map[string]bool
	mu              sync.Mutex
}

func New(store Store, telegram TelegramClient, discord DiscordClient) *Bridge {
	return &Bridge{store: store, telegram: telegram, discord: discord, failedReactions: make(map[string]bool)}
}

func (b *Bridge) HandleMessage(ctx context.Context, source Platform, msg Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	out, target, err := b.convertMessage(ctx, source, msg)
	if err != nil {
		return err
	}

	ids, sendErr := target.Send(ctx, out)
	if msg.ID == "" {
		return sendErr
	}
	for _, id := range ids {
		link := MessageLink{Origin: source, AuthorID: msg.Author.ID}
		if source == Telegram {
			link.TelegramID, link.DiscordID = msg.ID, id
		} else {
			link.TelegramID, link.DiscordID = id, msg.ID
		}
		if err := b.store.LinkMessage(ctx, link); err != nil {
			return fmt.Errorf("save forwarded message: %w", err)
		}
	}
	return sendErr
}

func (b *Bridge) HandleDiscordDelete(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	links, err := b.store.MessageLinks(ctx, Discord, id)
	if err != nil {
		return err
	}
	var result error
	for _, link := range links {
		if link.Origin != Discord {
			continue
		}
		if err := b.telegram.Delete(ctx, link.TelegramID); err != nil {
			result = errors.Join(result, fmt.Errorf("delete telegram message %s: %w", link.TelegramID, err))
			continue
		}
		if err := b.store.DeleteMessageLink(ctx, link); err != nil {
			result = errors.Join(result, fmt.Errorf("delete message link: %w", err))
		}
	}
	return result
}

func (b *Bridge) HandleEdit(ctx context.Context, source Platform, msg Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	links, err := b.store.MessageLinks(ctx, source, msg.ID)
	if err != nil || len(links) == 0 {
		return err
	}
	if links[0].Origin != source {
		return nil
	}
	if source == Discord && len(links) != 1 {
		return fmt.Errorf("edit of multipart forwarded message is not supported")
	}
	out, target, err := b.convertMessage(ctx, source, msg)
	if err != nil {
		return err
	}
	if source == Discord {
		return target.Edit(ctx, links[0].TelegramID, out)
	}
	ids := make([]string, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.DiscordID)
	}
	ids, editErr := b.discord.EditParts(ctx, ids, out)
	if err := b.store.ReplaceDiscordLinks(ctx, links[0], ids); err != nil {
		return errors.Join(editErr, fmt.Errorf("save edited message links: %w", err))
	}
	return editErr
}

func (b *Bridge) convertMessage(ctx context.Context, source Platform, msg Message) (OutboundMessage, Messenger, error) {
	author, telegramID := msg.Author, ""
	if msg.Author.ID != "" {
		if err := b.store.RememberUser(ctx, source, msg.Author); err != nil {
			return OutboundMessage{}, nil, err
		}
		if err := b.matchUser(ctx, source, msg.Author); err != nil {
			return OutboundMessage{}, nil, err
		}
		var err error
		author, telegramID, err = b.outboundAuthor(ctx, source, msg.Author)
		if err != nil {
			return OutboundMessage{}, nil, err
		}
	}
	out := OutboundMessage{
		Author: author, TelegramUserID: telegramID, Attachments: msg.Attachments,
		PreserveAttachments: msg.PreserveAttachments,
	}
	if msg.ReplyTo != "" {
		link, ok, err := b.store.MessageLink(ctx, source, msg.ReplyTo)
		if err != nil {
			return OutboundMessage{}, nil, err
		}
		if ok {
			if source == Telegram {
				out.ReplyTo = link.DiscordID
			} else {
				out.ReplyTo = link.TelegramID
			}
		}
	}

	var mentionErr error
	resolve := func(entity richtext.Entity, visible string) string {
		if mentionErr != nil {
			return ""
		}
		var result string
		result, mentionErr = b.mention(ctx, source, entity, visible)
		if source == Telegram && strings.HasPrefix(result, "<@") && strings.HasSuffix(result, ">") {
			out.MentionIDs = append(out.MentionIDs, result[2:len(result)-1])
		}
		return result
	}
	var target Messenger
	if source == Telegram {
		out.Text = richtext.ToDiscord(msg.Text, resolve)
		target = b.discord
	} else {
		out.Text = richtext.ToTelegram(msg.Text, resolve)
		target = b.telegram
	}
	if mentionErr != nil {
		return OutboundMessage{}, nil, mentionErr
	}

	return out, target, nil
}

func (b *Bridge) outboundAuthor(ctx context.Context, source Platform, author User) (User, string, error) {
	linked, found, err := b.store.LinkedUser(ctx, source, author.ID)
	if err != nil || !found {
		return author, "", err
	}
	if source == Telegram {
		return linked, "", nil
	}
	if linked.Name != "" {
		author.Name = linked.Name
	}
	return author, linked.ID, nil
}

// matchUser only makes an automatic match when neither account is already linked.
func (b *Bridge) matchUser(ctx context.Context, source Platform, user User) error {
	_, linked, err := b.store.LinkedUser(ctx, source, user.ID)
	if err != nil || linked || user.Username == "" {
		return err
	}

	var telegramUser, discordUser User
	if source == Telegram {
		telegramUser = user
		// Prefix the query so an all-numeric username is searched as a name, not an ID.
		users, err := b.discord.FindUsers(ctx, "@"+user.Username)
		if err != nil {
			slog.Warn("Discord user lookup failed; forwarding with Telegram identity", "user", user.Username, "error", err)
			return nil
		}
		for _, candidate := range users {
			if strings.EqualFold(candidate.Username, user.Username) {
				if discordUser.ID != "" {
					return nil
				}
				discordUser = candidate
			}
		}
		if discordUser.ID == "" {
			return nil
		}
		if err := b.store.RememberUser(ctx, Discord, discordUser); err != nil {
			return err
		}
	} else {
		discordUser = user
		var found bool
		telegramUser, found, err = b.store.User(ctx, Telegram, user.Username)
		if err != nil || !found {
			return err
		}
	}

	disabled, err := b.store.AutoLinkDisabled(ctx, telegramUser.ID)
	if err != nil || disabled {
		return err
	}
	_, linked, err = b.store.LinkedUser(ctx, other(source), oppositeID(source, telegramUser, discordUser))
	if err != nil || linked {
		return err
	}
	return b.store.LinkUsers(ctx, telegramUser.ID, discordUser.ID)
}

func other(p Platform) Platform {
	if p == Telegram {
		return Discord
	}
	return Telegram
}

func oppositeID(source Platform, telegramUser, discordUser User) string {
	if source == Telegram {
		return discordUser.ID
	}
	return telegramUser.ID
}

func (b *Bridge) mention(ctx context.Context, source Platform, entity richtext.Entity, visible string) (string, error) {
	key := entity.UserID
	if key == "" {
		key = strings.TrimPrefix(visible, "@")
	}

	user, found, err := b.store.User(ctx, source, key)
	if err != nil {
		return "", err
	}
	if !found && source == Discord && entity.UserID != "" {
		users, findErr := b.discord.FindUsers(ctx, entity.UserID)
		if findErr != nil {
			slog.Warn("Discord mention lookup failed", "user_id", entity.UserID, "error", findErr)
			return "", nil
		}
		if len(users) == 1 {
			user, found = users[0], true
			if err := b.store.RememberUser(ctx, Discord, user); err != nil {
				return "", err
			}
		}
	}
	if !found {
		return "", nil
	}

	if err := b.matchUser(ctx, source, user); err != nil {
		return "", err
	}
	linked, found, err := b.store.LinkedUser(ctx, source, user.ID)
	if err != nil {
		return "", err
	}
	if !found {
		if source == Discord {
			return "@" + html.EscapeString(user.Name), nil
		}
		return "", nil
	}
	if source == Telegram {
		return "<@" + linked.ID + ">", nil
	}
	name := linked.Name
	if linked.Username != "" {
		name = "@" + linked.Username
	}
	return `<a href="tg://user?id=` + linked.ID + `">` + html.EscapeString(name) + `</a>`, nil
}

// LinkCandidates is used by the Telegram /link command's selection buttons.
func (b *Bridge) LinkCandidates(ctx context.Context, user User, query string) ([]LinkCandidate, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.store.RememberUser(ctx, Telegram, user); err != nil {
		return nil, err
	}
	users, err := b.discord.GuildMembers(ctx)
	if err != nil {
		return nil, err
	}

	query = strings.TrimSpace(query)
	names := []string{user.Name, user.Username}
	if query != "" {
		names = []string{strings.TrimPrefix(query, "@")}
	}

	linkedUsers, err := b.store.LinkedUsers(ctx, Telegram, user.ID)
	if err != nil {
		return nil, err
	}
	candidates := make([]LinkCandidate, 0, len(users))
	for _, member := range users {
		linked := false
		for _, existing := range linkedUsers {
			if existing.ID == member.ID {
				linked = true
				break
			}
		}
		if linked {
			continue
		}
		score := 0
		if query != "" && query == member.ID {
			score = 100
		}
		for _, name := range names {
			for _, other := range []string{member.Name, member.Username} {
				score = max(score, similarity(name, other))
			}
		}
		candidates = append(candidates, LinkCandidate{User: member, MatchPercent: score})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].MatchPercent != candidates[j].MatchPercent {
			return candidates[i].MatchPercent > candidates[j].MatchPercent
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates, nil
}

func similarity(a, b string) int {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	x, y := []rune(a), []rune(b)
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	row := make([]int, len(y)+1)
	for j := range row {
		row[j] = j
	}
	for i, r := range x {
		prev := row[0]
		row[0] = i + 1
		for j, s := range y {
			old := row[j+1]
			cost := 0
			if r != s {
				cost = 1
			}
			row[j+1] = min(row[j+1]+1, row[j]+1, prev+cost)
			prev = old
		}
	}
	return (max(len(x), len(y)) - row[len(y)]) * 100 / max(len(x), len(y))
}

func (b *Bridge) UnlinkUser(ctx context.Context, telegramID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.store.UnlinkUser(ctx, telegramID)
}

func (b *Bridge) Whois(ctx context.Context, source Platform, messageID string, author User) (Identity, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	link, found, err := b.store.MessageLink(ctx, source, messageID)
	if err != nil {
		return Identity{}, err
	}
	if found {
		source = link.Origin
		author, found, err = b.store.User(ctx, source, link.AuthorID)
		if err != nil || !found {
			return Identity{}, err
		}
	}
	if author.ID == "" {
		return Identity{}, nil
	}

	linked, _, err := b.store.LinkedUser(ctx, source, author.ID)
	if err != nil {
		return Identity{}, err
	}
	if source == Telegram {
		return Identity{Telegram: author, Discord: linked}, nil
	}
	return Identity{Telegram: linked, Discord: author}, nil
}

func (b *Bridge) LinkUsers(ctx context.Context, telegramID, discordID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	users, err := b.discord.FindUsers(ctx, discordID)
	if err != nil {
		return err
	}
	if len(users) != 1 || users[0].ID != discordID {
		return fmt.Errorf("discord member %s not found", discordID)
	}
	if err := b.store.RememberUser(ctx, Discord, users[0]); err != nil {
		return err
	}
	return b.store.LinkUsers(ctx, telegramID, discordID)
}

// LinkView resolves the original message author and their direct links.
func (b *Bridge) LinkView(ctx context.Context, messageID string, fallback User, query string) (LinkView, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	view := LinkView{Platform: Telegram, Author: fallback}
	link, found, err := b.store.MessageLink(ctx, Telegram, messageID)
	if err != nil {
		return view, err
	}
	if found {
		view.Platform = link.Origin
		view.Author, found, err = b.store.User(ctx, link.Origin, link.AuthorID)
		if err != nil {
			return view, err
		}
		if !found {
			view.Author = User{}
		}
	}
	if view.Author.ID == "" {
		return view, nil
	}
	if err = b.store.RememberUser(ctx, view.Platform, view.Author); err != nil {
		return view, err
	}

	view.Linked, err = b.store.LinkedUsers(ctx, view.Platform, view.Author.ID)
	if err != nil {
		return view, err
	}
	var users []User
	if view.Platform == Telegram {
		users, err = b.discord.GuildMembers(ctx)
	} else {
		users, err = b.store.Users(ctx, Telegram)
	}
	if err != nil {
		return view, err
	}

	names := []string{view.Author.Name, view.Author.Username}
	query = strings.TrimSpace(query)
	if query != "" {
		names = []string{strings.TrimPrefix(query, "@")}
	}
	for _, u := range users {
		linked := false
		for _, existing := range view.Linked {
			if existing.ID == u.ID {
				linked = true
				break
			}
		}
		if linked {
			continue
		}
		score := 0
		if query != "" && query == u.ID {
			score = 100
		}
		for _, name := range names {
			for _, other := range []string{u.Name, u.Username} {
				score = max(score, similarity(name, other))
			}
		}
		view.Candidates = append(view.Candidates, LinkCandidate{User: u, MatchPercent: score})
	}
	sort.Slice(view.Candidates, func(i, j int) bool {
		a, b := view.Candidates[i], view.Candidates[j]
		if a.MatchPercent != b.MatchPercent {
			return a.MatchPercent > b.MatchPercent
		}
		return a.ID < b.ID
	})
	return view, nil
}

func (b *Bridge) UnlinkPair(ctx context.Context, telegramID, discordID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.store.UnlinkPair(ctx, telegramID, discordID)
}
