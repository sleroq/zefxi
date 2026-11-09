package telegram

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"image"
	"image/gif"
	_ "image/png"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func emojiMessage() *models.Message {
	return &models.Message{
		ID: 1, From: &models.User{ID: 7, FirstName: "Alice"},
		ReplyToMessage: &models.Message{ID: 2}, Text: " \t😀 \n",
		Entities: []models.MessageEntity{
			{Type: "bold", Offset: 0, Length: 6},
			{Type: "custom_emoji", Offset: 2, Length: 2, CustomEmojiID: "premium"},
		},
	}
}

func TestCustomEmojiConversion(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	webp, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=red:size=128x128", "-frames:v", "1", "-f", "webp", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	video, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=red:size=128x128:rate=20", "-t", "0.2",
		"-c:v", "libvpx-vp9", "-f", "webm", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, sticker string
		data          []byte
		mime          string
	}{
		{"static", `{"file_id":"s"}`, webp, "image/png"},
		{"video", `{"file_id":"s","is_video":true}`, video, "image/gif"},
	}
	if _, err := exec.LookPath("lottieconverter"); err == nil {
		fixture, err := os.ReadFile("../animation/testdata/sticker.json")
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		if _, err := w.Write(fixture); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, struct {
			name, sticker string
			data          []byte
			mime          string
		}{"tgs", `{"file_id":"s","is_animated":true}`, b.Bytes(), "image/gif"})
	} else {
		t.Log("lottieconverter not installed; TGS conversion skipped")
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "getCustomEmojiStickers"):
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
					}
					if !strings.Contains(r.FormValue("custom_emoji_ids"), "premium") {
						t.Error("missing emoji id")
					}
					apiOK(t, w, "["+tc.sticker+"]")
				case strings.HasSuffix(r.URL.Path, "getFile"):
					apiOK(t, w, `{"file_id":"s","file_path":"emoji/file"}`)
				default:
					if _, err := w.Write(tc.data); err != nil {
						t.Error(err)
					}
				}
			})
			if err := c.message(context.Background(), emojiMessage()); err != nil {
				t.Fatal(err)
			}
			if len(h.messages) != 1 {
				t.Fatal(h.messages)
			}
			m := h.messages[0]
			if m.Text.Value != "" || m.Author.ID != "7" || m.ReplyTo != "2" || len(m.Attachments) != 1 {
				t.Fatalf("message: %+v", m)
			}
			a := m.Attachments[0]
			if a.ContentType != tc.mime {
				t.Fatalf("mime: %s", a.ContentType)
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(a.Data))
			if err != nil || cfg.Width != 64 || cfg.Height != 64 {
				t.Fatalf("image dimensions: %+v, %v", cfg, err)
			}
			if tc.mime == "image/gif" {
				animated, err := gif.DecodeAll(bytes.NewReader(a.Data))
				if err != nil || len(animated.Image) < 2 {
					t.Fatalf("emoji is not animated: %v", err)
				}
			}
		})
	}
}

func TestCustomEmojiFallbackAndMixedText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		change   func(*models.Message)
		failure  string
		requests bool
	}{
		{"mixed", func(m *models.Message) { m.Text += "x" }, "", false},
		{"multiple", func(m *models.Message) {
			other := models.MessageEntity{Type: "custom_emoji", Offset: 2, Length: 2, CustomEmojiID: "other"}
			m.Entities = append(m.Entities, other)
		}, "", false},
		{"lookup", func(*models.Message) {}, "lookup", true},
		{"download", func(*models.Message) {}, "download", true},
		{"conversion", func(*models.Message) {}, "conversion", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch {
				case strings.HasSuffix(r.URL.Path, "getCustomEmojiStickers"):
					if tc.failure == "lookup" {
						http.Error(w, "failure", 500)
					} else {
						apiOK(t, w, `[{"file_id":"s"}]`)
					}
				case strings.HasSuffix(r.URL.Path, "getFile"):
					apiOK(t, w, `{"file_id":"s","file_path":"emoji/file"}`)
				default:
					if tc.failure == "download" {
						http.Error(w, "failure", 500)
					} else {
						_, _ = w.Write([]byte("invalid"))
					}
				}
			})

			m := emojiMessage()
			tc.change(m)
			if err := c.message(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			if len(h.messages) != 1 || h.messages[0].Text.Value != m.Text || len(h.messages[0].Attachments) != 0 {
				t.Fatalf("fallback: %+v", h.messages)
			}
			if (calls > 0) != tc.requests {
				t.Fatalf("requests: %d", calls)
			}
		})
	}
}

func TestCustomEmojiCancellation(t *testing.T) {
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.message(ctx, emojiMessage()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
	if len(h.messages) != 0 {
		t.Fatal(h.messages)
	}
}
