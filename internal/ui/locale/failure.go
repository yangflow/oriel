package locale

import (
	"regexp"
	"strings"
)

var (
	statusCode = regexp.MustCompile(`\bstatus (\d{3})\b`)
	endOfFile  = regexp.MustCompile(`\beof\b`)
)

// failureKind sorts one failure text of the fetch loop into a class and returns the phrase of that class in w, or
// "" for a text it does not know. Order matters: a DNS error is wrapped in "dial tcp", a feed that does not parse may
// end in "EOF".
func failureKind(w words, raw string) string {
	text := strings.ToLower(raw)
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(text, p) {
				return true
			}
		}
		return false
	}
	if m := statusCode.FindStringSubmatch(text); m != nil {
		switch code := m[1]; {
		case code == "401" || code == "403":
			return w.ErrForbidden
		case code == "404" || code == "410":
			return w.ErrNotFound
		case code == "429":
			return w.ErrRateLimited
		case code[0] == '5':
			return w.ErrServer
		}
		return ""
	}
	switch {
	case has("not a feed"):
		return w.ErrNotFeed
	case has("no such host", "lookup ", "resolve "):
		return w.ErrDNS
	case has("x509", "tls:", "certificate"):
		return w.ErrTLS
	case has("deadline exceeded", "timeout", "timed out"):
		return w.ErrTimeout
	case endOfFile.MatchString(text), has("connection refused", "connection reset", "dial tcp", "no route to host", "network is unreachable",
		"cannot reach the proxy", "the proxy failed"):
		return w.ErrNetwork
	}
	return ""
}

// FetchError is a short phrase in lang for the reason a fetch failed, which the server stores as English text, and
// whether the reason is one it knows: a timeout, a name that does not resolve, a certificate, a status the user can
// act on, or text that is not a feed. A text with several instances' reasons ("a: status 500; b: status 429") gets
// a phrase only when all of them are the same kind. Anything else comes back unchanged, so no reason is lost.
func FetchError(lang, raw string) (string, bool) {
	w := wordsOf(lang)
	phrase := ""
	for _, part := range strings.Split(raw, "; ") {
		kind := failureKind(w, part)
		if kind == "" || (phrase != "" && kind != phrase) {
			return raw, false
		}
		phrase = kind
	}
	if phrase == "" {
		return raw, false
	}
	return phrase, true
}

// WithOriginal adds the server's own text raw to text, which says a failure in the user's language, for a tooltip
// and a screen reader. Text that already carries the raw reason, or no reason, is returned as it is.
func WithOriginal(lang, text, raw string) string {
	if raw == "" || strings.Contains(text, raw) {
		return text
	}
	return text + "\n" + strings.ReplaceAll(wordsOf(lang).ErrOriginal, "{raw}", raw)
}
