package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/store"
)

func openSQLite(t *testing.T) (*store.Store, func() *store.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bridge.sqlite")
	open := func() *store.Store {
		t.Helper()

		s, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		})
		return s
	}
	return open(), open
}

func TestSQLiteUserLinks(t *testing.T) {
	ctx := context.Background()
	s, reopen := openSQLite(t)

	users := []struct {
		platform bridge.Platform
		user     bridge.User
	}{
		{bridge.Telegram, bridge.User{ID: "t1", Username: "Alice", Name: "Alice", AvatarURL: "old"}},
		{bridge.Telegram, bridge.User{ID: "t2", Username: "Bob"}},
		{bridge.Discord, bridge.User{ID: "d1", Username: "Alice"}},
		{bridge.Discord, bridge.User{ID: "d2", Username: "Bob"}},
	}
	for _, u := range users {
		if err := s.RememberUser(ctx, u.platform, u.user); err != nil {
			t.Fatal(err)
		}
	}

	updated := bridge.User{ID: "t1", Username: "ALICE_NEW", Name: "Updated", AvatarURL: "new"}
	if err := s.RememberUser(ctx, bridge.Telegram, updated); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"t1", "alice_new"} {
		got, ok, err := s.User(ctx, bridge.Telegram, key)
		if err != nil || !ok || got != updated {
			t.Fatalf("User(%q) = %+v, %v, %v", key, got, ok, err)
		}
	}
	if _, ok, err := s.User(ctx, bridge.Telegram, "Alice"); err != nil || ok {
		t.Fatalf("stale username: %v %v", ok, err)
	}

	for _, pair := range [][2]string{{"t1", "d1"}, {"t2", "d2"}, {"t1", "d2"}} {
		if err := s.LinkUsers(ctx, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = reopen()

	for _, lookup := range []struct {
		p        bridge.Platform
		id, want string
	}{{bridge.Telegram, "t1", "d1"}, {bridge.Discord, "d2", "t2"}} {
		got, ok, err := s.LinkedUser(ctx, lookup.p, lookup.id)
		if err != nil || !ok || got.ID != lookup.want {
			t.Fatalf("linked user: %+v %v %v", got, ok, err)
		}
	}

	gotUser, ok, err := s.User(ctx, bridge.Telegram, "ALIce_New")
	if err != nil || !ok || gotUser != updated {
		t.Fatalf("reopened user: %+v %v %v", gotUser, ok, err)
	}

}

func TestSQLiteMessageLinks(t *testing.T) {
	ctx := context.Background()
	s, reopen := openSQLite(t)

	links := []bridge.MessageLink{
		{TelegramID: "10", DiscordID: "20", Origin: bridge.Telegram, AuthorID: "t1"},
		{TelegramID: "10", DiscordID: "21", Origin: bridge.Telegram, AuthorID: "t1"},
		{TelegramID: "11", DiscordID: "20", Origin: bridge.Discord, AuthorID: "d2"},
	}
	for _, link := range links {
		if err := s.LinkMessage(ctx, link); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LinkMessage(ctx, links[0]); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = reopen()

	for _, lookup := range []struct {
		p    bridge.Platform
		id   string
		want bridge.MessageLink
	}{
		{bridge.Telegram, "10", links[0]}, {bridge.Discord, "20", links[0]}, {bridge.Discord, "21", links[1]}, {bridge.Telegram, "11", links[2]},
	} {
		got, ok, err := s.MessageLink(ctx, lookup.p, lookup.id)
		if err != nil || !ok || got != lookup.want {
			t.Fatalf("reply mapping: %+v %v %v", got, ok, err)
		}
	}
	for _, lookup := range []struct {
		p    bridge.Platform
		id   string
		want []bridge.MessageLink
	}{
		{bridge.Telegram, "10", links[:2]},
		{bridge.Discord, "20", []bridge.MessageLink{links[0], links[2]}},
		{bridge.Telegram, "missing", nil},
	} {
		got, err := s.MessageLinks(ctx, lookup.p, lookup.id)
		if err != nil || !reflect.DeepEqual(got, lookup.want) {
			t.Fatalf("MessageLinks(%s, %q) = %+v, %v; want %+v", lookup.p, lookup.id, got, err, lookup.want)
		}
	}
	if _, ok, err := s.MessageLink(ctx, bridge.Telegram, "missing"); err != nil || ok {
		t.Fatalf("missing message: %v %v", ok, err)
	}

}

func TestSQLiteReactionMembership(t *testing.T) {
	ctx := context.Background()
	s, reopen := openSQLite(t)

	changes := []struct {
		user, emoji string
		added       bool
		want        []bridge.ReactionCount
	}{
		{"u1", "b", true, []bridge.ReactionCount{{Emoji: "b", Count: 1}}},
		{"u1", "b", true, []bridge.ReactionCount{{Emoji: "b", Count: 1}}},
		{"u2", "b", true, []bridge.ReactionCount{{Emoji: "b", Count: 2}}},
		{"u1", "a", true, []bridge.ReactionCount{{Emoji: "a", Count: 1}, {Emoji: "b", Count: 2}}},
		{"u1", "b", false, []bridge.ReactionCount{{Emoji: "a", Count: 1}, {Emoji: "b", Count: 1}}},
		{"u1", "b", false, []bridge.ReactionCount{{Emoji: "a", Count: 1}, {Emoji: "b", Count: 1}}},
		{"u2", "b", false, []bridge.ReactionCount{{Emoji: "a", Count: 1}}},
	}
	for _, c := range changes {
		reaction := bridge.Reaction{MessageID: "10", UserID: c.user, Emoji: c.emoji, Added: c.added}
		got, err := s.ChangeReaction(ctx, bridge.Telegram, reaction)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("reaction %+v: %v %v", c, got, err)
		}
	}
	for _, scope := range []struct {
		p  bridge.Platform
		id string
	}{{bridge.Discord, "10"}, {bridge.Telegram, "11"}} {
		if got, err := s.ReactionCounts(ctx, scope.p, scope.id); err != nil || len(got) != 0 {
			t.Fatalf("reaction isolation: %v %v", got, err)
		}
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = reopen()

	counts, err := s.ReactionCounts(ctx, bridge.Telegram, "10")
	if err != nil || !reflect.DeepEqual(counts, []bridge.ReactionCount{{Emoji: "a", Count: 1}}) {
		t.Fatalf("reopened reactions: %v %v", counts, err)
	}
	reaction := bridge.Reaction{MessageID: "10", UserID: "u1", Emoji: "a", Added: false}
	counts, err = s.ChangeReaction(ctx, bridge.Telegram, reaction)
	if err != nil || len(counts) != 0 {
		t.Fatalf("remove final reaction: %v %v", counts, err)
	}
}

func TestMigrationAndDirectPairs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`create table user_links (telegram_id text primary key not null, discord_id text unique not null);
 insert into user_links values ('t1','d1'),('t2','d2')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, item := range []struct {
		p  bridge.Platform
		id string
	}{{bridge.Telegram, "t1"}, {bridge.Telegram, "t2"}, {bridge.Discord, "d1"}, {bridge.Discord, "d2"}} {
		if err := s.RememberUser(ctx, item.p, bridge.User{ID: item.id}); err != nil {
			t.Fatal(err)
		}
	}

	for _, pair := range [][2]string{{"t1", "d2"}, {"t2", "d1"}, {"t1", "d1"}} {
		if err := s.LinkUsers(ctx, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		p  bridge.Platform
		id string
	}{{bridge.Telegram, "t1"}, {bridge.Telegram, "t2"}, {bridge.Discord, "d1"}, {bridge.Discord, "d2"}} {
		links, err := s.LinkedUsers(ctx, item.p, item.id)
		if err != nil || len(links) != 2 {
			t.Fatalf("links %s %s: %v %v", item.p, item.id, links, err)
		}
	}
	if err := s.UnlinkPair(ctx, "t1", "d1"); err != nil {
		t.Fatal(err)
	}
	links, err := s.LinkedUsers(ctx, bridge.Telegram, "t1")
	if err != nil || len(links) != 1 || links[0].ID != "d2" {
		t.Fatalf("remaining: %v %v", links, err)
	}
	disabled, err := s.AutoLinkDisabled(ctx, "t1")
	if err != nil || !disabled {
		t.Fatalf("opt out: %v %v", disabled, err)
	}
	if err := s.LinkUsers(ctx, "t1", "d1"); err != nil {
		t.Fatal(err)
	}
	disabled, err = s.AutoLinkDisabled(ctx, "t1")
	if err != nil || disabled {
		t.Fatalf("opt in: %v %v", disabled, err)
	}
}
