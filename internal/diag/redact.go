// Package diag writes the diagnostics file (docs/product.md, "诊断文件"): the version, the system, the recent errors and
// the failures of each subscription, with nothing in it that is a secret, a full address or the reader's own content.
package diag

import (
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxText is how many characters one value, message or error keeps.
const maxText = 300

var (
	address    = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s"'<>\\]+`)
	assignment = regexp.MustCompile(`(?i)([\w.\-]*(?:key|token|password|passwd|secret|credential|authorization|cookie|signature)[\w.\-]*)=[^\s&"']+`)
	bearer     = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=\-]+`)
)

// home is the user's home directory, which names the local user; Redact writes it as "~".
var home, _ = os.UserHomeDir()

// sensitive are the words in the name of an attribute that make its value a secret.
var sensitive = []string{"key", "token", "password", "passwd", "secret", "credential", "authorization", "cookie", "signature"}

// Sensitive reports whether an attribute with this name must not be written at all.
func Sensitive(name string) bool {
	name = strings.ToLower(name)
	for _, word := range sensitive {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}

// Redact cuts every address in text down to scheme and host, writes the home directory as "~", blanks values that are assigned to a name that looks like
// a secret ("token=abc") and credentials after Bearer or Basic, and keeps at most maxText characters.
func Redact(text string) string { return redact(text, maxText) }

// redact is Redact keeping at most limit characters.
func redact(text string, limit int) string {
	text = address.ReplaceAllStringFunc(text, hostOnly)
	if len(home) > 1 {
		text = strings.ReplaceAll(text, home, "~")
	}
	text = assignment.ReplaceAllString(text, "$1=[redacted]")
	text = bearer.ReplaceAllString(text, "$1 [redacted]")
	return truncate(text, limit)
}

// hostOnly is an address without user name, password, path, query and fragment.
func hostOnly(address string) string {
	scheme, rest, _ := strings.Cut(address, "://")
	authority := rest
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		authority = authority[i+1:]
	}
	return scheme + "://" + authority
}

func truncate(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}
