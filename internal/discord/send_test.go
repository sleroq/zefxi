package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sleroq/zefxi/internal/bridge"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSendRouting(t *testing.T) {
	for _, reply := range []bool{false, true} {
		t.Run(map[bool]string{false: "webhook", true: "native reply"}[reply], func(t *testing.T) {
			c, err := New("token", "guild", "channel", 4)
			if err != nil {
				t.Fatal(err)
			}
			c.webhookID, c.webhookToken = "123", "secret"

			c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}

				if reply {
					if !strings.HasSuffix(r.URL.Path, "/channels/channel/messages") || payload["message_reference"] == nil {
						t.Fatal("reply did not use native bot reference")
					}
				} else if !strings.Contains(r.URL.Path, "/webhooks/123/secret") || r.URL.Query().Get("wait") != "true" {
					t.Fatal("non-reply did not use waiting webhook")
				}

				var content string
				_ = json.Unmarshal(payload["content"], &content)
				if !strings.Contains(content, "https://storage.example/file?signature=ok") ||
					!strings.Contains(content, "lost.txt (9 bytes); upload limit 4 bytes") ||
					!strings.Contains(content, "small.txt (3 bytes)]") {
					t.Fatalf("attachment fallback lost: %s", content)
				}
				if reply && !strings.HasPrefix(content, "Alice: ") {
					t.Fatal("reply sender missing")
				}

				var mentions struct {
					Parse []string `json:"parse"`
					Users []string `json:"users"`
				}
				_ = json.Unmarshal(payload["allowed_mentions"], &mentions)
				if len(mentions.Parse) != 0 || len(mentions.Users) != 1 || mentions.Users[0] != "123" {
					t.Fatal("unsafe mention policy")
				}
				body := io.NopCloser(strings.NewReader(`{"id":"sent"}`))
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
			})}

			m := bridge.OutboundMessage{Author: bridge.User{Name: "Alice"}, Text: "hello", MentionIDs: []string{"123"}, Attachments: []bridge.Attachment{
				{Name: "large.txt", Data: []byte("oversize"), URL: "https://storage.example/file?signature=ok"},
				{Name: "lost.txt", Size: 9},
				{Name: "small.txt", Size: 3},
			}}
			if reply {
				m.ReplyTo = "original"
			}

			ids, err := c.Send(context.Background(), m)
			if err != nil || len(ids) != 1 || ids[0] != "sent" {
				t.Fatalf("Send = %v, %v", ids, err)
			}
		})
	}
}

func TestWebhookNamesStayWithinDiscordLimits(t *testing.T) {
	for _, name := range []string{"Clyde", strings.Repeat("😀", 50)} {
		t.Run(fmt.Sprintf("length-%d", utf16Units(name)), func(t *testing.T) {
			c, err := New("token", "guild", "channel", 1024)
			if err != nil {
				t.Fatal(err)
			}
			c.webhookID, c.webhookToken = "123", "secret"
			c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var payload struct {
					Username string `json:"username"`
					Content  string `json:"content"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if name == "Clyde" && (payload.Username != "" || payload.Content != "Clyde: hello") {
					t.Fatalf("reserved name: %+v", payload)
				}
				if name != "Clyde" && (utf16Units(payload.Username) > 80 || payload.Content != "hello") {
					t.Fatalf("long name: %+v", payload)
				}
				body := io.NopCloser(strings.NewReader(`{"id":"sent"}`))
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
			})}
			if _, err := c.Send(context.Background(), bridge.OutboundMessage{Author: bridge.User{Name: name}, Text: "hello"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReplySenderNameCannotCreateMention(t *testing.T) {
	c, err := New("token", "guild", "channel", 1024)
	if err != nil {
		t.Fatal(err)
	}
	c.webhookID, c.webhookToken = "123", "secret"
	c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload.Content, "<@123>") {
			t.Fatalf("sender name contains an active mention: %q", payload.Content)
		}
		body := io.NopCloser(strings.NewReader(`{"id":"sent"}`))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
	})}
	msg := bridge.OutboundMessage{Author: bridge.User{Name: "<@123>"}, Text: "hello", ReplyTo: "original"}
	if _, err := c.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
}

func TestSendLongUnicode(t *testing.T) {
	for _, reply := range []bool{false, true} {
		for _, failLater := range []bool{false, true} {
			t.Run(fmt.Sprintf("reply=%t/fail=%t", reply, failLater), func(t *testing.T) {
				c, err := New("token", "guild", "channel", 20*1024*1024)
				if err != nil {
					t.Fatal(err)
				}
				c.webhookID, c.webhookToken = "123", "secret"

				text := strings.Repeat("hello 😀 ", 260) + "<@123456789012345678>" + strings.Repeat("界🚀", 900)
				calls := 0
				var received strings.Builder

				c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					var payload struct {
						Content   string `json:"content"`
						Reference *struct {
							ID string `json:"message_id"`
						} `json:"message_reference"`
					}
					if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
						reader, err := r.MultipartReader()
						if err != nil {
							t.Fatal(err)
						}
						part, err := reader.NextPart()
						if err != nil {
							t.Fatal(err)
						}
						if err := json.NewDecoder(part).Decode(&payload); err != nil {
							t.Fatal(err)
						}
						if calls != 1 {
							t.Fatal("attachment repeated")
						}
					} else if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}

					if !utf8.ValidString(payload.Content) || utf16Units(payload.Content) > 2000 {
						t.Fatal("invalid Unicode or oversized chunk")
					}
					if reply {
						if payload.Reference == nil || payload.Reference.ID != "original" || !strings.HasPrefix(payload.Content, "Alice: ") {
							t.Fatal("reply identity/reference lost")
						}
						payload.Content = strings.TrimPrefix(payload.Content, "Alice: ")
					}

					received.WriteString(payload.Content)
					status := http.StatusOK
					body := fmt.Sprintf(`{"id":"sent%d"}`, calls)
					if failLater && calls == 2 {
						status = http.StatusBadRequest
						body = `{"message":"failed","code":50035}`
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
				})}

				m := bridge.OutboundMessage{Author: bridge.User{Name: "Alice"}, Text: text}
				m.Attachments = []bridge.Attachment{{Name: "hello.txt", Data: []byte("hello")}}
				if reply {
					m.ReplyTo = "original"
				}

				ids, err := c.Send(context.Background(), m)
				if failLater {
					if err == nil || len(ids) != 1 || ids[0] != "sent1" {
						t.Fatalf("partial result: %v, %v", ids, err)
					}
					return
				}

				if err != nil || len(ids) != calls || calls < 3 {
					t.Fatalf("result: %v, %v", ids, err)
				}
				for i, id := range ids {
					if id != fmt.Sprintf("sent%d", i+1) {
						t.Fatalf("wrong ID: %s", id)
					}
				}
				if received.String() != text {
					t.Fatal("chunking changed content")
				}
			})
		}
	}
}

func TestSplitContentMentionBoundary(t *testing.T) {
	mention := "<@123456789012345678>"
	chunks, err := splitContent(strings.Repeat("😀", 995)+mention, 2000)
	if err != nil || len(chunks) != 2 || chunks[1] != mention {
		t.Fatalf("mention split: %v, %v", chunks, err)
	}
}

func TestEditRoutingAndPartialUpdateFilter(t *testing.T) {
	for _, raw := range []struct {
		json string
		want bool
	}{
		{`{"id":"1","embeds":[]}`, false}, {`{"id":"1","content":""}`, true}, {`{"id":"1","attachments":[]}`, true},
	} {
		if got := isContentUpdate([]byte(raw.json)); got != raw.want {
			t.Fatalf("filter %s: %v", raw.json, got)
		}
	}

	for _, webhook := range []bool{false, true} {
		c, err := New("token", "guild", "channel", 100)
		if err != nil {
			t.Fatal(err)
		}
		c.webhookID, c.webhookToken = "123", "secret"
		c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body := `{"id":"42"}`
			if r.Method == http.MethodGet {
				if webhook {
					body = `{"id":"42","webhook_id":"123"}`
				}
			} else {
				if r.Method != http.MethodPatch || strings.Contains(r.URL.Path, "/webhooks/") != webhook {
					t.Fatalf("wrong edit endpoint: %s %s", r.Method, r.URL.Path)
				}
				var payload struct {
					Content     string            `json:"content"`
					Attachments []json.RawMessage `json:"attachments"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				want := "edited"
				if !webhook {
					want = "Alice: edited"
				}
				if payload.Content != want || payload.Attachments == nil {
					t.Fatalf("payload: %+v", payload)
				}
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if err := c.Edit(context.Background(), "42", bridge.OutboundMessage{Author: bridge.User{Name: "Alice"}, Text: "edited"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEditPartsGrowAndShrink(t *testing.T) {
	for _, reply := range []bool{false, true} {
		t.Run(fmt.Sprintf("reply=%t", reply), func(t *testing.T) {
			c, err := New("token", "guild", "channel", 1024)
			if err != nil {
				t.Fatal(err)
			}
			c.webhookID, c.webhookToken = "123", "secret"

			var requests []string
			c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				body := `{}`
				switch r.Method {
				case http.MethodGet:
					body = `{"id":"one","webhook_id":"123"}`
					if reply {
						body = `{"id":"one","message_reference":{"message_id":"original"}}`
					}
				case http.MethodPost:
					body = `{"id":"two"}`
				}
				if r.Method == http.MethodPatch || r.Method == http.MethodPost {
					var payload struct {
						Content   string `json:"content"`
						Reference *struct {
							ID string `json:"message_id"`
						} `json:"message_reference"`
					}
					if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
						reader, err := r.MultipartReader()
						if err != nil {
							t.Fatal(err)
						}
						part, err := reader.NextPart()
						if err != nil {
							t.Fatal(err)
						}
						if err := json.NewDecoder(part).Decode(&payload); err != nil {
							t.Fatal(err)
						}
						if r.Method != http.MethodPatch || !strings.HasSuffix(r.URL.Path, "/one") {
							t.Fatal("attachment not on first part")
						}
						if _, err := reader.NextPart(); err != nil {
							t.Fatalf("missing file: %v", err)
						}
					} else if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if utf16Units(payload.Content) > 2000 {
						t.Fatal("oversized part")
					}
					if reply && !strings.HasPrefix(payload.Content, "Alice: ") {
						t.Fatal("missing reply prefix")
					}
					if reply && r.Method == http.MethodPost && (payload.Reference == nil || payload.Reference.ID != "original") {
						t.Fatal("missing reply reference")
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			msg := bridge.OutboundMessage{
				Author: bridge.User{Name: "Alice"}, Text: strings.Repeat("a", 2100),
				Attachments: []bridge.Attachment{{Name: "a.txt", Data: []byte("file")}},
			}
			ids, err := c.EditParts(context.Background(), []string{"one"}, msg)
			if err != nil || len(ids) != 2 || ids[1] != "two" {
				t.Fatalf("grow: %v %v", ids, err)
			}
			msg.Text = "short"
			ids, err = c.EditParts(context.Background(), ids, msg)
			if err != nil || len(ids) != 1 || ids[0] != "one" {
				t.Fatalf("shrink: %v %v", ids, err)
			}
			if !strings.HasPrefix(requests[len(requests)-1], "DELETE ") || !strings.HasSuffix(requests[len(requests)-1], "/two") {
				t.Fatalf("delete: %v", requests)
			}
		})
	}
}

func TestSendVoiceAndEditCaption(t *testing.T) {
	for _, reply := range []bool{false, true} {
		t.Run(fmt.Sprintf("reply=%t", reply), func(t *testing.T) {
			c, err := New("token", "guild", "channel", 1024)
			if err != nil {
				t.Fatal(err)
			}
			c.webhookID, c.webhookToken = "123", "secret"
			c.session.Client = &http.Client{Transport: voiceTestTransport(t, reply)}

			m := bridge.OutboundMessage{
				Author: bridge.User{Name: "Alice"}, Text: "caption",
				Attachments: []bridge.Attachment{{
					Name: "voice.ogg", ContentType: "audio/ogg", Data: []byte("audio"),
					Voice: &bridge.Voice{Duration: 12, Waveform: []byte{0, 255}},
				}},
			}
			if reply {
				m.ReplyTo = "original"
			}
			ids, err := c.Send(context.Background(), m)
			if err != nil || fmt.Sprint(ids) != "[caption voice]" {
				t.Fatalf("send: %v %v", ids, err)
			}
			m.Text = "edited"
			m.Attachments = nil
			m.PreserveAttachments = true
			ids, err = c.EditParts(context.Background(), ids, m)
			if err != nil || fmt.Sprint(ids) != "[caption voice]" {
				t.Fatalf("edit: %v %v", ids, err)
			}
		})
	}
}

func voiceTestTransport(t *testing.T, reply bool) roundTripFunc {
	t.Helper()

	calls := 0
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{}`
		switch {
		case r.Method == http.MethodPost && calls == 1:
			var p struct {
				Content   string `json:"content"`
				Username  string `json:"username"`
				Reference *struct {
					ID string `json:"message_id"`
				} `json:"message_reference"`
			}
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Fatal(err)
			}
			if reply {
				if p.Content != "Alice: caption" || p.Reference == nil || p.Reference.ID != "original" {
					t.Fatalf("reply: %+v", p)
				}
			} else if p.Content != "caption" || p.Username != "Alice" {
				t.Fatalf("identity: %+v", p)
			}
			body = `{"id":"caption"}`
		case r.Method == http.MethodPost && calls == 2:
			if !strings.HasSuffix(r.URL.Path, "/channels/channel/messages") {
				t.Fatalf("voice endpoint: %s", r.URL)
			}
			reader, err := r.MultipartReader()
			if err != nil {
				t.Fatal(err)
			}
			part, err := reader.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			var p struct {
				Flags       int    `json:"flags"`
				Content     string `json:"content"`
				Attachments []struct {
					ID       int    `json:"id"`
					Filename string `json:"filename"`
					Duration int    `json:"duration_secs"`
					Waveform []byte `json:"waveform"`
				} `json:"attachments"`
			}
			if err := json.NewDecoder(part).Decode(&p); err != nil {
				t.Fatal(err)
			}
			if p.Flags != 8192 || p.Content != "" || len(p.Attachments) != 1 || p.Attachments[0].ID != 0 || p.Attachments[0].Filename != "voice.ogg" || p.Attachments[0].Duration != 12 || string(p.Attachments[0].Waveform) != "\x00\xff" {
				t.Fatalf("voice payload: %+v", p)
			}
			part, err = reader.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(part)
			if part.FormName() != "files[0]" || string(data) != "audio" {
				t.Fatalf("file: %s %q", part.FormName(), data)
			}
			if _, err := reader.NextPart(); err != io.EOF {
				t.Fatalf("extra file: %v", err)
			}
			body = `{"id":"voice"}`
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/voice"):
			body = `{"id":"voice","flags":8192}`
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/caption"):
			body = `{"id":"caption","webhook_id":"123"}`
			if reply {
				body = `{"id":"caption","message_reference":{"message_id":"original"}}`
			}
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/caption"):
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
}
