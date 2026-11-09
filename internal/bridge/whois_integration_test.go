package bridge_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/richtext"
	"github.com/sleroq/zefxi/internal/store"
)

func TestWhoisResolvesOriginalAuthorAndCurrentLinks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bridge.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tg := &messenger{prefix: "tg"}
	telegramUser := bridge.User{ID: "1", Username: "alice", Name: "Alice on Telegram"}
	discordUser := bridge.User{ID: "100", Username: "alice", Name: "Alice on Discord"}
	dc := &discord{prefix: "dc", users: []bridge.User{discordUser}}
	app := bridge.New(db, tg, dc)

	for _, tc := range []struct {
		platform bridge.Platform
		id       string
		author   bridge.User
	}{
		{bridge.Telegram, "10", telegramUser},
		{bridge.Discord, "20", discordUser},
	} {
		message := bridge.Message{ID: tc.id, Author: tc.author, Text: richtext.Text{Value: "hello"}}
		if err := app.HandleMessage(ctx, tc.platform, message); err != nil {
			t.Fatal(err)
		}
	}
	if got := tg.messages[0]; got.Author.Name != telegramUser.Name || got.TelegramUserID != telegramUser.ID {
		t.Fatalf("mapped attribution: %+v", got)
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

	want := bridge.Identity{Telegram: telegramUser, Discord: discordUser}
	for _, tc := range []struct {
		platform bridge.Platform
		message  string
		fallback bridge.User
	}{
		{bridge.Telegram, "10", bridge.User{ID: "wrong-bot"}},
		{bridge.Discord, "dc1", bridge.User{ID: "wrong-webhook"}},
		{bridge.Discord, "20", bridge.User{ID: "wrong-bot"}},
		{bridge.Telegram, "tg1", bridge.User{ID: "wrong-bot"}},
		{bridge.Telegram, "old-telegram-message", telegramUser},
		{bridge.Discord, "old-discord-message", discordUser},
	} {
		got, err := app.Whois(ctx, tc.platform, tc.message, tc.fallback)
		if err != nil || got != want {
			t.Fatalf("Whois(%s, %s) = %+v, %v", tc.platform, tc.message, got, err)
		}
	}

	unlinked := bridge.User{ID: "200", Name: "Unlinked Discord"}
	message := bridge.Message{ID: "21", Author: unlinked, Text: richtext.Text{Value: "hello"}}
	if err := app.HandleMessage(ctx, bridge.Discord, message); err != nil {
		t.Fatal(err)
	}
	if got := tg.messages[1]; got.Author != unlinked || got.TelegramUserID != "" {
		t.Fatalf("unmapped attribution: %+v", got)
	}
	if got, err := app.Whois(ctx, bridge.Telegram, "tg2", bridge.User{}); err != nil || got != (bridge.Identity{Discord: unlinked}) {
		t.Fatalf("unmapped Whois = %+v, %v", got, err)
	}
	if got, err := app.Whois(ctx, bridge.Telegram, "unknown", bridge.User{}); err != nil || got != (bridge.Identity{}) {
		t.Fatalf("unknown Whois = %+v, %v", got, err)
	}

	newTelegram := bridge.User{ID: "2", Name: "New Telegram"}
	if err := db.RememberUser(ctx, bridge.Telegram, newTelegram); err != nil {
		t.Fatal(err)
	}
	if err := db.LinkUsers(ctx, newTelegram.ID, discordUser.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := app.Whois(ctx, bridge.Telegram, "tg1", bridge.User{}); err != nil || got != (bridge.Identity{Telegram: telegramUser, Discord: discordUser}) {
		t.Fatalf("Whois after relinking = %+v, %v", got, err)
	}
}
