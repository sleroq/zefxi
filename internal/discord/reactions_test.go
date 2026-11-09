package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestReactionUsers(t *testing.T) {
	c, err := New("token", "guild", "channel", 4)
	if err != nil {
		t.Fatal(err)
	}

	c.session.State.User = &discordgo.User{ID: "self"}
	var calls []string
	c.session.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"reactions":[{"emoji":{"name":"🔥"}},{"emoji":{"name":"party","id":"456"}},{"emoji":{"name":"🤖"}}]}`
		if strings.Contains(r.URL.Path, "/reactions/") {
			q := r.URL.Query()
			if q.Get("limit") != "100" {
				t.Fatal("missing page limit")
			}
			key := strings.TrimPrefix(r.URL.Path, "/api/v9/channels/channel/messages/message/reactions/")
			// The API version is owned by DiscordGo.
			key = key[strings.LastIndex(key, "/")+1:]
			calls = append(calls, key+":"+q.Get("type")+":"+q.Get("after"))
			users := []*discordgo.User{{ID: "self"}}
			switch key {
			case "🔥":
				if !strings.Contains(r.URL.EscapedPath(), "%F0%9F%94%A5") {
					t.Fatalf("unicode not encoded: %s", r.URL)
				}
				switch {
				case q.Get("type") == "0" && q.Get("after") == "":
					users = nil
					for i := 1; i <= 100; i++ {
						users = append(users, &discordgo.User{ID: fmt.Sprint(i), Username: "user", GlobalName: "Display"})
					}
				case q.Get("type") == "0":
					if q.Get("after") != "100" {
						t.Fatalf("wrong cursor: %s", q.Get("after"))
					}
					users = append(users, &discordgo.User{ID: "101"})
				default:
					users = append(users, &discordgo.User{ID: "1"}, &discordgo.User{ID: "102", Bot: true})
				}
			case "party:456":
				users = append(users, &discordgo.User{ID: "custom", Username: "Custom"})
			case "🤖":
			default:
				t.Fatalf("unexpected emoji path: %s", r.URL)
			}
			data, err := json.Marshal(users)
			if err != nil {
				t.Fatal(err)
			}
			body = string(data)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	got, err := c.ReactionUsers(context.Background(), "message")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Emoji != "🔥" || len(got[0].Users) != 102 || got[1].Emoji != ":party:" || len(got[1].Users) != 1 {
		t.Fatalf("unexpected reactions: %+v", got)
	}
	if got[0].Users[0].Username != "user" || got[0].Users[0].Name != "Display" || got[0].Users[101].ID != "102" {
		t.Fatalf("unexpected users: %+v", got[0].Users)
	}
	if len(calls) != 7 {
		t.Fatalf("unexpected pagination/type requests: %v", calls)
	}
}

func TestReactionUsersAPIErrors(t *testing.T) {
	for _, failMessage := range []bool{true, false} {
		t.Run(fmt.Sprint(failMessage), func(t *testing.T) {
			c, err := New("token", "guild", "channel", 4)
			if err != nil {
				t.Fatal(err)
			}
			c.session.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusForbidden, `{"message":"Missing Access","code":50001}`
				if !failMessage && !strings.Contains(r.URL.Path, "/reactions/") {
					status, body = http.StatusOK, `{"reactions":[{"emoji":{"name":"🔥"}}]}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			_, err = c.ReactionUsers(context.Background(), "message")
			if apiErr, ok := errors.AsType[*discordgo.RESTError](err); !ok || apiErr.Response.StatusCode != http.StatusForbidden {
				t.Fatalf("expected Discord forbidden error, got %v", err)
			}
		})
	}
}
