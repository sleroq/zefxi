package discord

import (
	"bytes"
	"context"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sleroq/zefxi/internal/bridge"
)

type gifHandler struct {
	bridge.Handler
	messages chan bridge.Message
}

func (h *gifHandler) HandleMessage(_ context.Context, _ bridge.Platform, m bridge.Message) error {
	h.messages <- m
	return nil
}

const gifShareLink = "https://tenor.com/view/cat-123"

func gifEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Type: "gifv", URL: gifShareLink,
		Video: &discordgo.MessageEmbedVideo{URL: "https://images-ext-1.discordapp.net/cat.mp4"},
	}
}

func gifDownloadClient(t *testing.T, fail bool) (*Client, <-chan string) {
	t.Helper()
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.URL.Path == "/cat.gif" {
			_, _ = w.Write([]byte("GIF89a-data"))
		} else {
			_, _ = w.Write([]byte("\x00\x00\x00\x18ftypmp42"))
		}
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	return &Client{http: &http.Client{Transport: stickerTransport{
		target: target.Host, base: http.DefaultTransport,
	}}}, requests
}

func assertGIFAnimation(t *testing.T, m bridge.Message, contentType string) {
	t.Helper()
	if len(m.Attachments) != 1 || !m.Attachments[0].Animated ||
		m.Attachments[0].ContentType != contentType || m.Text.Value != "" {
		t.Fatalf("animation: %+v", m)
	}
}

func nextGIFMessage(t *testing.T, h *gifHandler) bridge.Message {
	t.Helper()
	select {
	case m := <-h.messages:
		return m
	case <-time.After(time.Second):
		t.Fatal("message not forwarded")
		return bridge.Message{}
	}
}

func TestGIFMediaIngestion(t *testing.T) {
	for _, tc := range []struct {
		name, content, wantType, wantPath string
		cached, fail                      bool
	}{
		{name: "cached", content: gifShareLink, cached: true, wantType: "video/mp4", wantPath: "/cat.mp4"},
		{name: "direct", content: "https://media.tenor.com/cat.gif", wantType: "image/gif", wantPath: "/cat.gif"},
		{name: "discord CDN", content: "https://cdn.discordapp.com/cat.gif", wantType: "image/gif", wantPath: "/cat.gif"},
		{name: "failure", content: gifShareLink, cached: true, fail: true, wantPath: "/cat.mp4"},
		{name: "unsafe", content: "https://localhost/private.gif"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, requests := gifDownloadClient(t, tc.fail)
			h := &gifHandler{messages: make(chan bridge.Message, 1)}
			e := &discordgo.MessageCreate{Message: &discordgo.Message{
				ID: "1", Content: tc.content, Author: &discordgo.User{ID: "author"},
			}}
			if tc.cached {
				e.Embeds = []*discordgo.MessageEmbed{gifEmbed()}
			}

			c.handleMessage(context.Background(), h, e)
			m := nextGIFMessage(t, h)
			if tc.wantType != "" {
				assertGIFAnimation(t, m, tc.wantType)
			} else if len(m.Attachments) != 0 || m.Text.Value != tc.content {
				t.Fatalf("fallback: %+v", m)
			}
			if tc.wantPath != "" {
				if got := <-requests; got != tc.wantPath {
					t.Fatalf("media path: %s", got)
				}
			} else if len(requests) != 0 {
				t.Fatal("unsafe URL fetched")
			}
		})
	}
}

func TestGIFGatewayPendingUpdates(t *testing.T) {
	for _, update := range []bool{true, false} {
		name := "timeout"
		if update {
			name = "uncached update"
		}
		t.Run(name, func(t *testing.T) {
			c, _ := gifDownloadClient(t, false)
			ctx, cancel := context.WithCancel(context.Background())
			queue := newEventQueue(ctx)
			defer func() { cancel(); <-queue.done }()
			pending := newGIFLookups(30 * time.Millisecond)
			h := &gifHandler{messages: make(chan bridge.Message, 4)}
			e := &discordgo.MessageCreate{Message: &discordgo.Message{
				ID: "1", Content: gifShareLink, Author: &discordgo.User{ID: "author"},
			}}

			lookup := pending.register(e)
			queue.enqueue(ctx, func() { pending.await(ctx, e, lookup); c.handleMessage(ctx, h, e) })
			// Unrelated edits cannot satisfy the lookup; delivery cannot wait for the worker.
			pending.deliver(&discordgo.MessageUpdate{Message: &discordgo.Message{
				ID: "other", Embeds: []*discordgo.MessageEmbed{gifEmbed()},
			}})
			if update {
				pending.deliver(&discordgo.MessageUpdate{Message: &discordgo.Message{
					ID: "1", Embeds: []*discordgo.MessageEmbed{gifEmbed()},
				}})
			}
			queue.enqueue(ctx, func() {
				c.handleMessage(ctx, h, &discordgo.MessageCreate{Message: &discordgo.Message{
					ID: "2", Content: "next", Author: e.Author,
				}})
			})

			m := nextGIFMessage(t, h)
			if m.ID != "1" {
				t.Fatalf("out of order: %s", m.ID)
			}
			if update {
				assertGIFAnimation(t, m, "video/mp4")
			} else if len(m.Attachments) != 0 || m.Text.Value != gifShareLink {
				t.Fatalf("timeout fallback: %+v", m)
			}
			if next := nextGIFMessage(t, h); next.ID != "2" {
				t.Fatalf("out of order: %+v", next)
			}
			pending.deliver(&discordgo.MessageUpdate{Message: &discordgo.Message{
				ID: "1", Embeds: []*discordgo.MessageEmbed{gifEmbed()},
			}})
			barrier := make(chan struct{})
			queue.enqueue(ctx, func() { close(barrier) })
			<-barrier
			if len(h.messages) != 0 || len(pending.pending) != 0 {
				t.Fatal("duplicate forward or leaked lookup")
			}
		})
	}
}

func TestDiscordWebMGIFBecomesGIF(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=20", "-t", "0.5", "-c:v", "libvpx-vp9", "-f", "webm", "pipe:1")
	video, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write(video); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{http: &http.Client{Transport: stickerTransport{target: target.Host, base: http.DefaultTransport}}}
	h := &gifHandler{messages: make(chan bridge.Message, 1)}
	e := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1", Content: gifShareLink, Author: &discordgo.User{ID: "author"},
		Embeds: []*discordgo.MessageEmbed{{Type: "gifv", URL: gifShareLink,
			Video: &discordgo.MessageEmbedVideo{URL: "https://images-ext-1.discordapp.net/cat.webm"}}},
	}}
	c.handleMessage(ctx, h, e)
	message := nextGIFMessage(t, h)
	assertGIFAnimation(t, message, "image/gif")
	decoded, err := gif.DecodeAll(bytes.NewReader(message.Attachments[0].Data))
	if err != nil || len(decoded.Image) < 2 {
		t.Fatalf("invalid GIF: %v", err)
	}
}

func TestGIFLinkWithTrailingPunctuation(t *testing.T) {
	c, requests := gifDownloadClient(t, false)
	h := &gifHandler{messages: make(chan bridge.Message, 1)}
	e := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1", Content: "Look: https://media.tenor.com/cat.gif.", Author: &discordgo.User{ID: "author"},
	}}
	c.handleMessage(context.Background(), h, e)
	message := nextGIFMessage(t, h)
	if len(message.Attachments) != 1 || !message.Attachments[0].Animated || message.Text.Value != "Look: ." {
		t.Fatalf("GIF with punctuation: %+v", message)
	}
	if got := <-requests; got != "/cat.gif" {
		t.Fatalf("media path: %s", got)
	}
}

func TestDiscordAttachmentGIFWaitsForSignedEmbed(t *testing.T) {
	c, requests := gifDownloadClient(t, false)
	h := &gifHandler{messages: make(chan bridge.Message, 1)}
	link := "https://cdn.discordapp.com/attachments/1/2/cat.gif"
	e := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1", Content: link, Author: &discordgo.User{ID: "author"},
	}}
	pending := newGIFLookups(time.Second)
	lookup := pending.register(e)
	if lookup == nil {
		t.Fatal("unsigned attachment link was not registered for its embed")
	}
	pending.deliver(&discordgo.MessageUpdate{Message: &discordgo.Message{
		ID: "1", Embeds: []*discordgo.MessageEmbed{{Type: "image", Image: &discordgo.MessageEmbedImage{
			URL: link + "?ex=1&is=2&hm=signature", ProxyURL: "https://media.discordapp.net/cat.gif?ex=1&hm=signature",
		}}},
	}})
	pending.await(context.Background(), e, lookup)
	c.handleMessage(context.Background(), h, e)
	assertGIFAnimation(t, nextGIFMessage(t, h), "image/gif")
	if got := <-requests; got != "/cat.gif" {
		t.Fatalf("proxy path: %s", got)
	}
}

func TestDiscordAttachmentGIFThumbnailEmbed(t *testing.T) {
	c, requests := gifDownloadClient(t, false)
	h := &gifHandler{messages: make(chan bridge.Message, 1)}
	link := "https://cdn.discordapp.com/attachments/1/2/cat.gif"
	e := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1", Content: link, Author: &discordgo.User{ID: "author"},
	}}
	pending := newGIFLookups(time.Second)
	lookup := pending.register(e)
	if lookup == nil {
		t.Fatal("attachment link was not registered for its embed")
	}
	update, err := decodeGIFMessage([]byte(`{"id":"1","embeds":[{"type":"image",` +
		`"url":"https://cdn.discordapp.com/attachments/1/2/cat.gif",` +
		`"thumbnail":{"url":"https://cdn.discordapp.com/cat.gif?ex=1&hm=signed",` +
		`"proxy_url":"https://media.discordapp.net/cat.gif?ex=1&hm=signed"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	pending.deliver(&discordgo.MessageUpdate{Message: update})
	pending.await(context.Background(), e, lookup)
	c.handleMessage(context.Background(), h, e)
	assertGIFAnimation(t, nextGIFMessage(t, h), "image/gif")
	if got := <-requests; got != "/cat.gif" {
		t.Fatalf("GIF path: %s", got)
	}
}

func TestGIFImageEmbedUsesDiscordProxy(t *testing.T) {
	c, requests := gifDownloadClient(t, false)
	h := &gifHandler{messages: make(chan bridge.Message, 1)}
	link := "https://example.org/cat.gif"
	e := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1", Content: link, Author: &discordgo.User{ID: "author"},
	}}
	embed := &discordgo.MessageEmbed{Type: "image", URL: link, Image: &discordgo.MessageEmbedImage{
		URL: link, ProxyURL: "https://images-ext-1.discordapp.net/cat.gif",
	}}
	pending := newGIFLookups(time.Second)
	lookup := pending.register(e)
	if lookup == nil {
		t.Fatal("GIF link was not registered for its embed update")
	}
	pending.deliver(&discordgo.MessageUpdate{Message: &discordgo.Message{
		ID: "1", Embeds: []*discordgo.MessageEmbed{embed},
	}})
	pending.await(context.Background(), e, lookup)
	c.handleMessage(context.Background(), h, e)
	assertGIFAnimation(t, nextGIFMessage(t, h), "image/gif")
	if got := <-requests; got != "/cat.gif" {
		t.Fatalf("proxy path: %s", got)
	}
}

func TestUploadedGIFAnimated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("GIF89a"))
	}))
	defer server.Close()
	c := &Client{http: server.Client()}
	a, err := c.download(context.Background(), &discordgo.MessageAttachment{
		URL: server.URL, Filename: "original.gif", ContentType: "image/gif",
	})
	if err != nil || !a.Animated || a.Name != "original.gif" || string(a.Data) != "GIF89a" {
		t.Fatalf("upload changed: %+v, %v", a, err)
	}
}

func TestGIFVideoProxyDecode(t *testing.T) {
	data := []byte(`{"embeds":[{"type":"gifv","video":{
 "url":"https://media.tenor.com/a.mp4",
 "proxy_url":"https://images-ext-1.discordapp.net/a.mp4"
 }}]}`)
	m, err := decodeGIFMessage(data)
	if err != nil || m.Embeds[0].Video.URL != "https://images-ext-1.discordapp.net/a.mp4" {
		t.Fatalf("proxy lost: %+v, %v", m, err)
	}
}

func TestAnimatedWebPIngestion(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	file := t.TempDir() + "/animation.webp"
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=10", "-t", "0.5", "-c:v", "libwebp_anim", file)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate WebP: %v: %s", err, output)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	c := &Client{http: &http.Client{Transport: stickerTransport{target: target.Host, base: http.DefaultTransport}}}
	for _, link := range []string{"https://gif.fxtwitter.com/tweet_video/HEAGAJPW0AAR54T.webp", "https://example.org/animation.webp"} {
		t.Run(link, func(t *testing.T) {
			e := &discordgo.MessageCreate{Message: &discordgo.Message{
				ID: "1", Content: link, Author: &discordgo.User{ID: "author"},
			}}
			if strings.Contains(link, "example.org") {
				pending := newGIFLookups(time.Second)
				lookup := pending.register(e)
				if lookup == nil {
					t.Fatal("missing delayed embed lookup")
				}
				embed := &discordgo.MessageEmbed{Type: "image", URL: link, Image: &discordgo.MessageEmbedImage{
					URL: link, ProxyURL: "https://images-ext-1.discordapp.net/animation.webp",
				}}
				pending.deliver(&discordgo.MessageUpdate{Message: &discordgo.Message{
					ID: "1", Embeds: []*discordgo.MessageEmbed{embed},
				}})
				pending.await(ctx, e, lookup)
			}
			h := &gifHandler{messages: make(chan bridge.Message, 1)}
			c.handleMessage(ctx, h, e)
			m := nextGIFMessage(t, h)
			assertGIFAnimation(t, m, "image/gif")
			decoded, err := gif.DecodeAll(bytes.NewReader(m.Attachments[0].Data))
			if err != nil || len(decoded.Image) < 2 {
				t.Fatalf("invalid animation: %v", err)
			}
		})
	}
}

func TestGIFDoesNotFollowUnsafeRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cat.gif" {
			t.Error("unsafe redirect fetched")
		}
		http.Redirect(w, r, "https://localhost/private.gif", http.StatusFound)
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	c := &Client{http: &http.Client{Transport: stickerTransport{target: target.Host, base: http.DefaultTransport}}}
	link := "https://cdn.discordapp.com/cat.gif"
	content, attachments := c.downloadGIFs(context.Background(), &discordgo.Message{Content: link})
	if content != link || len(attachments) != 0 {
		t.Fatalf("unexpected result: %q %+v", content, attachments)
	}
}

func TestGIFCaptionEditPreservesMedia(t *testing.T) {
	c, _ := gifDownloadClient(t, false)
	h := &editHandler{}
	for _, tc := range []struct {
		content     string
		attachments []*discordgo.MessageAttachment
	}{
		{content: "new caption https://cdn.discordapp.com/cat.gif"},
		{content: "new caption", attachments: []*discordgo.MessageAttachment{{URL: "https://cdn.discordapp.com/cat.gif", Filename: "cat.gif", ContentType: "image/gif"}}},
	} {
		e := &discordgo.MessageCreate{Message: &discordgo.Message{
			ID: "1", Content: tc.content, Author: &discordgo.User{ID: "author"}, Attachments: tc.attachments,
		}}
		c.forwardMessage(context.Background(), h, e, true, true)
		m := h.edits[len(h.edits)-1]
		if !m.PreserveAttachments || len(m.Attachments) != 1 || !m.Attachments[0].Animated || m.Text.Value != "new caption" {
			t.Fatalf("edit: %+v", m)
		}
	}
}

func TestPreserveEditedMedia(t *testing.T) {
	gifLink := "https://cdn.discordapp.com/cat.gif"
	attachment := &discordgo.MessageAttachment{ID: "attachment"}
	for _, tc := range []struct {
		name, before, after            string
		oldAttachments, newAttachments []*discordgo.MessageAttachment
		want                           bool
	}{
		{name: "link caption", before: "old " + gifLink, after: "new " + gifLink, want: true},
		{name: "changed link", before: gifLink, after: "https://cdn.discordapp.com/other.gif"},
		{name: "attachment caption", before: "old", after: "new", oldAttachments: []*discordgo.MessageAttachment{attachment}, newAttachments: []*discordgo.MessageAttachment{attachment}, want: true},
		{name: "removed attachment", before: "old", after: "new", oldAttachments: []*discordgo.MessageAttachment{attachment}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := &discordgo.Message{Content: tc.before, Attachments: tc.oldAttachments}
			after := &discordgo.Message{Content: tc.after, Attachments: tc.newAttachments}
			event := &discordgo.Event{
				RawData: []byte(`{"content":"new","attachments":[]}`),
				Struct:  &discordgo.MessageUpdate{Message: after, BeforeUpdate: before},
			}
			if got := preserveEditedMedia(event, after); got != tc.want {
				t.Fatalf("preserve = %t", got)
			}
		})
	}
}
