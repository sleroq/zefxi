package main

import (
	"path/filepath"
	"testing"
)

func TestLoadConfigTelegramUserSession(t *testing.T) {
	for name, value := range map[string]string{
		"TELEGRAM_TOKEN": "token", "TELEGRAM_CHAT_ID": "-1000000000123", "TELEGRAM_APP_ID": "123",
		"TELEGRAM_APP_HASH": "hash", "DISCORD_TOKEN": "token", "DISCORD_SERVER": "guild",
		"DISCORD_CHANNEL": "channel", "SQLITE_PATH": filepath.Join(t.TempDir(), "bridge.db"),
		"TELEGRAM_SESSION_DIR": "", "TELEGRAM_2FA_PASSWORD": "", "DISCORD_MAX_UPLOAD_MIB": "",
	} {
		t.Setenv(name, value)
	}
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.telegramUser.AppID != 123 || c.telegramUser.AppHash != "hash" || c.telegramUser.SessionDir != filepath.Join(filepath.Dir(c.database), "telegram-session") {
		t.Fatalf("Telegram user config: %+v", c.telegramUser)
	}

	t.Setenv("TELEGRAM_SESSION_DIR", "/tmp/telegram-user")
	t.Setenv("TELEGRAM_2FA_PASSWORD", "password")
	c, err = loadConfig()
	if err != nil || c.telegramUser.SessionDir != "/tmp/telegram-user" || c.telegramUser.Password != "password" {
		t.Fatalf("Telegram user config: %+v, %v", c.telegramUser, err)
	}
	for _, tc := range []struct{ name, value string }{
		{"TELEGRAM_APP_ID", "0"}, {"TELEGRAM_APP_ID", "invalid"}, {"TELEGRAM_APP_HASH", ""},
		{"TELEGRAM_CHAT_ID", "-123"}, {"TELEGRAM_CHAT_ID", "123"}, {"TELEGRAM_CHAT_ID", "-1000000000000"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			if _, err := loadConfig(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

}
