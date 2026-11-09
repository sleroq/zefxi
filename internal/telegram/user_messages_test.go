package telegram

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/dialogs"

	"github.com/gotd/td/tg"
)

func TestUserMediaConversion(t *testing.T) {
	cases := []struct {
		name     string
		media    tg.MessageMediaClass
		filename string
		animated bool
	}{
		{"photo", &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 5, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "s", W: 10, H: 10, Size: 100}, &tg.PhotoSize{Type: "x", W: 100, H: 100, Size: 1000}}}}, "photo.jpg", false},
		{"voice", &tg.MessageMediaDocument{Voice: true, Document: &tg.Document{MimeType: "audio/ogg"}}, "voice.ogg", false},
		{"video note", &tg.MessageMediaDocument{Round: true, Document: &tg.Document{MimeType: "video/mp4"}}, "video-note.mp4", false},
		{"sticker", &tg.MessageMediaDocument{Document: &tg.Document{MimeType: "application/x-tgsticker", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{}}}}, "sticker.tgs", true},
		{"animation", &tg.MessageMediaDocument{Document: &tg.Document{MimeType: "video/mp4", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAnimated{}}}}, "animation.mp4", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, loc := userFile(tc.media)
			if loc == nil || a.Name != tc.filename || a.Animated != tc.animated {
				t.Fatalf("attachment %+v location %v", a, loc)
			}
		})
	}
}

type testMediaDownloader struct {
	downloader.Client
	location tg.InputFileLocationClass
}

func (d *testMediaDownloader) Download(location tg.InputFileLocationClass) *downloader.Builder {
	d.location = location
	return downloader.NewDownloader().Download(d, location)
}

func (d *testMediaDownloader) UploadGetFile(_ context.Context, request *tg.UploadGetFileRequest) (tg.UploadFileClass, error) {
	if request.Location != d.location {
		return nil, fmt.Errorf("unexpected file location")
	}
	return &tg.UploadFile{Bytes: []byte("sticker")}, nil
}

func TestUserFileUsesClientDownloader(t *testing.T) {
	c := &Client{maxDownloadBytes: 100}
	d := &testMediaDownloader{}
	file, loc := userFile(&tg.MessageMediaDocument{Document: &tg.Document{
		ID: 42, DCID: 2, Size: 7, MimeType: "image/webp", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{}},
	}})
	result, err := c.receiveUserFile(context.Background(), d, loc, file)
	if err != nil || string(result.Data) != "sticker" || result.Size != 7 || d.location != loc {
		t.Fatalf("result %+v location %v error %v", result, d.location, err)
	}
}

func TestLimitedMediaBuffer(t *testing.T) {
	b := &limitedMediaBuffer{limit: 5}
	if _, err := b.Write([]byte("123")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("456")); err == nil || !b.exceeded || b.Len() != 3 {
		t.Fatalf("buffer=%q exceeded=%t err=%v", b.String(), b.exceeded, err)
	}
}

func TestUserMentionEntity(t *testing.T) {
	entities, _ := userEntities([]tg.MessageEntityClass{&tg.MessageEntityMention{Offset: 3, Length: 4}})
	if len(entities) != 1 || entities[0].Kind != "mention" || entities[0].Offset != 3 || entities[0].Length != 4 {
		t.Fatalf("mention entities: %+v", entities)
	}
}

func TestUserCustomEmojiEntities(t *testing.T) {
	_, emojis := userEntities([]tg.MessageEntityClass{&tg.MessageEntityCustomEmoji{Offset: 1, Length: 2, DocumentID: 987}})
	if id := soleCustomEmoji(" 😀 ", emojis); id != "987" {
		t.Fatalf("emoji %q", id)
	}
}

type testPeerStorage struct {
	storage.PeerStorage
	user *tg.User
}

func (s testPeerStorage) Find(_ context.Context, key storage.PeerKey) (storage.Peer, error) {
	if key.Kind != dialogs.User || key.ID != s.user.ID {
		return storage.Peer{}, storage.ErrPeerNotFound
	}
	return storage.Peer{User: s.user}, nil
}
func TestUserAuthorFromPeerStorage(t *testing.T) {
	peers := testPeerStorage{user: &tg.User{ID: 42, FirstName: "Cached", Username: "cached"}}
	author := userAuthor(context.Background(), peers, tg.Entities{}, &tg.PeerUser{UserID: 42})
	if author.ID != "42" || author.Name != "Cached" || author.Username != "cached" {
		t.Fatalf("author %+v", author)
	}
}
