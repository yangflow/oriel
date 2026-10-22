package importer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The codes an import fails with. They are all an Error carries: never the text of a transport error, which can
// hold the address or a header.
const (
	ErrInvalidBase  = "invalid_base_url"
	ErrUnauthorized = "unauthorized"
	ErrRateLimited  = "rate_limited"
	ErrUnreachable  = "unreachable"
	ErrBadResponse  = "bad_response"
	ErrTooLarge     = "too_large"
	ErrRedirected   = "redirected"
)

// Error is a failed import, by code only.
type Error struct{ Code string }

func (e *Error) Error() string { return "import: " + e.Code }

// Code returns the code of an import error, or "" for any other.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

const (
	timeout      = 30 * time.Second
	maxBody      = 16 << 20
	spacing      = 200 * time.Millisecond
	maxRetries   = 3
	maxRetryWait = 60 * time.Second
	defaultWait  = 5 * time.Second
	maxRedirects = 3
)

var errRedirect = errors.New("redirect refused")

// Client makes the requests of an import. It is a plain HTTP client, not the guarded one the fetcher uses for entry
// links, because the address is the one the user typed for their own service and may be on their network. It stays
// safe by what it asks: only the calls of the importers, a timeout, a size limit, spacing between requests, the
// service's own rate limit, and no redirect to another host, so the credential is sent nowhere else.
// A Client serves one import at a time.
type Client struct {
	HTTP    *http.Client
	Spacing time.Duration
	// Sleep waits for d or until ctx ends; tests replace it.
	Sleep   func(ctx context.Context, d time.Duration) error
	MaxBody int64
	used    bool
}

// NewClient returns a Client with the limits of an import.
func NewClient() *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: timeout, CheckRedirect: checkRedirect},
		Spacing: spacing,
		Sleep:   sleep,
		MaxBody: maxBody,
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// checkRedirect follows a redirect only on the same host, never from https to http, and at most maxRedirects times.
func checkRedirect(req *http.Request, via []*http.Request) error {
	first := via[0]
	if len(via) > maxRedirects || req.URL.Host != first.URL.Host || (first.URL.Scheme == "https" && req.URL.Scheme != "https") {
		return errRedirect
	}
	return nil
}

// Do sends one request and returns the body of a successful answer. header is sent as it is. A 429 is waited out as
// the Retry-After header asks, capped, and tried again a few times.
func (c *Client) Do(ctx context.Context, method, rawURL string, header map[string]string, form url.Values) ([]byte, error) {
	if c.used && c.Spacing > 0 {
		if err := c.Sleep(ctx, c.Spacing); err != nil {
			return nil, err
		}
	}
	c.used = true
	for attempt := 0; ; attempt++ {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
		if err != nil {
			return nil, &Error{Code: ErrInvalidBase}
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.Header.Set("Accept", "application/json, text/plain")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, errRedirect) {
				return nil, &Error{Code: ErrRedirected}
			}
			return nil, &Error{Code: ErrUnreachable}
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBody+1))
		wait := retryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			if attempt >= maxRetries {
				return nil, &Error{Code: ErrRateLimited}
			}
			if err := c.Sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, &Error{Code: ErrUnauthorized}
		case resp.StatusCode < 200 || resp.StatusCode > 299:
			return nil, &Error{Code: ErrBadResponse}
		case err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &Error{Code: ErrUnreachable}
		case int64(len(data)) > c.MaxBody:
			return nil, &Error{Code: ErrTooLarge}
		}
		return data, nil
	}
}

// retryAfter reads a Retry-After header, in seconds or as a date, into a wait between a second and maxRetryWait.
func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	wait := defaultWait
	if secs, err := strconv.Atoi(v); err == nil {
		wait = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		wait = time.Until(t)
	}
	return min(max(wait, time.Second), maxRetryWait)
}
