package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

const (
	// WebSubSetting is the setting that holds the public base address of the server, such as https://oriel.example.com.
	// WebSub is off while it is empty. See docs/product.md "WebSub 推送".
	WebSubSetting = "websub_base_url"

	// WebSubPath is the callback route; the key of a source follows it.
	WebSubPath = "/websub/"

	websubEvery = 30 * time.Second // between two rounds of the subscribing loop
	websubBatch = 50               // sources handled in one round
	websubLease = 10 * 24 * time.Hour

	// A source with a live push subscription is still polled, but no more often than this.
	websubSafety = 6 * time.Hour
)

const atomNS = "http://www.w3.org/2005/Atom"

// WebSubBase returns the public base address WebSub is on for, empty when it is off.
func WebSubBase(ctx context.Context, st *store.Store) (string, error) {
	raw, err := st.Setting(ctx, WebSubSetting)
	return strings.TrimRight(strings.TrimSpace(raw), "/"), err
}

// websubLinks finds the hub and the topic (rel="self") a feed declares: first in the Link headers of the response,
// then inside the feed. Relative addresses resolve against base; only http and https addresses count. Either may
// be empty.
func websubLinks(header http.Header, body []byte, base *url.URL) (hub, self string) {
	for _, value := range header.Values("Link") {
		for _, link := range linkHeader(value) {
			target := absolute(base, link.target)
			if target == "" {
				continue
			}
			for _, rel := range strings.Fields(strings.ToLower(link.rel)) {
				switch {
				case rel == "hub" && hub == "":
					hub = target
				case rel == "self" && self == "":
					self = target
				}
			}
		}
	}
	if hub != "" && self != "" {
		return hub, self
	}
	var inHub, inSelf string
	if trimmed := bytes.TrimLeft(body, "\xef\xbb\xbf \t\r\n"); bytes.HasPrefix(trimmed, []byte("{")) {
		inHub, inSelf = jsonFeedLinks(trimmed)
	} else {
		inHub, inSelf = xmlFeedLinks(body)
	}
	if hub == "" {
		hub = absolute(base, inHub)
	}
	if self == "" {
		self = absolute(base, inSelf)
	}
	return hub, self
}

type headerLink struct{ target, rel string }

var (
	linkValue = regexp.MustCompile(`<([^>]*)>([^<]*)`)
	relParam  = regexp.MustCompile(`(?i);\s*rel\s*=\s*(?:"([^"]*)"|([^\s;,]*))`)
)

// linkHeader reads the values of a Link header.
func linkHeader(value string) []headerLink {
	var links []headerLink
	for _, m := range linkValue.FindAllStringSubmatch(value, -1) {
		link := headerLink{target: strings.TrimSpace(m[1])}
		if r := relParam.FindStringSubmatch(m[2]); r != nil {
			link.rel = r[1] + r[2]
		}
		links = append(links, link)
	}
	return links
}

func jsonFeedLinks(body []byte) (hub, self string) {
	var feed struct {
		FeedURL string `json:"feed_url"`
		Hubs    []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"hubs"`
	}
	if json.Unmarshal(body, &feed) != nil {
		return "", ""
	}
	for _, h := range feed.Hubs {
		if strings.EqualFold(strings.TrimSpace(h.Type), "websub") {
			hub = strings.TrimSpace(h.URL)
			break
		}
	}
	return hub, strings.TrimSpace(feed.FeedURL)
}

// xmlFeedLinks reads the Atom links that belong to the feed itself, the children of <feed> (Atom) or <channel>
// (RSS), and not those of its entries.
func xmlFeedLinks(body []byte) (hub, self string) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	var stack []string
	for {
		tok, err := dec.Token()
		if err != nil {
			return hub, self
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "link" && t.Name.Space == atomNS && len(stack) > 0 &&
				(stack[len(stack)-1] == "feed" || stack[len(stack)-1] == "channel") {
				var href, rel string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "href":
						href = strings.TrimSpace(a.Value)
					case "rel":
						rel = strings.ToLower(strings.TrimSpace(a.Value))
					}
				}
				switch {
				case rel == "hub" && hub == "":
					hub = href
				case rel == "self" && self == "":
					self = href
				}
				if hub != "" && self != "" {
					return hub, self
				}
			}
			stack = append(stack, t.Name.Local)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

// noteWebSub records the hub a freshly fetched feed declares, when WebSub is on. The topic is the feed's own
// address from rel="self", or the address it was fetched from.
func (p *Poller) noteWebSub(ctx context.Context, src store.FeedSource, header http.Header, body []byte, fetched *url.URL) error {
	base, err := WebSubBase(ctx, p.store)
	if err != nil || base == "" {
		return err
	}
	hub, topic := websubLinks(header, body, fetched)
	if hub == "" {
		topic = ""
	} else if topic == "" {
		topic = fetched.String()
	}
	if hub == src.WebSubHub && topic == src.WebSubTopic {
		return nil
	}
	return p.store.SetWebSubFeed(ctx, src.ID, hub, topic)
}

// nextFetch is when a source fetched at now is fetched again. A source with a live push subscription is only
// polled as a safety net, but never later than its lease runs out.
func (p *Poller) nextFetch(ctx context.Context, src store.FeedSource, interval time.Duration, now time.Time) (time.Time, error) {
	next := now.Add(interval)
	if src.WebSubUntil.IsZero() || !now.Before(src.WebSubUntil) {
		return next, nil
	}
	base, err := WebSubBase(ctx, p.store)
	if err != nil || base == "" {
		return next, err
	}
	return minTime(now.Add(max(interval, websubSafety)), src.WebSubUntil), nil
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// websubRound subscribes to the hubs of sources that need it and renews the leases that are running out.
func (p *Poller) websubRound(ctx context.Context) {
	base, err := WebSubBase(ctx, p.store)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot read the WebSub address", "error", err)
		}
		return
	}
	if base == "" {
		return
	}
	due, err := p.store.WebSubDue(ctx, p.now(), base, websubBatch)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list WebSub subscriptions", "error", err)
		}
		return
	}
	for _, w := range due {
		if ctx.Err() != nil {
			return
		}
		p.subscribe(ctx, w, base)
	}
}

// subscribe asks the hub for a push subscription whose callback is under base. The state is written before the
// request goes out, because the hub may verify it before it answers.
func (p *Poller) subscribe(ctx context.Context, w store.WebSub, base string) {
	now := p.now()
	w, err := p.store.WebSubRequest(ctx, w.SourceID, base, now)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot record a WebSub request", "source", w.SourceID, "error", err)
		}
		return
	}
	if err := p.sendSubscribe(ctx, w, base); err != nil {
		if ctx.Err() != nil {
			return
		}
		if err := p.store.WebSubFailed(ctx, w.SourceID, err.Error(), p.now()); err != nil {
			slog.Error("Cannot record a WebSub failure", "source", w.SourceID, "error", err)
		}
	}
}

func (p *Poller) sendSubscribe(ctx context.Context, w store.WebSub, base string) error {
	form := url.Values{
		"hub.mode":          {"subscribe"},
		"hub.topic":         {w.Topic},
		"hub.callback":      {base + WebSubPath + w.Key},
		"hub.secret":        {w.Secret},
		"hub.lease_seconds": {strconv.Itoa(int(websubLease / time.Second))},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.Hub, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", p.userAgent)
	client, err := p.network.Guarded(ctx, w.Proxy, p.guarded)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("hub answered status %d", resp.StatusCode)
	}
	return nil
}
