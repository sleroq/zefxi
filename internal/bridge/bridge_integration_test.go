package bridge_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/richtext"
	"github.com/sleroq/zefxi/internal/store"
)

type sentReaction struct {
	id, emoji string
	added     bool
}

type sentPin struct {
	id     string
	pinned bool
}

type messenger struct {
	prefix     string
	messages   []bridge.OutboundMessage
	reactions  []sentReaction
	edits      []bridge.OutboundMessage
	editIDs    []string
	failNext   bool
	pins       []sentPin
	deleted    []string
	deleteFail string
}

func (m *messenger) Send(_ context.Context, message bridge.OutboundMessage) ([]string, error) {
	m.messages = append(m.messages, message)
	return []string{fmt.Sprintf("%s%d", m.prefix, len(m.messages))}, nil
}
func (m *messenger) Edit(_ context.Context, id string, message bridge.OutboundMessage) error {
	m.editIDs = append(m.editIDs, id)
	m.edits = append(m.edits, message)
	return nil
}
func (m *messenger) SetReaction(_ context.Context, id, emoji string, added bool) error {
	m.reactions = append(m.reactions, sentReaction{id, emoji, added})
	if m.failNext {
		m.failNext = false
		return fmt.Errorf("reaction delivery failed")
	}
	return nil
}

func (m *messenger) Delete(_ context.Context, id string) error {
	m.deleted = append(m.deleted, id)
	if m.deleteFail == id {
		return fmt.Errorf("delete failed")
	}
	return nil
}

func (m *messenger) SetPin(_ context.Context, id string, pinned bool) error {
	m.pins = append(m.pins, sentPin{id, pinned})
	return nil
}

type discord struct {
	messenger
	users      []bridge.User
	reactors   []bridge.ReactionUsers
	reactionID string
	lookupErr  error
	lastQuery  string
	pinnedIDs  []string
	editResult []string
	editInput  []string
	editErr    error
}

func (d *discord) EditParts(ctx context.Context, ids []string, message bridge.OutboundMessage) ([]string, error) {
	d.editInput = append([]string(nil), ids...)
	if err := d.Edit(ctx, ids[0], message); err != nil {
		return ids, err
	}
	if d.editResult != nil {
		return d.editResult, d.editErr
	}
	return ids, d.editErr
}

func (d *discord) PinnedMessages(context.Context) ([]string, error) { return d.pinnedIDs, nil }

func (d *discord) GuildMembers(context.Context) ([]bridge.User, error) { return d.users, d.lookupErr }
func (d *discord) FindUsers(_ context.Context, query string) ([]bridge.User, error) {
	d.lastQuery = query
	if d.lookupErr != nil {
		return nil, d.lookupErr
	}
	query = strings.TrimPrefix(query, "@")
	var matches []bridge.User
	for _, user := range d.users {
		if user.ID == query || strings.Contains(strings.ToLower(user.Username), strings.ToLower(query)) {
			matches = append(matches, user)
		}
	}
	return matches, nil
}
func (d *discord) ReactionUsers(_ context.Context, messageID string) ([]bridge.ReactionUsers, error) {
	d.reactionID = messageID
	return d.reactors, nil
}

func closeStore(t *testing.T, db *store.Store) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Error(err)
	}
}

func TestDuplicateTelegramMessageIsForwardedOnce(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	dc := &discord{}
	dc.prefix = "dc"
	app := bridge.New(db, &messenger{}, dc)
	msg := bridge.Message{ID: "10", Author: bridge.User{ID: "1"}, Text: richtext.Text{Value: "hello"}}
	for range 2 {
		if err := app.HandleMessage(ctx, bridge.Telegram, msg); err != nil {
			t.Fatal(err)
		}
	}
	if len(dc.messages) != 1 {
		t.Fatalf("forwarded messages: %+v", dc.messages)
	}
}

func TestMessageStillForwardsWhenIdentityLookupFails(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	tg := &messenger{prefix: "tg"}
	dc := &discord{prefix: "dc", lookupErr: fmt.Errorf("lookup unavailable")}
	app := bridge.New(db, tg, dc)
	user := bridge.User{ID: "1", Username: "12345", Name: "Telegram Alice"}
	msg := bridge.Message{ID: "10", Author: user, Text: richtext.Text{Value: "hello"}}
	if err := app.HandleMessage(context.Background(), bridge.Telegram, msg); err != nil {
		t.Fatal(err)
	}
	if dc.lastQuery != "@12345" || len(dc.messages) != 1 || dc.messages[0].Author != user {
		t.Fatalf("lookup=%q messages=%+v", dc.lastQuery, dc.messages)
	}
}

func TestUnlinkedDiscordMentionUsesReadableName(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	tg := &messenger{prefix: "tg"}
	dc := &discord{prefix: "dc", users: []bridge.User{
		{ID: "300", Username: "ada", Name: "Ada & Co"},
	}}
	app := bridge.New(db, tg, dc)
	msg := bridge.Message{
		ID: "10", Author: bridge.User{ID: "200", Username: "bob", Name: "Bob"}, Text: richtext.FromDiscord("hello <@300>"),
	}
	if err := app.HandleMessage(context.Background(), bridge.Discord, msg); err != nil {
		t.Fatal(err)
	}
	if len(tg.messages) != 1 || tg.messages[0].Author.Name != "Bob" || tg.messages[0].Text != "hello @Ada &amp; Co" {
		t.Fatalf("message = %+v", tg.messages)
	}
}

func TestDiscordSenderFallsBackWhenLinkedTelegramNameIsEmpty(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	if err := db.RememberUser(ctx, bridge.Telegram, bridge.User{ID: "1", Username: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := db.LinkUsers(ctx, "1", "100"); err != nil {
		t.Fatal(err)
	}

	tg := &messenger{prefix: "tg"}
	dc := &discord{prefix: "dc"}
	app := bridge.New(db, tg, dc)
	msg := bridge.Message{
		ID:     "10",
		Author: bridge.User{ID: "100", Name: "Discord Alice"},
		Text:   richtext.Text{Value: "hello"},
	}
	if err := app.HandleMessage(ctx, bridge.Discord, msg); err != nil {
		t.Fatal(err)
	}
	if len(tg.messages) != 1 || tg.messages[0].Author.Name != "Discord Alice" {
		t.Fatalf("message = %+v", tg.messages)
	}
}

func TestReactionsAggregateSplitMessagesAndRetryAfterFailure(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	for _, telegramID := range []string{"1", "2"} {
		if err := db.LinkMessage(ctx, bridge.MessageLink{TelegramID: telegramID, DiscordID: "99", Origin: bridge.Discord}); err != nil {
			t.Fatal(err)
		}
	}
	tg := &messenger{prefix: "tg"}
	dc := &discord{prefix: "dc"}
	app := bridge.New(db, tg, dc)
	for _, event := range []bridge.Reaction{
		{MessageID: "1", UserID: "7", Emoji: "👍", Added: true},
		{MessageID: "2", UserID: "7", Emoji: "👍", Added: true},
		{MessageID: "1", UserID: "7", Emoji: "👍", Added: false},
		{MessageID: "2", UserID: "7", Emoji: "👍", Added: false},
	} {
		if err := app.HandleReaction(ctx, bridge.Telegram, event); err != nil {
			t.Fatal(err)
		}
	}
	if len(dc.reactions) != 2 || !dc.reactions[0].added || dc.reactions[1].added {
		t.Fatalf("split-message reactions: %+v", dc.reactions)
	}
	dc.failNext = true
	event := bridge.Reaction{MessageID: "1", UserID: "7", Emoji: "👍", Added: true}
	if err := app.HandleReaction(ctx, bridge.Telegram, event); err == nil {
		t.Fatal("expected reaction delivery failure")
	}
	if err := app.HandleReaction(ctx, bridge.Telegram, event); err != nil || len(dc.reactions) != 4 {
		t.Fatalf("reaction retry: %+v, %v", dc.reactions, err)
	}
}

func TestDiscordReactionsAndSummary(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)

	link := bridge.MessageLink{TelegramID: "tg1", DiscordID: "55", Origin: bridge.Discord}
	if err := db.LinkMessage(ctx, link); err != nil {
		t.Fatal(err)
	}
	tg := &messenger{prefix: "tg"}
	dc := &discord{prefix: "dc"}
	app := bridge.New(db, tg, dc)

	for _, r := range []bridge.Reaction{
		{MessageID: "55", UserID: "100", Emoji: "🔥", Added: true},
		{MessageID: "55", UserID: "200", Emoji: "custom:123", Added: true},
		{MessageID: "55", UserID: "100", Emoji: "🔥", Added: false},
	} {
		if err := app.HandleReaction(ctx, bridge.Discord, r); err != nil {
			t.Fatal(err)
		}
	}
	if len(tg.reactions) != 2 || tg.reactions[0] != (sentReaction{"tg1", "🔥", true}) || tg.reactions[1] != (sentReaction{"tg1", "", false}) {
		t.Fatalf("Discord reactions = %+v", tg.reactions)
	}
	heart := bridge.Reaction{MessageID: "55", UserID: "100", Emoji: "❤", Added: true}
	if err := app.HandleReaction(ctx, bridge.Discord, heart); err != nil || len(tg.reactions) != 3 || tg.reactions[2].emoji != "❤️" {
		t.Fatalf("heart variation: %+v, %v", tg.reactions, err)
	}

	if summary, err := app.ReactionSummary(ctx, "missing"); err != nil || summary.LinkFound {
		t.Fatalf("missing summary = %+v, %v", summary, err)
	}
	if summary, err := app.ReactionSummary(ctx, "tg1"); err != nil || !summary.LinkFound || len(summary.Reactions) != 0 {
		t.Fatalf("empty summary = %+v, %v", summary, err)
	}
	dc.reactors = []bridge.ReactionUsers{
		{Emoji: "🔥", Users: []bridge.User{{ID: "100", Name: "Alice"}, {ID: "200", Name: "Bob"}}},
		{Emoji: ":custom:", Users: []bridge.User{{ID: "300", Name: "Carol"}}},
	}
	want := dc.reactors
	if summary, err := app.ReactionSummary(ctx, "tg1"); err != nil || !summary.LinkFound || !reflect.DeepEqual(summary.Reactions, want) || dc.reactionID != "55" {
		t.Fatalf("summary = %+v, message = %q, error = %v", summary, dc.reactionID, err)
	}
}

func TestBridgePersistsRepliesAndIdentity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bridge.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tg := &messenger{prefix: "tg"}
	dc := &discord{prefix: "dc", users: []bridge.User{
		{ID: "100", Username: "alice", Name: "Alice on Discord", AvatarURL: "https://cdn.example/alice"},
		{ID: "200", Username: "bobby", Name: "Bob on Discord"},
	}}
	app := bridge.New(db, tg, dc)

	alice := bridge.User{ID: "1", Username: "Alice", Name: "Telegram Alice"}
	firstMessage := bridge.Message{
		ID: "10", Author: alice,
		Text:        richtext.Text{Value: "hello", Entities: []richtext.Entity{{Kind: "bold", Start: 0, End: 5}}},
		Attachments: []bridge.Attachment{{Name: "x.txt", Data: []byte("file"), Size: 4}},
	}
	if err := app.HandleMessage(ctx, bridge.Telegram, firstMessage); err != nil {
		t.Fatal(err)
	}
	if got := dc.messages[0]; got.Author.ID != "100" || got.Author.Name != "Alice on Discord" || got.Author.AvatarURL == "" || got.Text != "**hello**" || string(got.Attachments[0].Data) != "file" {
		t.Fatalf("Telegram forward = %+v", got)
	}

	// Explicit mapping works even when usernames differ.
	bob := bridge.User{ID: "2", Username: "robert", Name: "Telegram Bob"}
	candidates, err := app.LinkCandidates(ctx, bob, "bobby")
	if err != nil || len(candidates) != 2 || candidates[0].ID != "200" {
		t.Fatalf("link candidates = %v, %v", candidates, err)
	}
	if err := app.LinkUsers(ctx, "2", "200"); err != nil {
		t.Fatal(err)
	}
	discordReply := bridge.Message{
		ID: "55", Author: dc.users[1], ReplyTo: "dc1", Text: richtext.FromDiscord("hey <@100>"),
	}
	if err := app.HandleMessage(ctx, bridge.Discord, discordReply); err != nil {
		t.Fatal(err)
	}
	if got := tg.messages[0]; got.ReplyTo != "10" || got.Author.Name != "Telegram Bob" || got.TelegramUserID != "2" || !strings.Contains(got.Text, `tg://user?id=1`) {
		t.Fatalf("Discord reply = %+v", got)
	}
	telegramReply := bridge.Message{
		ID: "11", Author: alice, ReplyTo: "tg1",
		Text: richtext.Text{Value: "@robert answer", Entities: []richtext.Entity{{Kind: "mention", Start: 0, End: 7}}},
	}
	if err := app.HandleMessage(ctx, bridge.Telegram, telegramReply); err != nil {
		t.Fatal(err)
	}
	if got := dc.messages[1]; got.ReplyTo != "55" || got.Text != "<@200> answer" || len(got.MentionIDs) != 1 || got.MentionIDs[0] != "200" {
		t.Fatalf("cross-bridge reply = %+v", got)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	app = bridge.New(db, tg, dc)
	replyAfterRestart := bridge.Message{
		ID: "56", Author: dc.users[1], ReplyTo: "dc2", Text: richtext.Text{Value: "after restart"},
	}
	if err := app.HandleMessage(ctx, bridge.Discord, replyAfterRestart); err != nil {
		t.Fatal(err)
	}
	if got := tg.messages[1]; got.ReplyTo != "11" {
		t.Fatalf("reply after restart = %+v", got)
	}
}

func TestLinkCandidatesRankingAndLinkProtection(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)

	dc := &discord{users: []bridge.User{
		{ID: "3", Name: "Zed"}, {ID: "2", Username: "álIce"},
		{ID: "1", Name: "Alice"}, {ID: "4", Name: "Alice"},
	}}
	app := bridge.New(db, &messenger{}, dc)
	if err := db.RememberUser(ctx, bridge.Telegram, bridge.User{ID: "other"}); err != nil {
		t.Fatal(err)
	}
	if err := db.LinkUsers(ctx, "other", "4"); err != nil {
		t.Fatal(err)
	}

	got, err := app.LinkCandidates(ctx, bridge.User{ID: "target", Name: "Alice", Username: "ÁLICE"}, "")
	if err != nil || len(got) != 4 || got[0].ID != "1" || got[1].ID != "2" || got[0].MatchPercent != 100 || got[1].MatchPercent != 100 {
		t.Fatalf("candidates: %+v %v", got, err)
	}
	got, err = app.LinkCandidates(ctx, bridge.User{ID: "target"}, "3")
	if err != nil || got[0].ID != "3" || got[0].MatchPercent != 100 {
		t.Fatalf("ID search: %+v %v", got, err)
	}
	if err := app.LinkUsers(ctx, "target", "4"); err != nil {
		t.Fatal(err)
	}
	linked, ok, err := db.LinkedUser(ctx, bridge.Discord, "4")
	if err != nil || !ok || linked.ID != "other" {
		t.Fatalf("existing link lost: %+v %v %v", linked, ok, err)
	}

	if err := app.UnlinkUser(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	_, ok, err = db.LinkedUser(ctx, bridge.Discord, "4")
	if err != nil || !ok {
		t.Fatalf("unlink: %v %v", ok, err)
	}
}

func TestUnlinkPreventsAutomaticRelinking(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bridge.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tg := &messenger{prefix: "tg"}
	alice := bridge.User{ID: "1", Username: "alice", Name: "Telegram Alice"}
	discordAlice := bridge.User{ID: "100", Username: "alice", Name: "Discord Alice"}
	dc := &discord{prefix: "dc", users: []bridge.User{discordAlice, {ID: "200", Username: "bob"}}}
	app := bridge.New(db, tg, dc)

	if err := app.HandleMessage(ctx, bridge.Telegram, bridge.Message{ID: "1", Author: alice}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.LinkedUser(ctx, bridge.Telegram, alice.ID); err != nil || !ok {
		t.Fatalf("initial automatic link: %v %v", ok, err)
	}

	if err := app.UnlinkUser(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	app = bridge.New(db, tg, dc)

	for _, event := range []struct {
		source bridge.Platform
		user   bridge.User
	}{{bridge.Telegram, alice}, {bridge.Discord, discordAlice}} {
		if err := app.HandleMessage(ctx, event.source, bridge.Message{ID: "2", Author: event.user}); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := db.LinkedUser(ctx, bridge.Telegram, alice.ID); err != nil || ok {
			t.Fatalf("automatic relink after %s message: %v %v", event.source, ok, err)
		}
	}

	bob := bridge.User{ID: "2", Username: "bob"}
	if err := app.HandleMessage(ctx, bridge.Telegram, bridge.Message{ID: "3", Author: bob}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.LinkedUser(ctx, bridge.Telegram, bob.ID); err != nil || !ok {
		t.Fatalf("other user's automatic link: %v %v", ok, err)
	}

	if err := app.LinkUsers(ctx, alice.ID, discordAlice.ID); err != nil {
		t.Fatal(err)
	}
	if linked, ok, err := db.LinkedUser(ctx, bridge.Telegram, alice.ID); err != nil || !ok || linked.ID != discordAlice.ID {
		t.Fatalf("explicit link: %+v %v %v", linked, ok, err)
	}
	if disabled, err := db.AutoLinkDisabled(ctx, alice.ID); err != nil || disabled {
		t.Fatalf("opt-out after explicit link: %v %v", disabled, err)
	}
}

func TestLinkViewResolvesBothOrigins(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	tg := bridge.User{ID: "t", Name: "Alice"}
	dc := bridge.User{ID: "d", Name: "Alice"}
	other := bridge.User{ID: "other", Name: "Other"}
	for _, u := range []struct {
		p bridge.Platform
		u bridge.User
	}{{bridge.Telegram, tg}, {bridge.Telegram, other}, {bridge.Discord, dc}} {
		if err := db.RememberUser(ctx, u.p, u.u); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.LinkUsers(ctx, other.ID, dc.ID); err != nil {
		t.Fatal(err)
	}
	for _, l := range []bridge.MessageLink{{TelegramID: "1", DiscordID: "11", Origin: bridge.Telegram, AuthorID: tg.ID}, {TelegramID: "2", DiscordID: "22", Origin: bridge.Discord, AuthorID: dc.ID}} {
		if err := db.LinkMessage(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	app := bridge.New(db, &messenger{}, &discord{users: []bridge.User{dc}})
	view, err := app.LinkView(ctx, "1", bridge.User{}, "")
	if err != nil || view.Platform != bridge.Telegram || view.Author.ID != "t" || len(view.Candidates) != 1 {
		t.Fatalf("telegram: %+v %v", view, err)
	}
	view, err = app.LinkView(ctx, "2", bridge.User{}, "")
	if err != nil || view.Platform != bridge.Discord || view.Author.ID != "d" || len(view.Linked) != 1 || len(view.Candidates) != 1 || view.Candidates[0].ID != "t" {
		t.Fatalf("discord: %+v %v", view, err)
	}
}

func TestEditsUseOriginalMappings(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)

	tg := &messenger{prefix: "tg"}
	dc := &discord{prefix: "dc"}
	app := bridge.New(db, tg, dc)
	for _, tc := range []struct {
		source bridge.Platform
		msg    bridge.Message
		target *messenger
		copyID string
	}{
		{bridge.Telegram, bridge.Message{ID: "1", Author: bridge.User{ID: "t", Name: "T"}, Text: richtext.Text{Value: "old"}}, &dc.messenger, "dc1"},
		{bridge.Discord, bridge.Message{ID: "2", Author: bridge.User{ID: "d", Name: "D"}, Text: richtext.Text{Value: "old"}}, tg, "tg1"},
	} {
		if err := app.HandleMessage(ctx, tc.source, tc.msg); err != nil {
			t.Fatal(err)
		}
		tc.msg.Text.Value = "new"
		if err := app.HandleEdit(ctx, tc.source, tc.msg); err != nil {
			t.Fatal(err)
		}
		if len(tc.target.edits) != 1 || tc.target.editIDs[0] != tc.copyID || tc.target.edits[0].Text != "new" {
			t.Fatalf("edit: %+v %+v", tc.target.edits, tc.target.editIDs)
		}
		opposite := bridge.Telegram
		if tc.source == bridge.Telegram {
			opposite = bridge.Discord
		}
		tc.msg.ID = tc.copyID
		if err := app.HandleEdit(ctx, opposite, tc.msg); err != nil {
			t.Fatal(err)
		}
		if len(tc.target.edits) != 1 {
			t.Fatal("forwarded copy was edited")
		}
	}
	link := bridge.MessageLink{TelegramID: "extra", DiscordID: "2", Origin: bridge.Discord, AuthorID: "d"}
	if err := db.LinkMessage(ctx, link); err != nil {
		t.Fatal(err)
	}
	if err := app.HandleEdit(ctx, bridge.Discord, bridge.Message{ID: "2"}); err == nil {
		t.Fatal("multipart Telegram destination edit must fail")
	}
}

func TestUnmappedSystemEvent(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	tg := &messenger{prefix: "tg"}
	app := bridge.New(db, tg, &discord{})
	ctx := context.Background()
	event := bridge.Message{Author: bridge.User{Name: "Discord"}, Text: richtext.Text{Value: "channel renamed"}}
	if err := app.HandleMessage(ctx, bridge.Discord, event); err != nil {
		t.Fatal(err)
	}
	if len(tg.messages) != 1 || tg.messages[0].Text != "channel renamed" {
		t.Fatalf("messages: %+v", tg.messages)
	}
	if _, found, err := db.MessageLink(ctx, bridge.Telegram, "tg1"); err != nil || found {
		t.Fatalf("unexpected mapping: found=%v err=%v", found, err)
	}
}

func TestPinSynchronization(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)

	for _, link := range []bridge.MessageLink{{TelegramID: "1", DiscordID: "a", Origin: bridge.Telegram}, {TelegramID: "2", DiscordID: "b", Origin: bridge.Discord}} {
		if err := db.LinkMessage(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	tg := &messenger{}
	dc := &discord{pinnedIDs: []string{"a", "unmapped"}}
	app := bridge.New(db, tg, dc)
	if err := app.RefreshDiscordPins(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tg.pins) != 0 {
		t.Fatalf("baseline propagated: %+v", tg.pins)
	}

	dc.pinnedIDs = []string{"b", "unknown"}
	if err := app.RefreshDiscordPins(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tg.pins) != 2 {
		t.Fatalf("Discord changes: %+v", tg.pins)
	}
	changes := map[sentPin]bool{}
	for _, p := range tg.pins {
		changes[p] = true
	}
	if !changes[sentPin{"1", false}] || !changes[sentPin{"2", true}] {
		t.Fatalf("Discord changes: %+v", tg.pins)
	}

	// Telegram events resulting from Discord pin changes must not echo.
	if err := app.HandleTelegramPin(ctx, "2", true); err != nil {
		t.Fatal(err)
	}
	if err := app.HandleTelegramPin(ctx, "1", false); err != nil {
		t.Fatal(err)
	}
	if err := app.HandleTelegramPin(ctx, "missing", true); err != nil {
		t.Fatal(err)
	}
	if len(dc.pins) != 0 {
		t.Fatalf("unexpected Discord pins: %+v", dc.pins)
	}

	if err := app.HandleTelegramPin(ctx, "1", true); err != nil {
		t.Fatal(err)
	}
	if len(dc.pins) != 1 || dc.pins[0] != (sentPin{"a", true}) {
		t.Fatalf("Telegram pin: %+v", dc.pins)
	}
	dc.pinnedIDs = []string{"a", "b", "unknown"}
	if err := app.RefreshDiscordPins(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tg.pins) != 2 {
		t.Fatalf("Discord echoed Telegram pin: %+v", tg.pins)
	}
	if err := app.HandleTelegramPin(ctx, "1", false); err != nil {
		t.Fatal(err)
	}
	if len(dc.pins) != 2 || dc.pins[1] != (sentPin{"a", false}) {
		t.Fatalf("Telegram unpin: %+v", dc.pins)
	}
	dc.pinnedIDs = []string{"b", "unknown"}
	if err := app.RefreshDiscordPins(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tg.pins) != 2 {
		t.Fatalf("Discord echoed Telegram unpin: %+v", tg.pins)
	}
}

func TestLinkedReactionDeduplication(t *testing.T) {
	for _, discordFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("discordFirst=%v", discordFirst), func(t *testing.T) {
			ctx := context.Background()
			db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer closeStore(t, db)

			for _, id := range []string{"1", "2"} {
				if err := db.LinkMessage(ctx, bridge.MessageLink{TelegramID: id, DiscordID: "99", Origin: bridge.Discord}); err != nil {
					t.Fatal(err)
				}
			}
			// No user records are required, and a second linked account also matches.
			for _, id := range []string{"100", "101"} {
				if err := db.LinkUsers(ctx, "7", id); err != nil {
					t.Fatal(err)
				}
			}
			tg, dc := &messenger{}, &discord{}
			app := bridge.New(db, tg, dc)
			tgEvent := bridge.Reaction{MessageID: "1", UserID: "7", Emoji: "❤️", Added: true}
			dcEvent := bridge.Reaction{MessageID: "99", UserID: "101", Emoji: "❤", Added: true}
			send := func(platform bridge.Platform, event bridge.Reaction) {
				t.Helper()
				if err := app.HandleReaction(ctx, platform, event); err != nil {
					t.Fatal(err)
				}
			}
			if discordFirst {
				send(bridge.Discord, dcEvent)
				send(bridge.Telegram, tgEvent)
				if len(tg.reactions) != 2 || tg.reactions[1] != (sentReaction{"1", "", false}) || len(dc.reactions) != 0 {
					t.Fatalf("dedup: tg=%v dc=%v", tg.reactions, dc.reactions)
				}
			} else {
				send(bridge.Telegram, tgEvent)
				send(bridge.Discord, dcEvent)
				if len(dc.reactions) != 2 || dc.reactions[1] != (sentReaction{"99", "❤️", false}) || len(tg.reactions) != 0 {
					t.Fatalf("dedup: tg=%v dc=%v", tg.reactions, dc.reactions)
				}
			}
			// An unrelated user's reaction remains bridged while the linked pair is suppressed.
			other := bridge.Reaction{MessageID: "2", UserID: "8", Emoji: "❤️", Added: true}
			send(bridge.Telegram, other)
			if got := dc.reactions[len(dc.reactions)-1]; got != (sentReaction{"99", "❤️", true}) {
				t.Fatalf("unrelated: %v", got)
			}
			other.Added = false
			send(bridge.Telegram, other)
			if got := dc.reactions[len(dc.reactions)-1]; got != (sentReaction{"99", "❤️", false}) {
				t.Fatalf("unrelated removal: %v", got)
			}
			dcEvent.Added = false
			send(bridge.Discord, dcEvent)
			if got := dc.reactions[len(dc.reactions)-1]; got != (sentReaction{"99", "❤️", true}) {
				t.Fatalf("restore Telegram reaction: %v", got)
			}
			dcEvent.Added = true
			send(bridge.Discord, dcEvent)
			tgEvent.Added = false
			send(bridge.Telegram, tgEvent)
			if got := tg.reactions[len(tg.reactions)-1]; got != (sentReaction{"1", "❤️", true}) {
				t.Fatalf("restore Discord reaction: %v", got)
			}
		})
	}
}

func TestLinkedReactionOnSplitPart(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	for _, id := range []string{"1", "2"} {
		if err := db.LinkMessage(ctx, bridge.MessageLink{TelegramID: id, DiscordID: "99", Origin: bridge.Discord}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.LinkUsers(ctx, "7", "100"); err != nil {
		t.Fatal(err)
	}
	tg, dc := &messenger{}, &discord{}
	app := bridge.New(db, tg, dc)
	for _, event := range []struct {
		platform bridge.Platform
		reaction bridge.Reaction
	}{
		{bridge.Telegram, bridge.Reaction{MessageID: "2", UserID: "7", Emoji: "👍", Added: true}},
		{bridge.Discord, bridge.Reaction{MessageID: "99", UserID: "100", Emoji: "👍", Added: true}},
	} {
		if err := app.HandleReaction(ctx, event.platform, event.reaction); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(dc.reactions, []sentReaction{{"99", "👍", true}, {"99", "👍", false}}) || !reflect.DeepEqual(tg.reactions, []sentReaction{{"1", "👍", true}}) {
		t.Fatalf("split: tg=%v dc=%v", tg.reactions, dc.reactions)
	}
}

func TestLinkedReactionRetryOppositeDirection(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	if err := db.LinkMessage(ctx, bridge.MessageLink{TelegramID: "1", DiscordID: "99", Origin: bridge.Discord}); err != nil {
		t.Fatal(err)
	}
	if err := db.LinkUsers(ctx, "7", "100"); err != nil {
		t.Fatal(err)
	}
	tg, dc := &messenger{}, &discord{}
	app := bridge.New(db, tg, dc)
	initial := bridge.Reaction{MessageID: "1", UserID: "7", Emoji: "❤️", Added: true}
	if err := app.HandleReaction(ctx, bridge.Telegram, initial); err != nil {
		t.Fatal(err)
	}
	dc.failNext = true
	event := bridge.Reaction{MessageID: "99", UserID: "100", Emoji: "❤", Added: true}
	if err := app.HandleReaction(ctx, bridge.Discord, event); err == nil {
		t.Fatal("expected failure")
	}
	if err := app.HandleReaction(ctx, bridge.Discord, event); err != nil {
		t.Fatal(err)
	}
	if got := dc.reactions; !reflect.DeepEqual(got, []sentReaction{{"99", "❤️", true}, {"99", "❤️", false}, {"99", "❤️", false}}) {
		t.Fatalf("retry: %v", got)
	}
}

func TestTelegramHeartReactionAggregation(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	if err := db.LinkMessage(ctx, bridge.MessageLink{TelegramID: "1", DiscordID: "99", Origin: bridge.Discord}); err != nil {
		t.Fatal(err)
	}
	dc := &discord{}
	app := bridge.New(db, &messenger{}, dc)
	for _, event := range []bridge.Reaction{
		{MessageID: "1", UserID: "a", Emoji: "❤", Added: true},
		{MessageID: "1", UserID: "b", Emoji: "❤️", Added: true},
		{MessageID: "1", UserID: "a", Emoji: "❤️", Added: false},
		{MessageID: "1", UserID: "b", Emoji: "❤", Added: false},
		{MessageID: "1", UserID: "a", Emoji: "✈", Added: true},
		{MessageID: "1", UserID: "b", Emoji: "✈️", Added: true},
	} {
		if err := app.HandleReaction(ctx, bridge.Telegram, event); err != nil {
			t.Fatal(err)
		}
	}
	want := []sentReaction{{"99", "❤️", true}, {"99", "❤️", false}, {"99", "✈", true}, {"99", "✈️", true}}
	if !reflect.DeepEqual(dc.reactions, want) {
		t.Fatalf("reactions: got %v, want %v", dc.reactions, want)
	}
}

func TestTelegramLegacyHeartRemoval(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)
	if err := db.LinkMessage(ctx, bridge.MessageLink{TelegramID: "1", DiscordID: "99", Origin: bridge.Discord}); err != nil {
		t.Fatal(err)
	}
	legacy := bridge.Reaction{MessageID: "99", UserID: "1:a", Emoji: "❤", Added: true}
	if _, err := db.ChangeReaction(ctx, bridge.Telegram, legacy); err != nil {
		t.Fatal(err)
	}
	dc := &discord{}
	app := bridge.New(db, &messenger{}, dc)
	event := bridge.Reaction{MessageID: "1", UserID: "a", Emoji: "❤️", Added: false}
	if err := app.HandleReaction(ctx, bridge.Telegram, event); err != nil {
		t.Fatal(err)
	}
	counts, err := db.ReactionCounts(ctx, bridge.Telegram, "99")
	if err != nil || len(counts) != 0 {
		t.Fatalf("legacy counts: %v, error: %v", counts, err)
	}
	if !reflect.DeepEqual(dc.reactions, []sentReaction{{"99", "❤️", false}}) {
		t.Fatalf("reactions: %v", dc.reactions)
	}
}

func TestDiscordEditUpdatesOrderedLinks(t *testing.T) {
	ctx := context.Background()

	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)

	dc := &discord{prefix: "dc"}
	app := bridge.New(db, &messenger{}, dc)
	msg := bridge.Message{ID: "source", Author: bridge.User{ID: "author"}, Text: richtext.Text{Value: "hello"}}
	if err := app.HandleMessage(ctx, bridge.Telegram, msg); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		before, after []string
		fail          bool
	}{
		{[]string{"dc1"}, []string{"dc1", "dc2", "dc3"}, false},
		{[]string{"dc1", "dc2", "dc3"}, []string{"dc1"}, false},
		{[]string{"dc1"}, []string{"dc1", "dc4"}, true},
		{[]string{"dc1", "dc4"}, []string{"dc1", "dc4"}, false},
	} {
		dc.editResult = step.after
		dc.editErr = nil
		if step.fail {
			dc.editErr = fmt.Errorf("partial edit")
		}
		err := app.HandleEdit(ctx, bridge.Telegram, msg)
		if (err != nil) != step.fail {
			t.Fatalf("edit error: %v", err)
		}
		if !reflect.DeepEqual(dc.editInput, step.before) {
			t.Fatalf("input %v, want %v", dc.editInput, step.before)
		}
		links, err := db.MessageLinks(ctx, bridge.Telegram, msg.ID)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, link := range links {
			got = append(got, link.DiscordID)
			if link.Origin != bridge.Telegram || link.AuthorID != "author" {
				t.Fatalf("metadata: %+v", link)
			}
		}
		if !reflect.DeepEqual(got, step.after) {
			t.Fatalf("links %v, want %v", got, step.after)
		}
		for _, id := range step.before {
			if !slices.Contains(step.after, id) {
				if _, found, err := db.MessageLink(ctx, bridge.Discord, id); err != nil || found {
					t.Fatalf("obsolete link %s: %v %v", id, found, err)
				}
			}
		}
	}
}

func TestDiscordDelete(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)

	tg := &messenger{deleteFail: "2"}
	app := bridge.New(db, tg, &discord{})
	for _, link := range []bridge.MessageLink{
		{TelegramID: "1", DiscordID: "source", Origin: bridge.Discord},
		{TelegramID: "2", DiscordID: "source", Origin: bridge.Discord},
		{TelegramID: "3", DiscordID: "source", Origin: bridge.Discord},
		{TelegramID: "4", DiscordID: "copy", Origin: bridge.Telegram},
	} {
		if err := db.LinkMessage(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	if err := app.HandleDiscordDelete(ctx, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := app.HandleDiscordDelete(ctx, "copy"); err != nil {
		t.Fatal(err)
	}
	if len(tg.deleted) != 0 {
		t.Fatalf("unexpected deletions: %v", tg.deleted)
	}

	if err := app.HandleDiscordDelete(ctx, "source"); err == nil {
		t.Fatal("expected deletion failure")
	}
	if !reflect.DeepEqual(tg.deleted, []string{"1", "2", "3"}) {
		t.Fatalf("deletions: %v", tg.deleted)
	}
	links, err := db.MessageLinks(ctx, bridge.Discord, "source")
	if err != nil || len(links) != 1 || links[0].TelegramID != "2" {
		t.Fatalf("remaining links: %v, %v", links, err)
	}
	links, err = db.MessageLinks(ctx, bridge.Discord, "copy")
	if err != nil || len(links) != 1 {
		t.Fatalf("copy links: %v, %v", links, err)
	}

	tg.deleteFail = ""
	if err := app.HandleDiscordDelete(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	links, err = db.MessageLinks(ctx, bridge.Discord, "source")
	if err != nil || len(links) != 0 {
		t.Fatalf("remaining links: %v, %v", links, err)
	}
}

func TestTelegramDelete(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)

	dc := &discord{}
	dc.deleteFail = "b"
	app := bridge.New(db, &messenger{}, dc)
	for _, link := range []bridge.MessageLink{
		{TelegramID: "source", DiscordID: "a", Origin: bridge.Telegram},
		{TelegramID: "source", DiscordID: "b", Origin: bridge.Telegram},
		{TelegramID: "source", DiscordID: "c", Origin: bridge.Telegram},
		{TelegramID: "copy", DiscordID: "original", Origin: bridge.Discord},
	} {
		if err := db.LinkMessage(ctx, link); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.HandleTelegramDelete(ctx, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := app.HandleTelegramDelete(ctx, "copy"); err != nil {
		t.Fatal(err)
	}
	if len(dc.deleted) != 0 {
		t.Fatalf("unexpected deletions: %v", dc.deleted)
	}
	if err := app.HandleTelegramDelete(ctx, "source"); err == nil {
		t.Fatal("expected deletion failure")
	}
	if !reflect.DeepEqual(dc.deleted, []string{"a", "b", "c"}) {
		t.Fatalf("deletions: %v", dc.deleted)
	}
	links, err := db.MessageLinks(ctx, bridge.Telegram, "source")
	if err != nil || len(links) != 1 || links[0].DiscordID != "b" {
		t.Fatalf("remaining links: %v, %v", links, err)
	}
	links, err = db.MessageLinks(ctx, bridge.Telegram, "copy")
	if err != nil || len(links) != 1 {
		t.Fatalf("copy links: %v, %v", links, err)
	}
	dc.deleteFail = ""
	if err := app.HandleTelegramDelete(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dc.deleted, []string{"a", "b", "c", "b"}) {
		t.Fatalf("retry deletions: %v", dc.deleted)
	}
	links, err = db.MessageLinks(ctx, bridge.Telegram, "source")
	if err != nil || len(links) != 0 {
		t.Fatalf("remaining links: %v, %v", links, err)
	}
}

func TestTelegramForwardAttribution(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, db)

	if err := db.RememberUser(ctx, bridge.Discord, bridge.User{ID: "9", Name: "Discord *Name*"}); err != nil {
		t.Fatal(err)
	}
	if err := db.LinkUsers(ctx, "42", "9"); err != nil {
		t.Fatal(err)
	}
	dc := &discord{users: []bridge.User{{ID: "10", Username: "unlinked"}}}
	b := bridge.New(db, &messenger{}, dc)
	cases := []struct {
		source bridge.ForwardSource
		want   string
	}{
		{bridge.ForwardSource{User: bridge.User{ID: "42", Name: "Telegram"}}, `forwarded from "Discord \*Name\*"`},
		{bridge.ForwardSource{User: bridge.User{ID: "43", Name: "@everyone [x]", Username: "unlinked"}}, "forwarded from \"@\u200beveryone \\[x\\]\""},
		{bridge.ForwardSource{Name: "News", URL: "https://t.me/news"}, `forwarded from "[News](https://t.me/news)"`},
	}
	for i, tc := range cases {
		msg := bridge.Message{
			ID: fmt.Sprint(i), Author: bridge.User{Name: "Sender"}, Forward: &tc.source,
			Text: richtext.Text{Value: "body"}, Attachments: []bridge.Attachment{{Name: "voice.ogg", Voice: &bridge.Voice{}}},
		}
		if err := b.HandleMessage(ctx, bridge.Telegram, msg); err != nil {
			t.Fatal(err)
		}
		out := dc.messages[len(dc.messages)-1]
		if out.Text != tc.want+"\nbody" || len(out.Attachments) != 1 || len(out.MentionIDs) != 0 {
			t.Fatalf("out: %+v", out)
		}
		if err := b.HandleEdit(ctx, bridge.Telegram, msg); err != nil {
			t.Fatal(err)
		}
		if got := dc.edits[len(dc.edits)-1].Text; got != out.Text {
			t.Fatalf("edit %q", got)
		}
	}
	if _, found, err := db.User(ctx, bridge.Telegram, "43"); err != nil || found {
		t.Fatalf("forward source remembered: %v %v", found, err)
	}
}
