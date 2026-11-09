package bridge

import (
	"context"

	"github.com/sleroq/zefxi/internal/richtext"
)

type Platform string

const (
	Telegram Platform = "telegram"
	Discord  Platform = "discord"
)

type User struct {
	ID        string
	Username  string
	Name      string
	AvatarURL string
}

type LinkCandidate struct {
	User
	MatchPercent int
}

type StickerFormat string

const (
	StickerPNG    StickerFormat = "png"
	StickerAPNG   StickerFormat = "apng"
	StickerGIF    StickerFormat = "gif"
	StickerLottie StickerFormat = "lottie"
)

type Voice struct {
	Duration int
	Waveform []byte
}

type Attachment struct {
	Voice         *Voice
	Name          string
	StickerFormat StickerFormat
	ContentType   string
	Size          int64
	Data          []byte
	// Animated marks a GIF-like animation even when its bytes are MP4.
	Animated bool
	// URL is a shareable source link, never a Telegram bot-token URL.
	URL string
}

type ForwardSource struct {
	User User
	Name string
	URL  string
}

type Message struct {
	// ID is empty for events without a source message; these are not mapped.
	ID                  string
	Author              User
	Forward             *ForwardSource
	Text                richtext.Text
	ReplyTo             string
	Attachments         []Attachment
	PreserveAttachments bool
}

type OutboundMessage struct {
	Author User
	// Text is Discord markdown or Telegram HTML, according to the destination.
	Text                string
	ReplyTo             string
	Attachments         []Attachment
	MentionIDs          []string
	TelegramUserID      string
	PreserveAttachments bool
}

type Reaction struct {
	MessageID string
	UserID    string
	Emoji     string
	Added     bool
}

type ReactionCount struct {
	Emoji string
	Count int
}

type ReactionUsers struct {
	Emoji string
	Users []User
}

type ReactionSummary struct {
	LinkFound bool
	Reactions []ReactionUsers
}

type MessageLink struct {
	TelegramID string
	DiscordID  string
	Origin     Platform
	AuthorID   string
}

type LinkView struct {
	Platform   Platform
	Author     User
	Linked     []User
	Candidates []LinkCandidate
}

type Identity struct {
	Telegram User
	Discord  User
}

// Handler is implemented by the bridge and called by the platform adapters.
type Handler interface {
	LinkView(context.Context, string, User, string) (LinkView, error)
	UnlinkPair(context.Context, string, string) error
	HandleMessage(context.Context, Platform, Message) error
	HandleEdit(context.Context, Platform, Message) error
	HandleDiscordDelete(context.Context, string) error
	HandleTelegramDelete(context.Context, string) error
	HandleReaction(context.Context, Platform, Reaction) error
	HandleTelegramPin(context.Context, string, bool) error
	RefreshDiscordPins(context.Context) error
	LinkCandidates(context.Context, User, string) ([]LinkCandidate, error)
	UnlinkUser(context.Context, string) error
	LinkUsers(context.Context, string, string) error
	ReactionSummary(context.Context, string) (ReactionSummary, error)
	Whois(context.Context, Platform, string, User) (Identity, error)
}

// Messenger sends a message to one configured chat and manages the bot's reactions.
type Messenger interface {
	Send(context.Context, OutboundMessage) ([]string, error)
	Edit(context.Context, string, OutboundMessage) error
	SetReaction(context.Context, string, string, bool) error
	SetPin(context.Context, string, bool) error
}

type TelegramClient interface {
	Messenger
	Delete(context.Context, string) error
}

type DiscordClient interface {
	Messenger
	Delete(context.Context, string) error
	EditParts(context.Context, []string, OutboundMessage) ([]string, error)
	FindUsers(context.Context, string) ([]User, error)
	GuildMembers(context.Context) ([]User, error)
	ReactionUsers(context.Context, string) ([]ReactionUsers, error)
	PinnedMessages(context.Context) ([]string, error)
}

type Store interface {
	Users(context.Context, Platform) ([]User, error)
	LinkedUsers(context.Context, Platform, string) ([]User, error)
	UnlinkPair(context.Context, string, string) error
	RememberUser(context.Context, Platform, User) error
	User(context.Context, Platform, string) (User, bool, error)
	LinkUsers(context.Context, string, string) error
	UnlinkUser(context.Context, string) error
	AutoLinkDisabled(context.Context, string) (bool, error)
	LinkedUser(context.Context, Platform, string) (User, bool, error)
	LinkMessage(context.Context, MessageLink) error
	DeleteMessageLink(context.Context, MessageLink) error
	ReplaceDiscordLinks(context.Context, MessageLink, []string) error
	MessageLink(context.Context, Platform, string) (MessageLink, bool, error)
	MessageLinks(context.Context, Platform, string) ([]MessageLink, error)
	ChangeReaction(context.Context, Platform, Reaction) ([]ReactionCount, error)
	BridgedReactionCounts(context.Context, Platform, string) ([]ReactionCount, error)
}
