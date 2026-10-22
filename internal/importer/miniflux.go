package importer

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// miniflux reads Miniflux through its REST API with an API token.
type miniflux struct{}

const minifluxPage = 100

type minifluxFeed struct {
	FeedURL  string `json:"feed_url"`
	SiteURL  string `json:"site_url"`
	Title    string `json:"title"`
	Category struct {
		Title string `json:"title"`
	} `json:"category"`
}

type minifluxEntry struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	Published time.Time `json:"published_at"`
	Status    string    `json:"status"`
	Starred   bool      `json:"starred"`
	Feed      struct {
		FeedURL string `json:"feed_url"`
	} `json:"feed"`
}

func (miniflux) Fetch(ctx context.Context, c *Client, req Request, progress Progress) (*Snapshot, error) {
	header := map[string]string{"X-Auth-Token": req.Credential}
	data, err := c.Do(ctx, "GET", req.Base+"/v1/feeds", header, nil)
	if err != nil {
		return nil, err
	}
	var feeds []minifluxFeed
	if err := json.Unmarshal(data, &feeds); err != nil {
		return nil, &Error{Code: ErrBadResponse}
	}
	snap := &Snapshot{}
	seen := map[string]bool{}
	for _, f := range feeds {
		if strings.TrimSpace(f.FeedURL) == "" {
			continue
		}
		feed := Feed{URL: f.FeedURL, Title: f.Title, SiteURL: f.SiteURL}
		if name := strings.TrimSpace(f.Category.Title); name != "" {
			feed.Folders = []string{name}
			addFolder(seen, &snap.Folders, name)
		}
		snap.Feeds = append(snap.Feeds, feed)
	}
	progress(StageSubscriptions, len(snap.Feeds), len(snap.Feeds))

	var m merge
	for _, stage := range []string{StageStarred, StageRead} {
		query := "starred=true"
		if stage == StageRead {
			query = "status=read"
		}
		got := 0
		for offset := 0; ; offset += minifluxPage {
			if got >= MaxEntries {
				snap.Notes = append(snap.Notes, Note{Kind: stage, Error: "limit"})
				break
			}
			page := req.Base + "/v1/entries?" + query + "&order=published_at&direction=desc&limit=" +
				strconv.Itoa(minifluxPage) + "&offset=" + strconv.Itoa(offset)
			data, err := c.Do(ctx, "GET", page, header, nil)
			if err != nil {
				return nil, err
			}
			var list struct {
				Total   int             `json:"total"`
				Entries []minifluxEntry `json:"entries"`
			}
			if err := json.Unmarshal(data, &list); err != nil {
				return nil, &Error{Code: ErrBadResponse}
			}
			for _, e := range list.Entries {
				got++
				if e.Status == "removed" || strings.TrimSpace(e.URL) == "" || e.Feed.FeedURL == "" {
					continue
				}
				m.add(Entry{
					FeedURL: e.Feed.FeedURL, Link: e.URL, Title: e.Title, Author: e.Author, Published: e.Published,
					ContentHTML: e.Content, Read: e.Status == "read", Starred: e.Starred,
				})
			}
			progress(stage, min(got, list.Total), min(list.Total, MaxEntries))
			if len(list.Entries) == 0 || offset+minifluxPage >= list.Total {
				break
			}
		}
	}
	snap.Entries = m.entries
	return snap, nil
}
