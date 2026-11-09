package telegram

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	contribbolt "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/storage"
	gotd "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/sleroq/zefxi/internal/bridge"
	qrcode "github.com/yeqown/go-qrcode/v2"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/term"
)

// UserConfig configures the separate Telegram user session.
type UserConfig struct {
	AppID                         int
	AppHash, SessionDir, Password string
}

// RunUser receives updates using a persistent MTProto user session.
func (c *Client) RunUser(ctx context.Context, cfg UserConfig, h bridge.Handler) error {
	if cfg.AppID == 0 || cfg.AppHash == "" || cfg.SessionDir == "" {
		return errors.New("telegram user session requires app ID, app hash and session directory")
	}
	if err := os.MkdirAll(cfg.SessionDir, 0700); err != nil {
		return fmt.Errorf("create telegram session directory: %w", err)
	}
	if err := os.Chmod(cfg.SessionDir, 0700); err != nil {
		return fmt.Errorf("secure telegram session directory: %w", err)
	}
	sessionPath := filepath.Join(cfg.SessionDir, "session.json")
	if err := os.Chmod(sessionPath, 0600); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("secure telegram session file: %w", err)
	}
	db, err := bolt.Open(filepath.Join(cfg.SessionDir, "updates.db"), 0600, nil)
	if err != nil {
		return fmt.Errorf("open telegram update storage: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Warn("close telegram update storage", "error", err)
		}
	}()
	if err := os.Chmod(filepath.Join(cfg.SessionDir, "updates.db"), 0600); err != nil {
		return fmt.Errorf("secure telegram update storage: %w", err)
	}

	dispatcher := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(dispatcher)
	peers := contribbolt.NewPeerStorage(db, []byte("peers"))
	hashes := accessHashes{db: db}
	manager := updates.New(updates.Config{
		Handler: storage.UpdateHook(dispatcher, peers),
		Storage: contribbolt.NewStateStorage(db), AccessHasher: hashes, UserAccessHasher: hashes,
		OnTooLong:             func() { slog.Warn("Telegram update gap is too long to recover") },
		OnChannelTooLong:      func(id int64) { slog.Warn("Telegram channel update gap is too long to recover", "channel_id", id) },
		OnChannelInaccessible: func(id int64) { slog.Warn("Telegram channel is inaccessible", "channel_id", id) },
		OnLoadChannelStateFailed: func(id int64) {
			slog.Warn("Telegram channel state cannot be restored: access hash missing", "channel_id", id)
		},
	})
	options := gotd.Options{
		SessionStorage: &gotd.FileSessionStorage{Path: sessionPath}, UpdateHandler: manager,
	}
	client := gotd.NewClient(cfg.AppID, cfg.AppHash, options)
	c.registerUserUpdates(dispatcher, client, peers, h)
	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("check telegram user authorization: %w", err)
		}
		if !status.Authorized {
			if err := authenticateUser(ctx, client, loggedIn, cfg.Password); err != nil {
				return err
			}
		}
		self, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("get telegram user: %w", err)
		}
		started := updates.AuthOptions{OnStart: func(context.Context) {
			c.botCatchupUntil.Store(time.Now().Unix())
		}}
		if err := manager.Run(ctx, client.API(), self.ID, started); err != nil {
			return fmt.Errorf("run telegram update manager: %w", err)
		}
		return nil
	})
}

func authenticateUser(ctx context.Context, client *gotd.Client, loggedIn qrlogin.LoggedIn, password string) error {
	_, err := client.QR().Auth(ctx, loggedIn, showLoginQR)
	if err == nil {
		return nil
	}
	if !tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		return fmt.Errorf("telegram QR login: %w", err)
	}
	if password == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("telegram two-step verification requires a password or a terminal")
		}
		fmt.Fprint(os.Stderr, "Telegram two-step verification password: ")
		input, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("read telegram password: %w", err)
		}
		password = string(input)
	}
	if _, err := client.Auth().Password(ctx, password); err != nil {
		return fmt.Errorf("telegram two-step verification: %w", err)
	}
	return nil
}

func showLoginQR(_ context.Context, token qrlogin.Token) error {
	code, err := qrcode.New(token.URL())
	if err != nil {
		return fmt.Errorf("generate telegram login QR: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Scan this QR code in Telegram to sign in:")
	return code.Save(terminalQR{})
}

type terminalQR struct{}

func (terminalQR) Close() error { return nil }
func (terminalQR) Write(m qrcode.Matrix) error {
	var out strings.Builder
	out.WriteString("\x1b[47;30m")
	border := strings.Repeat("  ", m.Width()+8)
	for range 4 {
		out.WriteString(border)
		out.WriteByte('\n')
	}
	m.Iterate(qrcode.IterDirection_ROW, func(x, y int, value qrcode.QRValue) {
		if x == 0 {
			out.WriteString("        ")
		}
		if value.IsSet() {
			out.WriteString("██")
		} else {
			out.WriteString("  ")
		}
		if x == m.Width()-1 {
			out.WriteString("        \n")
		}
	})
	for range 4 {
		out.WriteString(border)
		out.WriteByte('\n')
	}
	out.WriteString("\x1b[0m")
	_, err := fmt.Fprint(os.Stderr, out.String())
	return err
}

type accessHashes struct{ db *bolt.DB }

func (s accessHashes) SetChannelAccessHash(_ context.Context, userID, channelID, hash int64) error {
	return s.setHash("channel-access-hashes", userID, channelID, hash)
}
func (s accessHashes) GetChannelAccessHash(_ context.Context, userID, channelID int64) (int64, bool, error) {
	return s.getHash("channel-access-hashes", userID, channelID)
}
func (s accessHashes) SetUserAccessHash(_ context.Context, userID, targetID, hash int64) error {
	return s.setHash("user-access-hashes", userID, targetID, hash)
}
func (s accessHashes) GetUserAccessHash(_ context.Context, userID, targetID int64) (int64, bool, error) {
	return s.getHash("user-access-hashes", userID, targetID)
}
func (s accessHashes) setHash(bucket string, userID, targetID, hash int64) error {
	var key [16]byte
	binary.BigEndian.PutUint64(key[:8], uint64(userID))
	binary.BigEndian.PutUint64(key[8:], uint64(targetID))
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(hash))
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return b.Put(key[:], value[:])
	})
}
func (s accessHashes) getHash(bucket string, userID, targetID int64) (int64, bool, error) {
	var key [16]byte
	binary.BigEndian.PutUint64(key[:8], uint64(userID))
	binary.BigEndian.PutUint64(key[8:], uint64(targetID))
	var hash int64
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		value := b.Get(key[:])
		if value == nil {
			return nil
		}
		if len(value) != 8 {
			return errors.New("invalid telegram access hash in storage")
		}
		hash, found = int64(binary.BigEndian.Uint64(value)), true
		return nil
	})
	return hash, found, err
}
