package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"github.com/sleroq/zefxi/internal/bridge"
)

func (c *Client) ReactionUsers(ctx context.Context, messageID string) ([]bridge.ReactionUsers, error) {
	message, err := c.session.ChannelMessage(c.channelID, messageID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("discord: fetch message %s reactions: %w", messageID, err)
	}
	var reactions []bridge.ReactionUsers
	for _, reaction := range message.Reactions {
		users, err := c.usersWhoReacted(ctx, messageID, reaction.Emoji.APIName())
		if err != nil {
			return nil, err
		}
		if len(users) == 0 {
			continue
		}
		emoji := reaction.Emoji.Name
		if reaction.Emoji.ID != "" {
			emoji = ":" + emoji + ":"
		}
		reactions = append(reactions, bridge.ReactionUsers{Emoji: emoji, Users: users})
	}
	return reactions, nil
}

func (c *Client) usersWhoReacted(ctx context.Context, messageID, emoji string) ([]bridge.User, error) {
	var result []bridge.User
	seen := make(map[string]bool)
	for reactionType := 0; reactionType <= 1; reactionType++ {
		after := ""
		for {
			users, err := c.reactionUsersPage(ctx, messageID, emoji, reactionType, after)
			if err != nil {
				return nil, err
			}
			for _, user := range users {
				if seen[user.ID] || (c.session.State.User != nil && user.ID == c.session.State.User.ID) {
					continue
				}
				seen[user.ID] = true
				result = append(result, bridge.User{ID: user.ID, Username: user.Username, Name: user.DisplayName()})
			}
			if len(users) < 100 {
				break
			}
			after = users[len(users)-1].ID
		}
	}
	return result, nil
}

func (c *Client) reactionUsersPage(ctx context.Context, messageID, emoji string, reactionType int, after string) ([]*discordgo.User, error) {
	query := url.Values{"limit": {"100"}, "type": {strconv.Itoa(reactionType)}}
	if after != "" {
		query.Set("after", after)
	}
	endpoint := discordgo.EndpointMessageReactions(c.channelID, messageID, url.PathEscape(emoji))
	body, err := c.session.RequestWithBucketID(http.MethodGet, endpoint+"?"+query.Encode(), nil,
		discordgo.EndpointMessageReaction(c.channelID, "", "", ""), discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("discord: fetch message %s reaction %s users (type %d): %w",
			messageID, emoji, reactionType, err)
	}
	var users []*discordgo.User
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("discord: decode message %s reaction %s users (type %d): %w",
			messageID, emoji, reactionType, err)
	}
	return users, nil
}
