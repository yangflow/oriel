package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

const (
	// WebSubWait is how long after a subscription request the hub has to verify it; a request that no verification
	// follows is sent again after it. WebSubVerifyWindow is how long a verification is accepted after the request.
	WebSubWait         = 10 * time.Minute
	WebSubVerifyWindow = time.Hour
)

// WebSub is the push subscription of one source, as the subscribing loop and the callback route see it.
type WebSub struct {
	SourceID    int64
	Paused      bool
	Hub         string
	Topic       string
	Key         string
	Secret      string
	State       string // "", "pending" or "active"
	ExpiresAt   time.Time
	RequestedAt time.Time
	Base        string
	NextAt      time.Time // when the next request or renewal is due
	Failures    int
	Error       string // why the last request failed
}

const webSubColumns = `id, paused, websub_hub, websub_topic, websub_key, websub_secret, websub_state, websub_expires_at,
	websub_requested_at, websub_base, websub_next_at, websub_failures, websub_error`

func scanWebSub(row interface{ Scan(...any) error }) (WebSub, error) {
	var w WebSub
	var expires, requested sql.NullInt64
	var next int64
	if err := row.Scan(&w.SourceID, &w.Paused, &w.Hub, &w.Topic, &w.Key, &w.Secret, &w.State, &expires, &requested,
		&w.Base, &next, &w.Failures, &w.Error); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WebSub{}, ErrNotFound
		}
		return WebSub{}, err
	}
	if expires.Valid {
		w.ExpiresAt = time.UnixMilli(expires.Int64)
	}
	if requested.Valid {
		w.RequestedAt = time.UnixMilli(requested.Int64)
	}
	w.NextAt = time.UnixMilli(next)
	return w, nil
}

// SetWebSubFeed records the hub and topic a feed declares, both empty when it declares none. Nothing changes when
// they are what is stored; otherwise any subscription to the old pair is dropped, and a new hub is subscribed to
// on the next round of the subscribing loop. The key and secret are kept.
func (s *Store) SetWebSubFeed(ctx context.Context, id int64, hub, topic string) error {
	if hub == "" {
		topic = ""
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET websub_hub = ?1, websub_topic = ?2, websub_state = '', websub_expires_at = NULL,
			websub_requested_at = NULL, websub_next_at = 0, websub_failures = 0, websub_error = ''
		WHERE id = ?3 AND (websub_hub <> ?1 OR websub_topic <> ?2)`, hub, topic, id)
	return err
}

// WebSubDue lists the unpaused sources with a hub whose next subscription or renewal has come, or whose
// subscription was made for another public address than base, the longest waiting first.
func (s *Store) WebSubDue(ctx context.Context, now time.Time, base string, limit int) ([]WebSub, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+webSubColumns+` FROM sources
		WHERE paused = 0 AND websub_hub <> '' AND (websub_next_at <= ?1 OR (websub_state <> '' AND websub_base <> ?2))
		ORDER BY websub_next_at, id LIMIT ?3`, now.UnixMilli(), base, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []WebSub
	for rows.Next() {
		w, err := scanWebSub(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, w)
	}
	return list, rows.Err()
}

// WebSubByKey returns the subscription whose callback path carries key. An empty or unknown key is ErrNotFound.
func (s *Store) WebSubByKey(ctx context.Context, key string) (WebSub, error) {
	if key == "" {
		return WebSub{}, ErrNotFound
	}
	return scanWebSub(s.db.QueryRowContext(ctx, "SELECT "+webSubColumns+" FROM sources WHERE websub_key = ?", key))
}

// WebSubRequest records that a subscription request to the hub of the source goes out now, with a callback under the
// public address base, and returns the subscription with its key and secret, generated at the first request. An
// active subscription stays active while it is renewed. If the hub does not verify within WebSubWait the loop asks
// again.
func (s *Store) WebSubRequest(ctx context.Context, id int64, base string, now time.Time) (WebSub, error) {
	key, secret := randomHex(16), randomHex(32)
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET websub_key = CASE WHEN websub_key = '' THEN ?1 ELSE websub_key END,
			websub_secret = CASE WHEN websub_secret = '' THEN ?2 ELSE websub_secret END,
			websub_state = CASE WHEN websub_state = 'active' THEN 'active' ELSE 'pending' END,
			websub_requested_at = ?3, websub_base = ?4, websub_next_at = ?5
		WHERE id = ?6`, key, secret, now.UnixMilli(), base, now.Add(WebSubWait).UnixMilli(), id)
	if err != nil {
		return WebSub{}, err
	}
	return scanWebSub(s.db.QueryRowContext(ctx, "SELECT "+webSubColumns+" FROM sources WHERE id = ?", id))
}

// WebSubVerified records that the hub verified the subscription: it is active until expires, and renewed from renew.
func (s *Store) WebSubVerified(ctx context.Context, id int64, expires, renew time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET websub_state = 'active', websub_expires_at = ?, websub_next_at = ?,
			websub_requested_at = NULL, websub_failures = 0, websub_error = ''
		WHERE id = ?`, expires.UnixMilli(), renew.UnixMilli(), id)
	return err
}

// WebSubFailed records a subscription request that did not work, the hub's denial or a verification that never
// came, and when to try again: ten minutes after the first failure, doubling up to a day. A subscription that was
// not active yet is dropped; an active one keeps working until its lease runs out.
func (s *Store) WebSubFailed(ctx context.Context, id int64, reason string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET websub_state = CASE WHEN websub_state = 'active' THEN 'active' ELSE '' END,
			websub_next_at = ?1 + min(?2 * (1 << min(websub_failures, 20)), ?3),
			websub_failures = websub_failures + 1, websub_error = ?4, websub_requested_at = NULL
		WHERE id = ?5`, now.UnixMilli(), WebSubWait.Milliseconds(), (24 * time.Hour).Milliseconds(), reason, id)
	return err
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
