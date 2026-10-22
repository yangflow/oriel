package fetch

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/andybalholm/cascadia"
)

// The limits of a source's extraction rules (docs/product.md, "正文抽取").
const (
	maxSelectorLen = 500
	maxRefererLen  = 2000
	maxRewrites    = 10
	maxRewriteLen  = 500
)

var (
	// ErrInvalidSelector is a content selector that is too long or that CSS cannot parse.
	ErrInvalidSelector = errors.New("invalid content selector")
	// ErrInvalidReferer is a Referer that is not an absolute http or https address with a host, or is too long.
	ErrInvalidReferer = errors.New("invalid referer")
	// ErrInvalidRewrites is a list of rewrite rules that is too long or has a line that is not a rule.
	ErrInvalidRewrites = errors.New("invalid rewrite rules")
)

// ParseSelector checks a content selector, trimmed of surrounding space, and returns the trimmed text. An empty
// selector is valid and means automatic extraction.
func ParseSelector(selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", nil
	}
	if len([]rune(selector)) > maxSelectorLen {
		return "", ErrInvalidSelector
	}
	if _, err := cascadia.Parse(selector); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSelector, err)
	}
	return selector, nil
}

// ParseReferer checks the Referer to send, trimmed of surrounding space, and returns the trimmed text. An empty one
// is valid and means none is sent.
func ParseReferer(referer string) (string, error) {
	referer = strings.TrimSpace(referer)
	if referer == "" {
		return "", nil
	}
	if len([]rune(referer)) > maxRefererLen {
		return "", ErrInvalidReferer
	}
	u, err := url.Parse(referer)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", ErrInvalidReferer
	}
	return referer, nil
}

// rewrite is one rule. A host rule swaps the host name of a link, a prefix rule swaps the start of its address.
type rewrite struct {
	from, to string
	host     bool
}

// ParseRewrites checks the rewrite rules, one "from => to" per line, and returns them in their normal form: each line
// trimmed, blank lines dropped, the spaces around "=>" gone. An empty text is valid and means no rules.
func ParseRewrites(text string) (string, error) {
	rules, err := parseRewrites(text)
	if err != nil {
		return "", err
	}
	lines := make([]string, len(rules))
	for i, r := range rules {
		lines[i] = r.from + " => " + r.to
	}
	return strings.Join(lines, "\n"), nil
}

func parseRewrites(text string) ([]rewrite, error) {
	var rules []rewrite
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(rules) == maxRewrites || len([]rune(line)) > maxRewriteLen {
			return nil, ErrInvalidRewrites
		}
		from, to, ok := strings.Cut(line, "=>")
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if !ok || from == "" || to == "" || strings.Contains(to, "=>") {
			return nil, fmt.Errorf("%w: %q", ErrInvalidRewrites, line)
		}
		switch {
		case isWebPrefix(from) && isWebPrefix(to):
			rules = append(rules, rewrite{from: from, to: to})
		case !strings.Contains(from, "/") && !strings.Contains(to, "/") && !strings.ContainsAny(from+to, " \t"):
			rules = append(rules, rewrite{from: strings.ToLower(from), to: to, host: true})
		default:
			return nil, fmt.Errorf("%w: %q", ErrInvalidRewrites, line)
		}
	}
	return rules, nil
}

func isWebPrefix(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// rewriteLink applies the rules to an entry link from top to bottom, each seeing the result of the one before. The
// result must still be an http or https address with a host.
func rewriteLink(address string, rules []rewrite) (string, error) {
	if len(rules) == 0 {
		return address, nil
	}
	for _, r := range rules {
		if r.host {
			u, err := url.Parse(address)
			if err != nil || !strings.EqualFold(u.Host, r.from) {
				continue
			}
			u.Host = r.to
			address = u.String()
		} else if strings.HasPrefix(address, r.from) {
			address = r.to + address[len(r.from):]
		}
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("a rewrite gave %q, which is not a web address", address)
	}
	return address, nil
}
