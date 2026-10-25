package importer

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// freshRSS reads FreshRSS (or any server speaking the Google Reader API) with a user name and an API password.
type freshRSS struct{}

const (
	freshPage      = 100
	readStateTag   = "user/-/state/com.google/read"
	starredStream  = "user/-/state/com.google/starred"
	readStream     = "user/-/state/com.google/read"
	feedStreamHead = "feed/"
)

type freshItem struct {
	Title     string `json:"title"`
	Author    string `json:"author"`
	Published int64  `json:"published"`
	Canonical []struct {
		Href string `json:"href"`
	} `json:"canonical"`
	Alternate []struct {
		Href string `json:"href"`
	} `json:"alternate"`
	Summary struct {
		Content string `json:"content"`
	} `json:"summary"`
	Content struct {
		Content string `json:"content"`
	} `json:"content"`
	Categories []string `json:"categories"`
	Origin     struct {
		StreamID string `json:"streamId"`
	} `json:"origin"`
}

func (freshRSS) Fetch(ctx context.Context, c *Client, req Request, progress Progress) (*Snapshot, error) {
	api := req.Base + "/api/greader.php"
	data, err := c.Do(ctx, "POST", api+"/accounts/ClientLogin", nil,
		url.Values{"Email": {req.Username}, "Passwd": {req.Credential}})
	if err != nil {
		return nil, err
	}
	var auth string
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Auth="); ok {
			auth = v
		}
	}
	if auth == "" {
		return nil, &Error{Code: ErrBadResponse}
	}
	header := map[string]string{"Authorization": "GoogleLogin auth=" + auth}
	reader := api + "/reader/api/0/"

	data, err = c.Do(ctx, "GET", reader+"subscription/list?output=json", header, nil)
	if err != nil {
		return nil, err
	}
	var subs struct {
		Subscriptions []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			URL        string `json:"url"`
			HTMLURL    string `json:"htmlUrl"`
			Categories []struct {
				Label string `json:"label"`
			} `json:"categories"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, &Error{Code: ErrBadResponse}
	}
	snap := &Snapshot{}
	seen := map[string]bool{}
	urls := map[string]string{} // subscription id -> feed address
	for _, s := range subs.Subscriptions {
		if strings.TrimSpace(s.URL) == "" {
			continue
		}
		urls[s.ID] = s.URL
		feed := Feed{URL: s.URL, Title: s.Title, SiteURL: s.HTMLURL}
		// A subscription is in at most one folder here: the first label stays, the others are listed as dropped.
		for _, cat := range s.Categories {
			name := strings.TrimSpace(cat.Label)
			switch {
			case name == "":
			case len(feed.Folders) == 0:
				feed.Folders = append(feed.Folders, name)
				addFolder(seen, &snap.Folders, name)
			default:
				feed.DroppedLabels = append(feed.DroppedLabels, name)
			}
		}
		snap.Feeds = append(snap.Feeds, feed)
	}

	progress(StageSubscriptions, len(snap.Feeds), len(snap.Feeds))

	// The read state comes from the contents of the read stream rather than from the ids of its items, because the ids
	// carry no link to match an entry by.
	var m merge
	for _, stage := range []string{StageStarred, StageRead} {
		stream := starredStream
		if stage == StageRead {
			stream = readStream
		}
		got, cont := 0, ""
		for {
			if got >= MaxEntries {
				snap.Notes = append(snap.Notes, Note{Kind: stage, Error: "limit"})
				break
			}
			page := reader + "stream/contents/" + stream + "?output=json&n=" + strconv.Itoa(freshPage)
			if cont != "" {
				page += "&c=" + url.QueryEscape(cont)
			}
			data, err := c.Do(ctx, "GET", page, header, nil)
			if err != nil {
				return nil, err
			}
			var list struct {
				Items        []freshItem `json:"items"`
				Continuation string      `json:"continuation"`
			}
			if err := json.Unmarshal(data, &list); err != nil {
				return nil, &Error{Code: ErrBadResponse}
			}
			for _, it := range list.Items {
				got++
				link := ""
				if len(it.Canonical) > 0 {
					link = it.Canonical[0].Href
				} else if len(it.Alternate) > 0 {
					link = it.Alternate[0].Href
				}
				feedURL := urls[it.Origin.StreamID]
				if feedURL == "" {
					if v, ok := strings.CutPrefix(it.Origin.StreamID, feedStreamHead); ok && strings.HasPrefix(v, "http") {
						feedURL = v
					}
				}
				if strings.TrimSpace(link) == "" || (feedURL == "" && stage == StageRead) {
					continue
				}
				e := Entry{
					FeedURL: feedURL, Link: link, Title: it.Title, Author: it.Author, ContentHTML: it.Content.Content,
					Read: stage == StageRead, Starred: stage == StageStarred,
				}
				if e.ContentHTML == "" {
					e.ContentHTML = it.Summary.Content
				}
				if it.Published > 0 {
					e.Published = time.Unix(it.Published, 0).UTC()
				}
				for _, cat := range it.Categories {
					if cat == readStateTag {
						e.Read = true
					}
				}
				m.add(e)
			}
			progress(stage, got, 0)
			cont = list.Continuation
			if cont == "" || len(list.Items) == 0 {
				break
			}
		}
	}
	snap.Entries = m.entries
	return snap, nil
}
