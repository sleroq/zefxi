package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sleroq/zefxi/internal/animation"
	"github.com/sleroq/zefxi/internal/bridge"
)

var gifLinks = regexp.MustCompile(`https?://[^\s<>]+`)

func publicGIFURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "cdn.discordapp.com", "media.discordapp.net", "images-ext-1.discordapp.net", "images-ext-2.discordapp.net",
		"gif.fxtwitter.com", "media.tenor.com", "c.tenor.com", "media.giphy.com", "i.giphy.com",
		"media0.giphy.com", "media1.giphy.com", "media2.giphy.com", "media3.giphy.com", "media4.giphy.com":
		return true
	}
	return false
}

func pickerLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "tenor.com", "www.tenor.com":
		return strings.HasPrefix(u.Path, "/view/")
	case "giphy.com", "www.giphy.com":
		return strings.HasPrefix(u.Path, "/gifs/")
	}
	return false
}

func animationURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	ext := path.Ext(u.Path)
	return strings.EqualFold(ext, ".gif") || strings.EqualFold(ext, ".webp")
}

func discordAttachmentURL(u *url.URL) bool {
	return strings.EqualFold(u.Hostname(), "cdn.discordapp.com") && strings.HasPrefix(u.Path, "/attachments/")
}

func gifSourceKey(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && discordAttachmentURL(u) {
		u.RawQuery = ""
		u.ForceQuery = false
		return u.String()
	}
	return raw
}

func isGIFEmbed(e *discordgo.MessageEmbed) bool {
	return e.Type == "gifv" || (e.Type == "image" &&
		((e.Image != nil && (animationURL(e.URL) || animationURL(e.Image.URL))) ||
			(e.Thumbnail != nil && (animationURL(e.URL) || animationURL(e.Thumbnail.URL)))))
}

func hasGIFEmbed(embeds []*discordgo.MessageEmbed) bool {
	return slices.ContainsFunc(embeds, isGIFEmbed)
}

func gifEmbedSources(embeds []*discordgo.MessageEmbed) map[string]string {
	sources := make(map[string]string)
	for _, e := range embeds {
		if !isGIFEmbed(e) {
			continue
		}
		media := ""
		if e.Video != nil {
			media = e.Video.URL
		}
		if media == "" && e.Image != nil {
			media = e.Image.ProxyURL
			if media == "" {
				media = e.Image.URL
			}
		}
		if media == "" && e.Thumbnail != nil {
			media = e.Thumbnail.URL
			if media == "" {
				media = e.Thumbnail.ProxyURL
			}
		}
		if media != "" {
			sources[gifSourceKey(e.URL)] = media
			if e.Type == "image" && e.Image != nil {
				sources[gifSourceKey(e.Image.URL)] = media
			}
			if e.Type == "image" && e.Thumbnail != nil {
				sources[gifSourceKey(e.Thumbnail.URL)] = media
			}
		}
	}
	return sources
}

func (c *Client) downloadGIFs(ctx context.Context, m *discordgo.Message) (string, []bridge.Attachment) {
	content := m.Content
	sources := gifEmbedSources(m.Embeds)

	var attachments []bridge.Attachment
	for _, raw := range gifLinks.FindAllString(m.Content, -1) {
		link := strings.TrimRight(raw, ".,!?;:)")
		media := sources[gifSourceKey(link)]
		if media == "" && publicGIFURL(link) {
			u, _ := url.Parse(link)
			if strings.EqualFold(path.Ext(u.Path), ".gif") || (strings.EqualFold(u.Hostname(), "gif.fxtwitter.com") && strings.EqualFold(path.Ext(u.Path), ".webp")) {
				media = link
			}
		}
		if media == "" {
			continue
		}
		// Embed media is supplied by Discord, but still restrict network access to public media CDNs.
		if !publicGIFURL(media) {
			continue
		}
		u, _ := url.Parse(media)
		ext, contentType := ".mp4", "video/mp4"
		if strings.EqualFold(path.Ext(u.Path), ".gif") {
			ext, contentType = ".gif", "image/gif"
		}
		source := &discordgo.MessageAttachment{
			URL: media, Filename: "animation" + ext, ContentType: contentType,
		}
		client := *c.http
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if !publicGIFURL(req.URL.String()) || len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			if c.http.CheckRedirect != nil {
				return c.http.CheckRedirect(req, via)
			}
			return nil
		}
		downloadClient := *c
		downloadClient.http = &client
		a, err := downloadClient.download(ctx, source)
		if err != nil || len(a.Data) == 0 {
			continue
		}
		switch {
		case bytes.HasPrefix(a.Data, []byte("GIF87a")), bytes.HasPrefix(a.Data, []byte("GIF89a")):
			a.Name, a.ContentType = "animation.gif", "image/gif"
		case len(a.Data) >= 8 && string(a.Data[4:8]) == "ftyp":
			a.Name, a.ContentType = "animation.mp4", "video/mp4"
		case bytes.HasPrefix(a.Data, []byte{0x1a, 0x45, 0xdf, 0xa3}),
			len(a.Data) >= 21 && string(a.Data[:4]) == "RIFF" && string(a.Data[8:16]) == "WEBPVP8X" && a.Data[20]&2 != 0:
			converted, err := animation.FromVideo(ctx, a.Data)
			if err != nil {
				slog.Warn("Discord GIF conversion failed", "error", err)
				continue
			}
			a.Data, a.Size = converted, int64(len(converted))
			a.Name, a.ContentType = "animation.gif", "image/gif"
		default:
			continue
		}
		a.Animated = true
		attachments = append(attachments, a)
		content = strings.Replace(content, link, "", 1)
	}
	if len(attachments) > 0 {
		content = strings.TrimSpace(content)
	}
	return content, attachments
}

type gifLookup struct {
	updates  chan []*discordgo.MessageEmbed
	deadline time.Time
}

type gifLookups struct {
	mu      sync.Mutex
	pending map[string]*gifLookup
	timeout time.Duration
}

func newGIFLookups(timeout time.Duration) *gifLookups {
	return &gifLookups{pending: make(map[string]*gifLookup), timeout: timeout}
}

func (p *gifLookups) register(e *discordgo.MessageCreate) *gifLookup {
	if hasGIFEmbed(e.Embeds) {
		return nil
	}
	if !slices.ContainsFunc(gifLinks.FindAllString(e.Content, -1), func(raw string) bool {
		link := strings.TrimRight(raw, ".,!?;:)")
		if pickerLink(link) {
			return true
		}
		if !animationURL(link) {
			return false
		}
		u, _ := url.Parse(link)
		return !publicGIFURL(link) || discordAttachmentURL(u)
	}) {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pending) >= 256 {
		return nil
	}
	lookup := &gifLookup{updates: make(chan []*discordgo.MessageEmbed, 1), deadline: time.Now().Add(p.timeout)}
	p.pending[e.ID] = lookup
	return lookup
}

func (p *gifLookups) deliver(e *discordgo.MessageUpdate) {
	if !hasGIFEmbed(e.Embeds) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if lookup := p.pending[e.ID]; lookup != nil {
		select {
		case lookup.updates <- e.Embeds:
		default:
		}
	}
}

func (p *gifLookups) await(ctx context.Context, e *discordgo.MessageCreate, lookup *gifLookup) {
	if lookup == nil {
		return
	}
	defer func() { p.mu.Lock(); delete(p.pending, e.ID); p.mu.Unlock() }()
	timer := time.NewTimer(time.Until(lookup.deadline))
	defer timer.Stop()
	select {
	case embeds := <-lookup.updates:
		e.Embeds = embeds
	case <-timer.C:
	case <-ctx.Done():
	}
}

// discordgo does not expose video.proxy_url, so retain it at the gateway boundary.
func decodeGIFMessage(data json.RawMessage) (*discordgo.Message, error) {
	var payload struct {
		Embeds []struct {
			Video *struct {
				ProxyURL string `json:"proxy_url"`
			} `json:"video"`
		} `json:"embeds"`
	}
	// Decode the standard message separately because the proxy-only embeds shadow its field.
	var message discordgo.Message
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	for i, e := range payload.Embeds {
		if e.Video != nil && e.Video.ProxyURL != "" && message.Embeds[i].Video != nil {
			message.Embeds[i].Video.URL = e.Video.ProxyURL
		}
	}
	return &message, nil
}
