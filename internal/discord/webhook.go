package discord

import (
	"context"
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// InitializeWebhook selects a bot-owned webhook or creates one for the channel.
func (c *Client) InitializeWebhook(ctx context.Context) error {
	user, err := c.session.User("@me", discordgo.WithContext(ctx))
	if err != nil {
		return webhookInitError("get bot identity", err)
	}
	webhooks, err := c.session.ChannelWebhooks(c.channelID, discordgo.WithContext(ctx))
	if err != nil {
		return webhookInitError("list channel webhooks", err)
	}
	for _, webhook := range webhooks {
		if webhook.User != nil && webhook.User.ID == user.ID && webhook.ID != "" && webhook.Token != "" {
			c.webhookID, c.webhookToken = webhook.ID, webhook.Token
			return nil
		}
	}
	webhook, err := c.session.WebhookCreate(c.channelID, "Zefxi", "", discordgo.WithContext(ctx))
	if err != nil {
		return webhookInitError("create channel webhook (Manage Webhooks permission required)", err)
	}
	if webhook.ID == "" || webhook.Token == "" {
		return errors.New("discord: created webhook has no ID or token")
	}
	c.webhookID, c.webhookToken = webhook.ID, webhook.Token
	return nil
}

func webhookInitError(operation string, err error) error {
	if restErr, ok := errors.AsType[*discordgo.RESTError](err); ok {
		return fmt.Errorf("discord: %s: HTTP %d", operation, restErr.Response.StatusCode)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("discord: %s: %w", operation, err)
	}
	return fmt.Errorf("discord: %s: request failed (%T)", operation, err)
}
