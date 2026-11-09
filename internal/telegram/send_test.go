package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/sleroq/zefxi/internal/bridge"
)

func TestMediaDelivery(t *testing.T) {
	var paths []string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		paths = append(paths, method)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("reply_parameters") != `{"message_id":42}` {
			t.Errorf("reply: %s", r.FormValue("reply_parameters"))
		}
		if method == "sendMediaGroup" {
			var raw []map[string]string
			if err := json.Unmarshal([]byte(r.FormValue("media")), &raw); err != nil {
				t.Fatal(err)
			}
			if len(raw) != 2 {
				t.Fatalf("media: %v", raw)
			}
			for i, item := range raw {
				want := ""
				if i == 0 {
					want = "<b>A&amp;&lt;</b>"
				}
				if item["caption"] != want || item["parse_mode"] != "HTML" {
					t.Errorf("item: %v", item)
				}
				key := strings.TrimPrefix(item["media"], "attach://")
				if len(r.MultipartForm.File[key]) != 1 {
					t.Errorf("missing part %s", key)
				}
				if item["type"] != []string{"photo", "video"}[i] {
					t.Errorf("type: %v", item)
				}
			}
			if raw[0]["media"] == raw[1]["media"] {
				t.Error("duplicate attachment names")
			}
			apiOK(t, w, `[{"message_id":2},{"message_id":3}]`)
			return
		}
		field := map[string]string{"sendAnimation": "animation", "sendDocument": "document"}[method]
		if len(r.MultipartForm.File[field]) != 1 {
			t.Errorf("missing %s", field)
		}
		want := "<b>A&amp;&lt;</b>"
		if method == "sendAnimation" {
			want += ": <i>body</i>"
		}
		if r.FormValue("caption") != want {
			t.Errorf("caption: %s", r.FormValue("caption"))
		}
		apiOK(t, w, fmt.Sprintf(`{"message_id":%d}`, map[string]int{"sendAnimation": 1, "sendDocument": 4}[method]))
	})

	outbound := bridge.OutboundMessage{Author: bridge.User{Username: "A&<"}, Text: "<i>body</i>", ReplyTo: "42", Attachments: []bridge.Attachment{
		{Name: "gif.mp4", ContentType: "video/mp4", Animated: true, Data: []byte("gif")},
		{Name: "same", ContentType: "image/png", Data: []byte("png")},
		{Name: "same", ContentType: "video/mp4", Data: []byte("mp4")},
		{Name: "unknown", Data: []byte("file")},
	}}
	ids, err := c.Send(context.Background(), outbound)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "1,2,3,4" || strings.Join(paths, ",") != "sendAnimation,sendMediaGroup,sendDocument" {
		t.Fatalf("ids %v paths %v", ids, paths)
	}
}

func TestPhotoAlbumHasOneCaption(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "sendMediaGroup") {
			t.Fatalf("unexpected method: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		var media []map[string]string
		if err := json.Unmarshal([]byte(r.FormValue("media")), &media); err != nil {
			t.Fatal(err)
		}
		if len(media) != 2 || media[0]["caption"] != "<b>Alice</b>: hello" || media[1]["caption"] != "" {
			t.Fatalf("album: %v", media)
		}
		apiOK(t, w, `[{"message_id":11},{"message_id":12}]`)
	})
	outbound := bridge.OutboundMessage{Author: bridge.User{Name: "Alice"}, Text: "hello", Attachments: []bridge.Attachment{
		{Name: "a.jpg", ContentType: "image/jpeg", Data: []byte("photo one")},
		{Name: "b.png", Data: []byte("photo two")},
	}}
	ids, err := c.Send(context.Background(), outbound)
	if err != nil || strings.Join(ids, ",") != "11,12" {
		t.Fatalf("ids %v error %v", ids, err)
	}
}

func TestLongCaptionSendsBodyAfterMedia(t *testing.T) {
	calls := 0
	body := "<b>" + strings.Repeat("😀", 600) + "</b>"
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if calls <= 2 && r.FormValue("caption") != "<b>A</b>" {
			t.Error("missing author caption")
		}
		if calls == 3 && r.FormValue("text") != "<b>A</b>: "+body {
			t.Error("body corrupted")
		}
		apiOK(t, w, fmt.Sprintf(`{"message_id":%d}`, calls))
	})

	outbound := bridge.OutboundMessage{Author: bridge.User{Name: "A"}, Text: body,
		Attachments: []bridge.Attachment{
			{Name: "a.gif", ContentType: "image/gif", Data: []byte("gif")},
			{Name: "b.png", ContentType: "image/png", Data: []byte("png")}}}
	ids, err := c.Send(context.Background(), outbound)
	if err != nil || strings.Join(ids, ",") != "1,2,3" {
		t.Fatalf("ids %v error %v", ids, err)
	}
}

func TestMediaDeliveryReturnsPartialIDs(t *testing.T) {
	calls := 0
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			apiOK(t, w, `{"message_id":7}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	})
	outbound := bridge.OutboundMessage{Author: bridge.User{Name: "A"}, Attachments: []bridge.Attachment{
		{Name: "a.gif", ContentType: "image/gif", Data: []byte("gif")},
		{Name: "b.png", ContentType: "image/png", Data: []byte("png")},
	}}
	ids, err := c.Send(context.Background(), outbound)
	if err == nil || strings.Join(ids, ",") != "7" {
		t.Fatalf("ids %v error %v", ids, err)
	}
}

func TestSendLinkedAuthor(t *testing.T) {
	for _, attachment := range []bool{false, true} {
		t.Run(fmt.Sprint("attachment=", attachment), func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				field := "text"
				if attachment {
					field = "caption"
				}
				want := `<b><a href="tg://user?id=7&amp;&#34;">A&amp;B</a></b>: hello`
				if r.FormValue(field) != want || r.FormValue("parse_mode") != "HTML" {
					t.Errorf("%s=%q parse_mode=%q", field, r.FormValue(field), r.FormValue("parse_mode"))
				}
				apiOK(t, w, `{"message_id":1}`)
			})
			m := bridge.OutboundMessage{Author: bridge.User{Name: "A&B"}, TelegramUserID: `7&"`, Text: "hello"}
			if attachment {
				m.Attachments = []bridge.Attachment{{Name: "a.txt", Data: []byte("hello")}}
			}
			if _, err := c.Send(context.Background(), m); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEditTextAndMedia(t *testing.T) {
	var methods []string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		methods = append(methods, method)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if method == "editMessageText" && r.FormValue("text") != "<b>Alice</b>: hello" {
			t.Fatalf("text %q", r.FormValue("text"))
		}
		if method == "editMessageMedia" {
			var media map[string]any
			if err := json.Unmarshal([]byte(r.FormValue("media")), &media); err != nil {
				t.Fatal(err)
			}
			if media["caption"] != "<b>Alice</b>: hello" || media["type"] != "photo" {
				t.Fatalf("media %+v", media)
			}
		}
		apiOK(t, w, `{"message_id":1}`)
	})

	m := bridge.OutboundMessage{Author: bridge.User{Name: "Alice"}, Text: "hello"}
	if err := c.Edit(context.Background(), "1", m); err != nil {
		t.Fatal(err)
	}
	m.Attachments = []bridge.Attachment{{Name: "a.png", ContentType: "image/png", Data: []byte("data")}}
	if err := c.Edit(context.Background(), "1", m); err != nil {
		t.Fatal(err)
	}
	m.PreserveAttachments = true
	if err := c.Edit(context.Background(), "1", m); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "editMessageText,editMessageMedia,editMessageCaption" {
		t.Fatal(methods)
	}
	m.Attachments = append(m.Attachments, m.Attachments[0])
	if err := c.Edit(context.Background(), "1", m); err == nil {
		t.Fatal("multipart edit must fail")
	}
}

func TestStickerDelivery(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip(err)
	}

	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 10, 8))); err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("fail=", fail), func(t *testing.T) {
			var methods []string
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				methods = append(methods, method)
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				if r.FormValue("reply_parameters") != `{"message_id":42}` {
					t.Errorf("reply: %s", r.FormValue("reply_parameters"))
				}
				if method == "sendMessage" && r.FormValue("text") != "<b>Alice</b>: hello" {
					t.Errorf("text: %s", r.FormValue("text"))
				}
				if method == "sendSticker" {
					if len(r.MultipartForm.File["sticker"]) != 1 || r.FormValue("caption") != "" {
						t.Error("invalid sticker upload")
					}
					if fail {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
				}
				apiOK(t, w, fmt.Sprintf(`{"message_id":%d}`, len(methods)))
			})
			m := bridge.OutboundMessage{Author: bridge.User{Name: "Alice"}, Text: "hello", ReplyTo: "42", Attachments: []bridge.Attachment{
				{Name: "sticker.png", StickerFormat: bridge.StickerPNG, Data: source.Bytes()},
				{Name: "file.txt", Data: []byte("file")},
			}}
			ids, err := c.Send(context.Background(), m)
			if fail {
				if err == nil || strings.Join(ids, ",") != "1" || strings.Join(methods, ",") != "sendMessage,sendSticker" {
					t.Fatalf("ids %v methods %v error %v", ids, methods, err)
				}
			} else if err != nil || strings.Join(ids, ",") != "1,2,3" || strings.Join(methods, ",") != "sendMessage,sendSticker,sendDocument" {
				t.Fatalf("ids %v methods %v error %v", ids, methods, err)
			}
		})
	}
}

func TestStickerConversionFailureSendsSourceLink(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(r.FormValue("text"), "https://example.com/sticker.png") {
			t.Errorf("text: %s", r.FormValue("text"))
		}
		apiOK(t, w, `{"message_id":5}`)
	})
	attachment := bridge.Attachment{
		Name: "bad.png", StickerFormat: bridge.StickerPNG, Data: []byte("bad"),
		URL: "https://example.com/sticker.png",
	}
	m := bridge.OutboundMessage{Author: bridge.User{Name: "A"}, Attachments: []bridge.Attachment{attachment}}
	ids, err := c.Send(context.Background(), m)
	if err == nil || strings.Join(ids, ",") != "5" {
		t.Fatalf("ids %v error %v", ids, err)
	}
	if err := c.Edit(context.Background(), "5", m); err == nil {
		t.Fatal("sticker edit must fail")
	}
}

func TestDeleteMessage(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/deleteMessage") {
			t.Errorf("method: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("message_id") != "42" || r.FormValue("chat_id") != "-100" {
			t.Errorf("form: %v", r.Form)
		}
		apiOK(t, w, `true`)
	})
	if err := c.Delete(context.Background(), "42"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), "invalid"); err == nil {
		t.Fatal("expected invalid ID error")
	}
}
