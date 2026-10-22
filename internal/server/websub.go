package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	stdhash "hash"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/store"
)

const (
	maxPush       = 8 << 20 // the largest push body read
	maxChallenge  = 1000
	defaultLease  = 24 * time.Hour
	minLease      = 60 * time.Second
	maxLease      = 90 * 24 * time.Hour
	renewAtFactor = 0.8 // the share of a lease after which it is renewed
)

// websub is the callback route a WebSub hub calls. It takes no session: the unguessable key, the checks below and
// the signature of a push protect it. Everything that is not a valid call is 404.
func (s *Server) websub(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	base, err := fetch.WebSubBase(ctx, s.store)
	if err != nil {
		internal(w, err)
		return
	}
	if base == "" {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	sub, err := s.store.WebSubByKey(ctx, r.PathValue("key"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && (sub.Paused || sub.Hub == "")) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	now := s.now()
	if r.Method == http.MethodGet {
		s.websubVerify(w, r, sub, now)
		return
	}
	if sub.State != "active" || !now.Before(sub.ExpiresAt) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPush))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, "too_large")
			return
		}
		fail(w, http.StatusBadRequest, "invalid_body")
		return
	}
	// A bad signature still gets a success answer, as the specification asks, but nothing happens.
	if validSignature(r.Header.Get("X-Hub-Signature"), sub.Secret, body) {
		if err := s.store.MarkSourceDue(ctx, sub.SourceID, now); err != nil && !errors.Is(err, store.ErrPaused) {
			internal(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// websubVerify answers the hub's intent verification, and its denial of a subscription.
func (s *Server) websubVerify(w http.ResponseWriter, r *http.Request, sub store.WebSub, now time.Time) {
	query := r.URL.Query()
	mode := query.Get("hub.mode")
	asked := !sub.RequestedAt.IsZero() && now.Sub(sub.RequestedAt) <= store.WebSubVerifyWindow
	if (mode != "subscribe" && mode != "denied") || sub.Topic == "" || query.Get("hub.topic") != sub.Topic || !asked {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	ctx := r.Context()
	if mode == "denied" {
		reason := query.Get("hub.reason")
		if runes := []rune(reason); len(runes) > 200 {
			reason = string(runes[:200])
		}
		if err := s.store.WebSubFailed(ctx, sub.SourceID, reason, now); err != nil {
			internal(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	challenge := query.Get("hub.challenge")
	if challenge == "" || len(challenge) > maxChallenge {
		fail(w, http.StatusBadRequest, "invalid_challenge")
		return
	}
	lease := defaultLease
	if n, err := strconv.ParseInt(query.Get("hub.lease_seconds"), 10, 64); err == nil {
		lease = time.Duration(min(max(n, 0), int64(maxLease/time.Second))) * time.Second
	}
	lease = min(max(lease, minLease), maxLease)
	renew := now.Add(time.Duration(float64(lease) * renewAtFactor))
	if err := s.store.WebSubVerified(ctx, sub.SourceID, now.Add(lease), renew); err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, challenge)
}

// validSignature checks an X-Hub-Signature header, "<algorithm>=<hex>", against the HMAC of body.
func validSignature(header, secret string, body []byte) bool {
	algorithm, digest, ok := strings.Cut(strings.TrimSpace(header), "=")
	if !ok || secret == "" {
		return false
	}
	var newHash func() stdhash.Hash
	switch strings.ToLower(algorithm) {
	case "sha1":
		newHash = sha1.New
	case "sha256":
		newHash = sha256.New
	case "sha384":
		newHash = sha512.New384
	case "sha512":
		newHash = sha512.New
	default:
		return false
	}
	got, err := hex.DecodeString(digest)
	if err != nil {
		return false
	}
	mac := hmac.New(newHash, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
