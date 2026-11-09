package bridge

import (
	"context"
	"errors"
	"fmt"
)

// HandleTelegramPin maps a service message to its Discord counterpart.
func (b *Bridge) HandleTelegramPin(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	link, found, err := b.store.MessageLink(ctx, Telegram, id)
	if err != nil || !found {
		return err
	}
	if b.discordPins[link.DiscordID] {
		return nil
	}
	if err := b.discord.SetPin(ctx, link.DiscordID, true); err != nil {
		return fmt.Errorf("pin Discord message %s: %w", link.DiscordID, err)
	}
	if b.discordPins != nil {
		b.discordPins[link.DiscordID] = true
	}
	return nil
}

// RefreshDiscordPins snapshots the channel on startup and diffs subsequent updates.
func (b *Bridge) RefreshDiscordPins(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids, err := b.discord.PinnedMessages(ctx)
	if err != nil {
		return fmt.Errorf("fetch Discord pins: %w", err)
	}
	next := make(map[string]bool, len(ids))
	for _, id := range ids {
		next[id] = true
	}
	previous := b.discordPins
	b.discordPins = next
	if previous == nil {
		return nil
	}
	var errs []error
	for _, change := range []struct {
		from, to map[string]bool
		pinned   bool
	}{{previous, next, false}, {next, previous, true}} {
		for id := range change.from {
			if change.to[id] {
				continue
			}
			link, found, err := b.store.MessageLink(ctx, Discord, id)
			if err == nil && found {
				err = b.telegram.SetPin(ctx, link.TelegramID, change.pinned)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("sync Discord pin %s: %w", id, err))
			}
		}
	}
	return errors.Join(errs...)
}
