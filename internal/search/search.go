// Package search parses what is typed in the timeline's search field (docs/product.md, Search syntax). It touches no
// storage: the server turns the result into a query and the screen reads the words to highlight from it, so both
// agree on the grammar. Parsing never fails; whatever is not understood is a plain word.
package search

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MinIndexed is the fewest characters the trigram index can match; a shorter word or phrase is matched by LIKE.
const MinIndexed = 3

// MaxTokens is how many tokens of a query are read; the rest are ignored.
const MaxTokens = 16

// Kinds are the values kind: understands.
var Kinds = []string{"article", "video", "social", "image"}

// Day is a calendar day, with no time zone yet.
type Day struct {
	Year  int
	Month time.Month
	Day   int
}

// Start is the first instant of the day in loc.
func (d Day) Start(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

func (d Day) before(o Day) bool {
	if d.Year != o.Year {
		return d.Year < o.Year
	}
	if d.Month != o.Month {
		return d.Month < o.Month
	}
	return d.Day < o.Day
}

// Query is a parsed search. Words and phrases are Include terms; with a leading minus they are Exclude terms.
type Query struct {
	Include []string // words and phrases that must all be found
	Exclude []string // words and phrases that must not be found
	From    []string // a subscription name must contain any of these
	Kinds   []string // the entry's kind is any of these
	After   *Day     // on or after this day: the latest one given
	Before  *Day     // before this day: the earliest one given
}

// None reports whether the query asks for nothing, as an empty or blank field does.
func (q Query) None() bool {
	return len(q.Include) == 0 && len(q.Exclude) == 0 && len(q.From) == 0 && len(q.Kinds) == 0 && q.After == nil && q.Before == nil
}

// Short reports whether a word or a phrase is under MinIndexed characters, which the index cannot answer: the
// result is then matched by LIKE on the title and the summary only, and is limited.
func (q Query) Short() bool {
	for _, terms := range [][]string{q.Include, q.Exclude} {
		for _, t := range terms {
			if utf8.RuneCountInString(t) < MinIndexed {
				return true
			}
		}
	}
	return false
}

// Parse reads a query. Tokens are separated by white space; see docs/product.md for the grammar.
func Parse(s string) Query {
	var q Query
	p := parser{rs: []rune(s)}
	for n := 0; n < MaxTokens; {
		p.skipSpace()
		if p.done() {
			break
		}
		if p.token(&q) {
			n++
		}
	}
	return q
}

type parser struct {
	rs  []rune
	pos int
}

func (p *parser) done() bool { return p.pos >= len(p.rs) }

func (p *parser) skipSpace() {
	for !p.done() && unicode.IsSpace(p.rs[p.pos]) {
		p.pos++
	}
}

// token reads one token into q and reports whether it counted (an empty pair of quotes does not).
func (p *parser) token(q *Query) bool {
	negated := p.rs[p.pos] == '-' && p.pos+1 < len(p.rs) && !unicode.IsSpace(p.rs[p.pos+1])
	if negated {
		p.pos++
	}
	if p.rs[p.pos] == '"' {
		p.pos++
		phrase := p.quoted()
		if strings.TrimSpace(phrase) == "" {
			return false
		}
		q.add(negated, phrase)
		return true
	}
	if !negated && p.fromQuoted(q) {
		return true
	}
	word := p.word()
	if !negated {
		if name, value, ok := strings.Cut(word, ":"); ok && operator(q, strings.ToLower(name), value) {
			return true
		}
	}
	q.add(negated, word)
	return true
}

// quoted reads up to the closing quote, or to the end when it is missing; the opening quote is already read.
func (p *parser) quoted() string {
	var b strings.Builder
	for !p.done() {
		r := p.rs[p.pos]
		p.pos++
		switch {
		case r == '"':
			return b.String()
		case r == '\\' && !p.done() && (p.rs[p.pos] == '"' || p.rs[p.pos] == '\\'):
			b.WriteRune(p.rs[p.pos])
			p.pos++
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// word reads up to white space. A backslash escapes a quote or a backslash, and a minus at the start of the word.
func (p *parser) word() string {
	var b strings.Builder
	for first := true; !p.done() && !unicode.IsSpace(p.rs[p.pos]); first = false {
		r := p.rs[p.pos]
		p.pos++
		if r == '\\' && !p.done() {
			if next := p.rs[p.pos]; next == '"' || next == '\\' || (first && next == '-') {
				b.WriteRune(next)
				p.pos++
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// fromQuoted reads from:"a name with spaces" at the cursor. Quotes are only read after from:, and an empty name is
// the plain word from:.
func (p *parser) fromQuoted(q *Query) bool {
	const prefix = `from:"`
	if p.pos+len(prefix) > len(p.rs) || !strings.EqualFold(string(p.rs[p.pos:p.pos+len(prefix)]), prefix) {
		return false
	}
	p.pos += len(prefix)
	if name := strings.TrimSpace(p.quoted()); name != "" {
		q.From = append(q.From, name)
	} else {
		q.Include = append(q.Include, "from:")
	}
	return true
}

// operator applies name:value if it is a valid operator and reports whether it did.
func operator(q *Query, name, value string) bool {
	switch name {
	case "from":
		value = strings.TrimSpace(value)
		if value == "" {
			return false
		}
		q.From = append(q.From, value)
	case "kind":
		value = strings.ToLower(value)
		for _, k := range Kinds {
			if value == k {
				q.Kinds = append(q.Kinds, k)
				return true
			}
		}
		return false
	case "after", "before":
		t, err := time.Parse("2006-01-02", value)
		if err != nil {
			return false
		}
		d := Day{t.Year(), t.Month(), t.Day()}
		if name == "after" && (q.After == nil || q.After.before(d)) {
			q.After = &d
		} else if name == "before" && (q.Before == nil || d.before(*q.Before)) {
			q.Before = &d
		}
	default:
		return false
	}
	return true
}

func (q *Query) add(negated bool, term string) {
	if negated {
		q.Exclude = append(q.Exclude, term)
	} else {
		q.Include = append(q.Include, term)
	}
}
