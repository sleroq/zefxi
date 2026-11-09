package telegram

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/gotd/contrib/storage"
	gotdtelegram "github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/sleroq/zefxi/internal/bridge"
)

func (c *Client) matchesUserPeer(peer tg.PeerClass) bool {
	p, ok := peer.(*tg.PeerChannel)
	return ok && c.chatID == -1000000000000-p.ChannelID
}

func (c *Client) registerUserUpdates(d tg.UpdateDispatcher, client *gotdtelegram.Client, peers storage.PeerStorage, h bridge.Handler) {
	message := func(ctx context.Context, e tg.Entities, m tg.MessageClass, edit bool) {
		if err := c.forwardUserMessage(ctx, client, peers, h, e, m, edit); err != nil {
			slog.Error("Telegram user message failed", "error", err)
		}
	}

	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		message(ctx, e, u.Message, false)
		return nil
	})
	d.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		message(ctx, e, u.Message, true)
		return nil
	})

	deletes := func(ctx context.Context, ids []int) {
		for _, id := range ids {
			if err := h.HandleTelegramDelete(ctx, strconv.Itoa(id)); err != nil {
				slog.Error("Telegram deletion failed", "error", err)
			}
		}
	}

	pins := func(ctx context.Context, ids []int, pinned bool) {
		for _, id := range ids {
			if err := h.HandleTelegramPin(ctx, strconv.Itoa(id), pinned); err != nil {
				slog.Error("Telegram pin failed", "error", err)
			}
		}
	}

	d.OnDeleteChannelMessages(func(ctx context.Context, _ tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		if c.matchesUserPeer(&tg.PeerChannel{ChannelID: u.ChannelID}) {
			deletes(ctx, u.Messages)
		}
		return nil
	})
	d.OnPinnedChannelMessages(func(ctx context.Context, _ tg.Entities, u *tg.UpdatePinnedChannelMessages) error {
		if c.matchesUserPeer(&tg.PeerChannel{ChannelID: u.ChannelID}) {
			pins(ctx, u.Messages, u.Pinned)
		}
		return nil
	})
}
