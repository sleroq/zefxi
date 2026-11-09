package telegram

import (
	"context"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestAccessHashesPersistAndRemainScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}

	hashes := accessHashes{db: db}
	ctx := context.Background()
	if err := hashes.SetChannelAccessHash(ctx, 1, 7, -42); err != nil {
		t.Fatal(err)
	}
	if err := hashes.SetUserAccessHash(ctx, 1, 7, 0); err != nil {
		t.Fatal(err)
	}
	if err := hashes.SetChannelAccessHash(ctx, 2, 7, 123); err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()

	hashes = accessHashes{db: db}
	for _, tt := range []struct {
		name               string
		get                func(context.Context, int64, int64) (int64, bool, error)
		user, target, want int64
		found              bool
	}{
		{"channel", hashes.GetChannelAccessHash, 1, 7, -42, true},
		{"other account", hashes.GetChannelAccessHash, 2, 7, 123, true},
		{"user zero hash", hashes.GetUserAccessHash, 1, 7, 0, true},
		{"different kind", hashes.GetUserAccessHash, 2, 7, 0, false},
		{"missing", hashes.GetChannelAccessHash, 1, 8, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, found, err := tt.get(ctx, tt.user, tt.target)
			if err != nil || got != tt.want || found != tt.found {
				t.Fatalf("got (%d, %v, %v), want (%d, %v, nil)", got, found, err, tt.want, tt.found)
			}
		})
	}
}
