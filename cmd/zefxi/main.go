package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/sleroq/zefxi/internal/bridge"
	"github.com/sleroq/zefxi/internal/discord"
	"github.com/sleroq/zefxi/internal/store"
	"github.com/sleroq/zefxi/internal/telegram"
)

type config struct {
	telegramToken, discordToken, guildID, channelID, database string
	telegramChatID                                            int64
	discordMaxUpload                                          int64
}

func loadConfig() (config, error) {
	var c config
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"TELEGRAM_TOKEN", &c.telegramToken},
		{"DISCORD_TOKEN", &c.discordToken},
		{"DISCORD_SERVER", &c.guildID},
		{"DISCORD_CHANNEL", &c.channelID},
	} {
		*field.value = os.Getenv(field.name)
		if *field.value == "" {
			return c, fmt.Errorf("%s is required", field.name)
		}
	}
	chatID, err := strconv.ParseInt(os.Getenv("TELEGRAM_CHAT_ID"), 10, 64)
	if err != nil || chatID == 0 {
		return c, errors.New("TELEGRAM_CHAT_ID must be a nonzero integer")
	}
	c.telegramChatID = chatID
	c.database = os.Getenv("SQLITE_PATH")
	if c.database == "" {
		c.database = "zefxi.db"
	}
	c.discordMaxUpload = 20 * 1024 * 1024
	if value := os.Getenv("DISCORD_MAX_UPLOAD_MIB"); value != "" {
		mib, err := strconv.ParseInt(value, 10, 32)
		if err != nil || mib <= 0 {
			return c, errors.New("DISCORD_MAX_UPLOAD_MIB must be a positive integer")
		}
		c.discordMaxUpload = mib * 1024 * 1024
	}
	return c, nil
}

func run(ctx context.Context, c config) (runErr error) {
	db, err := store.Open(c.database)
	if err != nil {
		return fmt.Errorf("open SQLite: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, db.Close()) }()

	dc, err := discord.New(c.discordToken, c.guildID, c.channelID, c.discordMaxUpload)
	if err != nil {
		return err
	}
	if err := dc.InitializeWebhook(ctx); err != nil {
		return err
	}
	tg, err := telegram.New(c.telegramToken, c.telegramChatID)
	if err != nil {
		return fmt.Errorf("start Telegram bot: %w", err)
	}
	app := bridge.New(db, tg, dc)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- dc.Run(ctx, app) }()
	go func() { errs <- tg.Run(ctx, app) }()

	select {
	case err = <-errs:
		cancel()
	case <-ctx.Done():
		err = <-errs
	}
	otherErr := <-errs
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	if errors.Is(otherErr, context.Canceled) {
		otherErr = nil
	}
	return errors.Join(err, otherErr)
}

func main() {
	c, err := loadConfig()
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err = run(ctx, c)
		stop()
	}
	if err != nil {
		slog.Error("zefxi stopped", "error", err)
		os.Exit(1)
	}
}
