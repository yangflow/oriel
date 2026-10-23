package server

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/store"
	"golang.org/x/net/html/charset"
)

// maxOPML is the largest OPML file an import accepts.
const maxOPML = 1 << 20

// An import spreads the first fetches of its sources over importSpreadEach per subscription in the file, at most
// importSpreadMax, so that hundreds of new sources do not all become due at once.
const (
	importSpreadEach = 2 * time.Second
	importSpreadMax  = 10 * time.Minute
)

// countOutlines counts the outlines that are subscriptions, in groups too.
func countOutlines(outlines []opmlOutline) int {
	n := 0
	for _, o := range outlines {
		if o.XMLURL != "" || o.RSSHubPath != "" {
			n++
		}
		n += countOutlines(o.Outlines)
	}
	return n
}

type opmlDocument struct {
	XMLName xml.Name      `xml:"opml"`
	Version string        `xml:"version,attr"`
	Title   string        `xml:"head>title"`
	Body    []opmlOutline `xml:"body>outline"`
}

// opmlOutline is a subscription when it has an xmlUrl or an orielRsshubPath, otherwise a group.
// orielKind and orielRsshubPath keep what OPML has no place for.
type opmlOutline struct {
	Text       string        `xml:"text,attr"`
	Title      string        `xml:"title,attr,omitempty"`
	Type       string        `xml:"type,attr,omitempty"`
	XMLURL     string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL    string        `xml:"htmlUrl,attr,omitempty"`
	Kind       string        `xml:"orielKind,attr,omitempty"`
	RSSHubPath string        `xml:"orielRsshubPath,attr,omitempty"`
	Outlines   []opmlOutline `xml:"outline"`
}

// source reads a subscription outline. The RSSHub path wins over xmlUrl, the kind defaults to article, and a title
// that only repeats the address counts as none, so that the next fetch fills it in.
func (o opmlOutline) source() (store.NewSource, string) {
	src := store.NewSource{Kind: o.Kind, FetchInterval: defaultInterval, FetchIntervalAuto: true}
	if src.RSSHubPath = strings.TrimSpace(o.RSSHubPath); src.RSSHubPath == "" {
		src.URL = strings.TrimSpace(o.XMLURL)
	}
	if src.Kind == "" {
		src.Kind = "article"
	}
	title := strings.TrimSpace(o.Title)
	if title == "" {
		title = strings.TrimSpace(o.Text)
	}
	if title == src.URL+src.RSSHubPath {
		title = ""
	}
	return src, title
}

type importFailure struct {
	Title   string `json:"title"`
	Address string `json:"address"`
	Error   string `json:"error"`
}

func (s *Server) importOPML(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxOPML))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, http.StatusRequestEntityTooLarge, "opml_too_large")
		return
	}
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_opml")
		return
	}
	var doc opmlDocument
	// A file in another encoding (GBK, ISO-8859-1) says so in its XML declaration.
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(&doc); err != nil {
		fail(w, http.StatusBadRequest, "invalid_opml")
		return
	}
	idx, err := s.store.DuplicateIndex(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	added, existing, failed := 0, 0, []importFailure{}
	spread := min(importSpreadMax, time.Duration(countOutlines(doc.Body))*importSpreadEach)
	var walk func([]opmlOutline) error
	walk = func(outlines []opmlOutline) error {
		for _, o := range outlines {
			if o.XMLURL != "" || o.RSSHubPath != "" {
				src, title := o.source()
				switch code := checkSource(src); {
				case code != "":
					failed = append(failed, importFailure{Title: title, Address: src.URL + src.RSSHubPath, Error: code})
				default:
					src.FirstFetchDelay = time.Duration(rand.Int64N(int64(max(spread, 1))))
					_, err := s.store.CreateSourceIn(r.Context(), idx, src, title, s.now())
					switch {
					case errors.Is(err, store.ErrExists):
						existing++
					case err != nil:
						return err
					default:
						added++
					}
				}
			}
			if err := walk(o.Outlines); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(doc.Body); err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"added": added, "existing": existing, "failed": failed})
}

func (s *Server) exportOPML(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Sources(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	raw, err := s.store.Setting(r.Context(), fetch.RSSHubSetting)
	if err != nil {
		internal(w, err)
		return
	}
	root := "" // the primary instance, which other readers can use
	if instances := fetch.Instances(raw); len(instances) > 0 {
		root = instances[0]
	}
	doc := opmlDocument{Version: "2.0", Title: "Oriel", Body: make([]opmlOutline, len(list))}
	for i, src := range list {
		o := opmlOutline{Text: src.Title, Title: src.Title, Type: "rss", XMLURL: src.URL, HTMLURL: src.SiteURL, Kind: src.Kind}
		if src.RSSHubPath != "" {
			o.RSSHubPath = src.RSSHubPath
			// Other readers need a full address; without a RSSHub root there is none to give.
			if root != "" {
				o.XMLURL = strings.TrimRight(root, "/") + src.RSSHubPath
			}
		}
		if o.Text == "" {
			o.Text = src.URL + src.RSSHubPath
		}
		doc.Body[i] = o
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="oriel.opml"`)
	w.Write([]byte(xml.Header))
	w.Write(append(data, '\n'))
}
