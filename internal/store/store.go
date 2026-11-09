package store

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sleroq/zefxi/internal/bridge"
)

type Store struct{ db *sql.DB }

var _ bridge.Store = (*Store)(nil)

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
 create table if not exists users (
 platform text not null, id text not null, username text not null,
 username_key text not null, name text not null, avatar_url text not null,
 primary key (platform, id));
 create index if not exists users_username on users(platform, username_key);
 create table if not exists user_links (
 telegram_id text not null, discord_id text not null, primary key (telegram_id, discord_id));
 create table if not exists unlinked_users (telegram_id text primary key not null);
 create table if not exists message_links (
 telegram_id text not null, discord_id text not null, origin text not null, author_id text not null,
 primary key (telegram_id, discord_id));
 create index if not exists message_links_discord on message_links(discord_id);
 create table if not exists reactions (
 platform text not null, message_id text not null, user_id text not null, emoji text not null,
 primary key (platform, message_id, user_id, emoji));`)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	// Older databases enforced one-to-one links. Rebuild only that schema, atomically.
	var pk int
	if err = db.QueryRow(`select pk from pragma_table_info('user_links') where name='discord_id'`).Scan(&pk); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if pk == 0 {
		_, err = db.Exec(`begin;
 alter table user_links rename to user_links_old;
 create table user_links (telegram_id text not null, discord_id text not null, primary key (telegram_id, discord_id));
 insert into user_links select telegram_id, discord_id from user_links_old order by rowid;
 drop table user_links_old;
 commit;`)
		if err != nil {
			_, _ = db.Exec(`rollback`)
			return nil, errors.Join(err, db.Close())
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) RememberUser(ctx context.Context, platform bridge.Platform, user bridge.User) error {
	_, err := s.db.ExecContext(ctx, `insert into users(platform,id,username,username_key,name,avatar_url)
 values(?,?,?,?,?,?) on conflict(platform,id) do update set
 username=excluded.username, username_key=excluded.username_key, name=excluded.name, avatar_url=excluded.avatar_url`,
		platform, user.ID, user.Username, strings.ToLower(user.Username), user.Name, user.AvatarURL)
	return err
}

func (s *Store) User(ctx context.Context, platform bridge.Platform, id string) (bridge.User, bool, error) {
	var user bridge.User
	err := s.db.QueryRowContext(ctx, `select id,username,name,avatar_url from users
 where platform=? and (id=? or (username_key=? and username_key<>''))
 order by (id=?) desc, id limit 1`, platform, id, strings.ToLower(id), id).
		Scan(&user.ID, &user.Username, &user.Name, &user.AvatarURL)
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.User{}, false, nil
	}
	return user, err == nil, err
}

func (s *Store) LinkUsers(ctx context.Context, telegramID, discordID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	query := `insert into user_links(telegram_id,discord_id) values(?,?) on conflict do nothing`
	if _, err = tx.ExecContext(ctx, query, telegramID, discordID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `delete from unlinked_users where telegram_id=?`, telegramID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UnlinkUser(ctx context.Context, telegramID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.ExecContext(ctx, `delete from user_links where telegram_id=?`, telegramID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `insert into unlinked_users(telegram_id) values(?) on conflict do nothing`, telegramID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UnlinkPair(ctx context.Context, telegramID, discordID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.ExecContext(ctx, `delete from user_links where telegram_id=? and discord_id=?`, telegramID, discordID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `insert into unlinked_users(telegram_id) values(?) on conflict do nothing`, telegramID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Users(ctx context.Context, platform bridge.Platform) (users []bridge.User, err error) {
	query := `select id,username,name,avatar_url from users where platform=? order by id`
	rows, err := s.db.QueryContext(ctx, query, platform)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var u bridge.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.AvatarURL); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) LinkedUsers(ctx context.Context, platform bridge.Platform, id string) (users []bridge.User, err error) {
	rows, err := s.db.QueryContext(ctx, `select u.id,u.username,u.name,u.avatar_url from user_links l join users u
 on (?='telegram' and l.telegram_id=? and u.platform='discord' and u.id=l.discord_id)
 or (?='discord' and l.discord_id=? and u.platform='telegram' and u.id=l.telegram_id) order by l.rowid`, platform, id, platform, id)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var u bridge.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.AvatarURL); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) AutoLinkDisabled(ctx context.Context, telegramID string) (bool, error) {
	var disabled bool
	err := s.db.QueryRowContext(ctx, `select exists(select 1 from unlinked_users where telegram_id=?)`, telegramID).
		Scan(&disabled)
	return disabled, err
}

func (s *Store) LinkedUser(ctx context.Context, platform bridge.Platform, id string) (bridge.User, bool, error) {
	var user bridge.User
	err := s.db.QueryRowContext(ctx, `select u.id,u.username,u.name,u.avatar_url from user_links l join users u
 on (?='telegram' and l.telegram_id=? and u.platform='discord' and u.id=l.discord_id)
 or (?='discord' and l.discord_id=? and u.platform='telegram' and u.id=l.telegram_id) order by l.rowid limit 1`, platform, id, platform, id).
		Scan(&user.ID, &user.Username, &user.Name, &user.AvatarURL)
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.User{}, false, nil
	}
	return user, err == nil, err
}

func (s *Store) LinkMessage(ctx context.Context, link bridge.MessageLink) error {
	_, err := s.db.ExecContext(ctx, `insert into message_links(telegram_id,discord_id,origin,author_id) values(?,?,?,?)
 on conflict(telegram_id,discord_id) do update set origin=excluded.origin,author_id=excluded.author_id`, link.TelegramID, link.DiscordID, link.Origin, link.AuthorID)
	return err
}

func (s *Store) DeleteMessageLink(ctx context.Context, link bridge.MessageLink) error {
	_, err := s.db.ExecContext(ctx, `delete from message_links where telegram_id=? and discord_id=?`,
		link.TelegramID, link.DiscordID)
	return err
}

func (s *Store) ReplaceDiscordLinks(ctx context.Context, link bridge.MessageLink, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `delete from message_links where telegram_id=?`, link.TelegramID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `insert into message_links(telegram_id,discord_id,origin,author_id) values(?,?,?,?)`,
			link.TelegramID, id, link.Origin, link.AuthorID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) MessageLinks(ctx context.Context, platform bridge.Platform, id string) (links []bridge.MessageLink, err error) {
	rows, err := s.db.QueryContext(ctx, `select telegram_id,discord_id,origin,author_id from message_links
 where (?='telegram' and telegram_id=?) or (?='discord' and discord_id=?) order by rowid`, platform, id, platform, id)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var link bridge.MessageLink
		if err := rows.Scan(&link.TelegramID, &link.DiscordID, &link.Origin, &link.AuthorID); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) MessageLink(ctx context.Context, platform bridge.Platform, id string) (bridge.MessageLink, bool, error) {
	var link bridge.MessageLink
	err := s.db.QueryRowContext(ctx, `select telegram_id,discord_id,origin,author_id from message_links
 where (?='telegram' and telegram_id=?) or (?='discord' and discord_id=?) order by rowid limit 1`, platform, id, platform, id).
		Scan(&link.TelegramID, &link.DiscordID, &link.Origin, &link.AuthorID)
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.MessageLink{}, false, nil
	}
	return link, err == nil, err
}

func (s *Store) ChangeReaction(ctx context.Context, platform bridge.Platform, reaction bridge.Reaction) ([]bridge.ReactionCount, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	query := `delete from reactions where platform=? and message_id=? and user_id=? and emoji=?`
	if reaction.Added {
		query = `insert into reactions(platform,message_id,user_id,emoji) values(?,?,?,?) on conflict do nothing`
	}
	if _, err = tx.ExecContext(ctx, query, platform, reaction.MessageID, reaction.UserID, reaction.Emoji); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, reactionCountsSQL, platform, reaction.MessageID)
	if err != nil {
		return nil, err
	}
	counts, err := readReactionCounts(rows)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return counts, nil
}

const reactionCountsSQL = `select emoji,count(*) from reactions where platform=? and message_id=? group by emoji order by emoji`

func (s *Store) ReactionCounts(ctx context.Context, platform bridge.Platform, messageID string) ([]bridge.ReactionCount, error) {
	rows, err := s.db.QueryContext(ctx, reactionCountsSQL, platform, messageID)
	if err != nil {
		return nil, err
	}
	return readReactionCounts(rows)
}

// BridgedReactionCounts excludes reactions already represented by a linked account
// on the destination message. Stored user IDs retain their source-message prefix.
func (s *Store) BridgedReactionCounts(ctx context.Context, platform bridge.Platform, messageID string) ([]bridge.ReactionCount, error) {
	rows, err := s.db.QueryContext(ctx, `select r.emoji, count(*) from reactions r
 where r.platform=? and r.message_id=? and not exists (
   select 1 from message_links m join user_links u join reactions other
   where replace(other.emoji, '️', '') = replace(r.emoji, '️', '') and (
     (r.platform='telegram' and m.discord_id=r.message_id
       and u.telegram_id=substr(r.user_id, instr(r.user_id, ':')+1)
       and other.platform='discord' and other.message_id=m.telegram_id
       and other.user_id=m.discord_id || ':' || u.discord_id)
     or
     (r.platform='discord' and m.telegram_id=r.message_id
       and m.discord_id=substr(r.user_id, 1, instr(r.user_id, ':')-1)
       and u.discord_id=substr(r.user_id, instr(r.user_id, ':')+1)
       and other.platform='telegram' and other.message_id=m.discord_id
       and other.user_id=m.telegram_id || ':' || u.telegram_id)
   )
 ) group by r.emoji order by r.emoji`, platform, messageID)
	if err != nil {
		return nil, err
	}
	return readReactionCounts(rows)
}

func readReactionCounts(rows *sql.Rows) (counts []bridge.ReactionCount, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var count bridge.ReactionCount
		if err := rows.Scan(&count.Emoji, &count.Count); err != nil {
			return nil, err
		}
		counts = append(counts, count)
	}
	return counts, rows.Err()
}

func rollback(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		slog.Error("SQLite rollback failed", "error", err)
	}
}
