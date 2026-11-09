package telegram

import (
	"bytes"
	"compress/gzip"
	"context"
	"image/gif"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func TestIncomingAnimationBecomesDiscordGIF(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=20", "-t", "0.5", "-c:v", "mpeg4",
		"-movflags", "frag_keyframe+empty_moov", "-f", "mp4", "pipe:1")
	video, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "getFile"):
			apiOK(t, w, `{"file_id":"gif","file_path":"animations/gif.mp4"}`)
		case strings.Contains(r.URL.Path, "/file/bot123:secret/"):
			if _, err := w.Write(video); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	})
	message := &models.Message{
		ID: 1, Chat: models.Chat{ID: -100}, From: &models.User{ID: 7, FirstName: "Alice"},
		Animation: &models.Animation{FileID: "gif", FileName: "funny.mp4", MimeType: "video/mp4"},
	}
	if err := c.message(ctx, message); err != nil {
		t.Fatal(err)
	}
	if len(h.messages) != 1 || len(h.messages[0].Attachments) != 1 {
		t.Fatalf("messages: %+v", h.messages)
	}
	got := h.messages[0].Attachments[0]
	if got.Name != "funny.gif" || got.ContentType != "image/gif" || got.Size != int64(len(got.Data)) {
		t.Fatalf("converted attachment: %+v", got)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(got.Data))
	if err != nil || len(animation.Image) < 2 {
		t.Fatalf("invalid animated GIF: %v", err)
	}
}

func TestIncomingAnimatedStickerBecomesDiscordGIF(t *testing.T) {
	if _, err := exec.LookPath("lottieconverter"); err != nil {
		t.Skip("lottieconverter not installed")
	}

	fixture, err := os.ReadFile("../animation/testdata/sticker.json")
	if err != nil {
		t.Fatal(err)
	}
	var tgs bytes.Buffer
	writer := gzip.NewWriter(&tgs)
	if _, err := writer.Write(fixture); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "getFile") {
			apiOK(t, w, `{"file_id":"sticker","file_path":"stickers/sticker.tgs"}`)
			return
		}
		if _, err := w.Write(tgs.Bytes()); err != nil {
			t.Error(err)
		}
	})

	message := &models.Message{
		ID: 1, Chat: models.Chat{ID: -100}, From: &models.User{ID: 7, FirstName: "Alice"},
		Sticker: &models.Sticker{FileID: "sticker", IsAnimated: true},
	}
	if err := c.message(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(h.messages) != 1 || len(h.messages[0].Attachments) != 1 {
		t.Fatalf("messages: %+v", h.messages)
	}
	got := h.messages[0].Attachments[0]
	if got.Name != "sticker.gif" || got.ContentType != "image/gif" {
		t.Fatalf("sticker: %+v", got)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(got.Data))
	if err != nil || len(animation.Image) < 2 {
		t.Fatalf("invalid sticker GIF: %v", err)
	}
}

func TestAnimationConversionFailureKeepsOriginalWithNotice(t *testing.T) {
	video := []byte("not a valid MP4")
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "getFile") {
			apiOK(t, w, `{"file_id":"gif","file_path":"animations/gif.mp4"}`)
			return
		}
		if _, err := w.Write(video); err != nil {
			t.Error(err)
		}
	})
	message := &models.Message{
		ID: 1, Chat: models.Chat{ID: -100}, From: &models.User{ID: 7, FirstName: "Alice"},
		Animation: &models.Animation{FileID: "gif", FileName: "funny.mp4", MimeType: "video/mp4"},
	}
	if err := c.message(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(h.messages) != 1 || len(h.messages[0].Attachments) != 1 {
		t.Fatalf("messages: %+v", h.messages)
	}
	got := h.messages[0]
	if !strings.Contains(got.Text.Value, "could not be converted") || !bytes.Equal(got.Attachments[0].Data, video) {
		t.Fatalf("conversion fallback: %+v", got)
	}
}

func TestTelegramStickerAnimationKinds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sticker  *models.Sticker
		animated bool
		mime     string
	}{
		{"static", &models.Sticker{FileID: "s"}, false, "image/webp"},
		{"lottie", &models.Sticker{FileID: "s", IsAnimated: true}, true, "application/x-tgsticker"},
		{"video", &models.Sticker{FileID: "s", IsVideo: true}, true, "video/webm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := messageFiles(&models.Message{Sticker: tc.sticker})
			if len(files) != 1 || files[0].attachment.Animated != tc.animated || files[0].attachment.ContentType != tc.mime {
				t.Fatalf("files: %+v", files)
			}
		})
	}
}
