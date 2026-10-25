package server

import (
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
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
// orielKind, orielRsshubPath, orielOpenTarget, orielHideFromAll, orielTranslateTitles and orielDailyCap keep what OPML has no place for.
type opmlOutline struct {
	Text            string        `xml:"text,attr"`
	Title           string        `xml:"title,attr,omitempty"`
	Type            string        `xml:"type,attr,omitempty"`
	XMLURL          string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL         string        `xml:"htmlUrl,attr,omitempty"`
	Kind            string        `xml:"orielKind,attr,omitempty"`
	RSSHubPath      string        `xml:"orielRsshubPath,attr,omitempty"`
	OpenTarget      string        `xml:"orielOpenTarget,attr,omitempty"`
	HideFromAll     string        `xml:"orielHideFromAll,attr,omitempty"`
	TranslateTitles string        `xml:"orielTranslateTitles,attr,omitempty"`
	DailyCap        string        `xml:"orielDailyCap,attr,omitempty"`
	Outlines        []opmlOutline `xml:"outline"`
}

// source reads a subscription outline. The RSSHub path wins over xmlUrl, the kind defaults to article, and a title
// that only repeats the address counts as none, so that the next fetch fills it in.
func (o opmlOutline) source() (store.NewSource, string) {
	src := store.NewSource{Kind: o.Kind, OpenTarget: strings.TrimSpace(o.OpenTarget), FetchInterval: defaultInterval, FetchIntervalAuto: true,
		HideFromAll: strings.TrimSpace(o.HideFromAll) == "1", TranslateTitles: strings.TrimSpace(o.TranslateTitles) == "1"}
	if n, err := strconv.Atoi(strings.TrimSpace(o.DailyCap)); err == nil && n >= 1 && n <= store.MaxDailyCap {
		src.DailyCap = n
	}
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

// opmlResult is what an import reports: the subscriptions it added, the ones that were there already, and the ones
// it could not add.
type opmlResult struct {
	Added    int             `json:"added"`
	Existing int             `json:"existing"`
	Folders  int             `json:"folders"` // folders the import created
	Failed   []importFailure `json:"failed"`
}

// readOPML reads and checks the file of an import request. It answers the request itself and returns false when the
// file is too large or is not OPML, so that nothing is added and no job starts.
func readOPML(w http.ResponseWriter, r *http.Request) (opmlDocument, bool) {
	var doc opmlDocument
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxOPML))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, http.StatusRequestEntityTooLarge, "opml_too_large")
		return doc, false
	}
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_opml")
		return doc, false
	}
	// A file in another encoding (GBK, ISO-8859-1) says so in its XML declaration.
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(&doc); err != nil {
		fail(w, http.StatusBadRequest, "invalid_opml")
		return doc, false
	}
	return doc, true
}

// addSources adds the subscriptions of doc and calls progress with how many it has handled, after each one. Those it
// has added stay when it stops on an error; the result then holds the counts up to there.
func (s *Server) addSources(ctx context.Context, doc opmlDocument, progress func(done int)) (opmlResult, error) {
	res := opmlResult{Failed: []importFailure{}}
	idx, err := s.store.DuplicateIndex(ctx)
	if err != nil {
		return res, err
	}
	done := 0
	spread := min(importSpreadMax, time.Duration(countOutlines(doc.Body))*importSpreadEach)
	// folder is the name of the outermost group the outlines are in: folders have one level.
	var walk func([]opmlOutline, string) error
	walk = func(outlines []opmlOutline, folder string) error {
		for _, o := range outlines {
			if o.XMLURL != "" || o.RSSHubPath != "" {
				src, title := o.source()
				switch code := checkSource(src); {
				case code != "":
					res.Failed = append(res.Failed, importFailure{Title: title, Address: src.URL + src.RSSHubPath, Error: code})
				default:
					src.FirstFetchDelay = time.Duration(rand.Int64N(int64(max(spread, 1))))
					if folder != "" {
						id, created, err := s.store.EnsureFolder(ctx, folder, s.now())
						if err != nil && !errors.Is(err, store.ErrInvalidFolderName) {
							return err
						}
						src.FolderID = id
						if created {
							res.Folders++
						}
					}
					_, err := s.store.CreateSourceIn(ctx, idx, src, title, s.now())
					var exists *store.ExistsError
					switch {
					case errors.As(err, &exists):
						res.Existing++
						// One that was there already is filed only when it was in no folder.
						if src.FolderID != 0 {
							if _, err := s.store.FileSource(ctx, exists.ID, src.FolderID); err != nil {
								return err
							}
						}
					case errors.Is(err, store.ErrExists):
						res.Existing++
					case err != nil:
						return err
					default:
						res.Added++
					}
				}
				done++
				progress(done)
			}
			inner := folder
			if inner == "" && o.XMLURL == "" && o.RSSHubPath == "" {
				inner = strings.TrimSpace(cmp.Or(o.Title, o.Text))
			}
			if err := walk(o.Outlines, inner); err != nil {
				return err
			}
		}
		return nil
	}
	return res, walk(doc.Body, "")
}

// importOPML adds the subscriptions of the file and answers when it is done. It takes the same one-at-a-time lock as
// the import job.
func (s *Server) importOPML(w http.ResponseWriter, r *http.Request) {
	doc, ok := readOPML(w, r)
	if !ok {
		return
	}
	if !s.opml.begin() {
		fail(w, http.StatusConflict, "opml_import_busy")
		return
	}
	defer s.opml.endSync()
	res, err := s.addSources(r.Context(), doc, func(int) {})
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, res)
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
	folders, err := s.store.Folders(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	doc := opmlDocument{Version: "2.0", Title: "Oriel"}
	groups := map[int64]*opmlOutline{}
	for _, f := range folders {
		groups[f.ID] = &opmlOutline{Text: f.Name, Title: f.Name}
	}
	var unfiled []opmlOutline
	for _, src := range list {
		o := opmlOutline{Text: src.Title, Title: src.Title, Type: "rss", XMLURL: src.URL, HTMLURL: src.SiteURL, Kind: src.Kind}
		if src.OpenTarget != store.OpenFollow {
			o.OpenTarget = src.OpenTarget
		}
		if src.HideFromAll {
			o.HideFromAll = "1"
		}
		if src.TranslateTitles {
			o.TranslateTitles = "1"
		}
		if src.DailyCap > 0 {
			o.DailyCap = strconv.Itoa(src.DailyCap)
		}
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
		if group, ok := groups[src.FolderID]; ok {
			group.Outlines = append(group.Outlines, o)
		} else {
			unfiled = append(unfiled, o)
		}
	}
	// One group per folder that has subscriptions, then the subscriptions in no folder.
	for _, f := range folders {
		if group := groups[f.ID]; len(group.Outlines) > 0 {
			doc.Body = append(doc.Body, *group)
		}
	}
	doc.Body = append(doc.Body, unfiled...)
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
