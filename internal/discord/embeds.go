package discord

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// appendRichEmbeds renders explicit embeds without fetching their links or duplicating link previews.
func appendRichEmbeds(content string, embeds []*discordgo.MessageEmbed) string {
	parts := []string{}
	seenURLs := make(map[string]bool)

	addText := func(text string) {
		if text != "" {
			parts = append(parts, text)
		}
	}
	addURL := func(raw string) {
		link := shareableURL(raw)
		if link == "" || seenURLs[link] || strings.Contains(content, link) {
			return
		}
		seenURLs[link] = true
		parts = append(parts, link)
	}

	for _, embed := range embeds {
		if embed.Type != "rich" {
			continue
		}
		if embed.Author != nil {
			addText(embed.Author.Name)
			addURL(embed.Author.URL)
			addURL(embed.Author.IconURL)
		}
		addText(embed.Title)
		addURL(embed.URL)
		addText(embed.Description)
		for _, field := range embed.Fields {
			addText(field.Name)
			addText(field.Value)
		}
		if embed.Image != nil {
			addURL(embed.Image.URL)
		}
		if embed.Thumbnail != nil {
			addURL(embed.Thumbnail.URL)
		}
		if embed.Footer != nil {
			addText(embed.Footer.Text)
			addURL(embed.Footer.IconURL)
		}
	}

	if len(parts) == 0 {
		return content
	}
	if content != "" {
		content += "\n"
	}
	return content + strings.Join(parts, "\n")
}
