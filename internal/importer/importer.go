// Package importer reads subscriptions, starred entries and read state out of another feed reader.
//
// Each service is an Importer that turns what its API answers into one Snapshot; nothing here writes to Oriel. The
// credential travels only in a Request, is sent only to the address the user typed, and is not kept: the Snapshot
// has no trace of it and errors carry a code, never the text of a transport error.
package importer

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// The services that can be imported from.
const (
	Miniflux = "miniflux"
	FreshRSS = "freshrss"
)

// Services lists the services in the order the interface shows them.
var Services = []string{Miniflux, FreshRSS}

// Stages of the progress an Importer reports.
const (
	StageSubscriptions = "subscriptions"
	StageStarred       = "starred"
	StageRead          = "read"
)

// MaxEntries is how many starred entries, and how many read entries, an import reads. A service with more has the rest
// left out, and the Snapshot says so in a Note.
const MaxEntries = 20000

// Request is what an import needs to reach a service. Base is already normalised by ParseBase.
type Request struct {
	Base       string
	Username   string // FreshRSS only
	Credential string // the API token (Miniflux) or API password (FreshRSS)
}

// Feed is a subscription in the other reader.
type Feed struct {
	URL     string
	Title   string
	SiteURL string
	Folders []string // the folders or tags it is in there
}

// Entry is an entry of a subscription, with its state in the other reader. Entries are merged by feed and link, so
// one that is both starred and read appears once.
type Entry struct {
	FeedURL     string
	Link        string
	Title       string
	Author      string
	Published   time.Time
	ContentHTML string
	Read        bool
	Starred     bool
}

// Note says that part of a stream was left out. Kind is the stream (StageStarred or StageRead), Error the reason.
type Note struct {
	Kind  string
	Error string
}

// Snapshot is everything read from a service.
type Snapshot struct {
	Feeds   []Feed
	Folders []string // the distinct folder or tag names
	Entries []Entry
	Notes   []Note
}

// Counts are the figures of a snapshot shown before anything is written.
type Counts struct {
	Sources int `json:"sources"`
	Folders int `json:"folders"`
	Starred int `json:"starred"`
	Read    int `json:"read"`
}

// Counts counts the snapshot.
func (s *Snapshot) Counts() Counts {
	c := Counts{Sources: len(s.Feeds), Folders: len(s.Folders)}
	for _, e := range s.Entries {
		if e.Starred {
			c.Starred++
		}
		if e.Read {
			c.Read++
		}
	}
	return c
}

// Progress reports that done of total items of a stage are read; total is 0 when the service does not say.
type Progress func(stage string, done, total int)

// Importer reads one service.
type Importer interface {
	Fetch(ctx context.Context, c *Client, req Request, progress Progress) (*Snapshot, error)
}

// For returns the importer of a service.
func For(service string) (Importer, bool) {
	switch service {
	case Miniflux:
		return miniflux{}, true
	case FreshRSS:
		return freshRSS{}, true
	}
	return nil, false
}

// ParseBase checks the address of a service and returns it without a trailing slash. It must be an http or https
// address with a host and without a user name, password, query or fragment. The path stays, for a service served
// under one. For FreshRSS a trailing /api/greader.php is taken off, so either the site or the API address will do.
func ParseBase(raw, service string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", &Error{Code: ErrInvalidBase}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if service == FreshRSS {
		u.Path = strings.TrimRight(strings.TrimSuffix(u.Path, "/api/greader.php"), "/")
	}
	u.RawPath = ""
	return u.String(), nil
}

// add records a feed's folder once.
func addFolder(seen map[string]bool, folders *[]string, name string) {
	name = strings.TrimSpace(name)
	if name == "" || seen[name] {
		return
	}
	seen[name] = true
	*folders = append(*folders, name)
}

// merge keeps one Entry for each feed and link, and ORs the state of those that repeat. The order of first sight
// is kept. Only a starred entry keeps its content: a read one is only ever marked, so its content would just be held
// in memory.
type merge struct {
	index   map[string]int
	entries []Entry
}

func (m *merge) add(e Entry) {
	if !e.Starred {
		e.ContentHTML = ""
	}
	if m.index == nil {
		m.index = map[string]int{}
	}
	key := e.FeedURL + "\n" + e.Link
	if i, ok := m.index[key]; ok {
		have := &m.entries[i]
		have.Read = have.Read || e.Read
		have.Starred = have.Starred || e.Starred
		if have.ContentHTML == "" {
			have.ContentHTML = e.ContentHTML
		}
		return
	}
	m.index[key] = len(m.entries)
	m.entries = append(m.entries, e)
}
