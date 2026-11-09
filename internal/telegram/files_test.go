package telegram

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestDownloadedAttachmentDoesNotExposeTokenURL(t *testing.T) {
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "getFile"):
			apiOK(t, w, `{"file_id":"file","file_path":"documents/report.txt","file_size":7}`)
		case strings.Contains(r.URL.Path, "/file/bot123:secret/"):
			if _, err := w.Write([]byte("content")); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	})
	m := &models.Message{
		ID:       1,
		Chat:     models.Chat{ID: -100},
		From:     &models.User{ID: 7},
		Caption:  "Report",
		Document: &models.Document{FileID: "file", FileName: "report.txt", MimeType: "text/plain", FileSize: 7},
	}
	if err := c.message(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if len(h.messages) != 1 || len(h.messages[0].Attachments) != 1 {
		t.Fatalf("messages: %+v", h.messages)
	}
	attachment := h.messages[0].Attachments[0]
	if attachment.URL != "" || string(attachment.Data) != "content" {
		t.Fatalf("attachment: %+v", attachment)
	}
}

func TestFileLookupFailureShowsTelegramAPIError(t *testing.T) {
	c, h := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`, http.StatusInternalServerError)
	})
	m := &models.Message{
		ID: 1, Chat: models.Chat{ID: -100}, From: &models.User{ID: 7},
		Photo: []models.PhotoSize{{FileID: "photo", FileSize: 2370}},
	}
	if err := c.message(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if len(h.messages) != 1 || len(h.messages[0].Attachments) != 0 ||
		!strings.Contains(h.messages[0].Text.Value, "Attachment unavailable: photo.jpg; could not retrieve it due to a Telegram API error") {
		t.Fatalf("messages: %+v", h.messages)
	}
}

func TestSetReactionSetsAndClearsSingleEmoji(t *testing.T) {
	var reactions []string
	var present []bool
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		reactions = append(reactions, r.FormValue("reaction"))
		_, exists := r.MultipartForm.Value["reaction"]
		present = append(present, exists)
		if r.FormValue("message_id") != "42" {
			t.Errorf("message ID: %s", r.FormValue("message_id"))
		}
		apiOK(t, w, `true`)
	})
	if err := c.SetReaction(context.Background(), "42", "👍", true); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReaction(context.Background(), "42", "👍", false); err != nil {
		t.Fatal(err)
	}
	if len(reactions) != 2 || !strings.Contains(reactions[0], `"emoji":"👍"`) || reactions[1] != "[]" || !present[1] {
		t.Fatalf("reactions: %v", reactions)
	}
}
