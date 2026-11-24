// Package rss is the reference connector (docs/connectors.md): an RSS or Atom feed, polled gently with ETag and
// If-Modified-Since, whose new items become feed.item events and which the agent reads with
// `harnessly connector read <connection> items|item`. It has no action outward.
package rss

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/htmlindex"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/connectors"
)

// ID names the connector.
const ID = "rss"

const (
	// EventItem is the one event: a new item in the feed.
	EventItem = "feed.item"
	// defaultEvery and minEvery are the poll interval in minutes: gentle on someone else's server.
	defaultEvery = 30
	minEvery     = 15
	maxBody      = 5 << 20
	// knownMax ids of the feed are kept in the cursor to tell a new item from an old one.
	knownMax  = 500
	userAgent = "harnessly-feed/1 (+https://github.com/ly-solutions/harnessly)"
)

type feedConn struct{}

func init() { connectors.Register(feedConn{}) }

func (feedConn) Spec() connectors.Spec {
	return connectors.Spec{
		ID:      ID,
		Kind:    connectors.Feed,
		Inbox:   "feed", // HRN-1727: the inbox takes a feed's items (harnessly-inbox 0.10.12)
		Title:   connectors.T("RSS or Atom feed", "Лента RSS или Atom"),
		Summary: connectors.T("New posts of a blog, a release page or a news site", "Новые записи блога, страницы релизов или новостного сайта"),
		Fields: []connectors.Field{
			{ID: "url", Type: connectors.FieldURL, Label: connectors.T("Feed address", "Адрес ленты"), Placeholder: "https://example.com/feed.xml"},
			{ID: "every", Type: connectors.FieldNumber, Optional: true, Default: strconv.Itoa(defaultEvery),
				Label: connectors.T("Check every, minutes", "Проверять раз в, минут"),
				Hint:  connectors.T("At least 15", "Не чаще раза в 15 минут")},
		},
		Events: []string{EventItem},
		Tools: []connectors.Tool{
			{Name: "items", Summary: "the latest items of the feed: title, date, link and id", Args: []connectors.Arg{{Name: "limit", Summary: "how many, 20 by default"}}},
			{Name: "item", Summary: "one item with its text", Args: []connectors.Arg{{Name: "id", Summary: "the item's id from items or from an event", Required: true}}},
		},
	}
}

// item is a feed's entry in one format for RSS 2.0, RSS 1.0 and Atom.
type item struct {
	ID        string
	Title     string
	Link      string
	Author    string
	Published time.Time
	Text      string
}

type feed struct {
	Title string
	Items []item
}

// cursor is what the runtime keeps between polls: the validators of the last answer and the ids already seen.
type cursor struct {
	ETag         string   `json:"etag,omitempty"`
	LastModified string   `json:"lastModified,omitempty"`
	Known        []string `json:"known"`
}

func readCursor(c connectors.Cursor) (cursor, bool) {
	var cur cursor
	if c == "" || json.Unmarshal([]byte(c), &cur) != nil {
		return cursor{}, false
	}
	return cur, true
}

func (c cursor) encode() connectors.Cursor {
	raw, _ := json.Marshal(c)
	return connectors.Cursor(raw)
}

// fetch asks the feed, conditionally when validators are given. notModified is a 304.
func fetch(ctx context.Context, client *http.Client, raw string, cur cursor) (f *feed, next cursor, notModified bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, cur, false, connectors.Sayf("The feed address is not valid")
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.1")
	if cur.ETag != "" {
		req.Header.Set("If-None-Match", cur.ETag)
	}
	if cur.LastModified != "" {
		req.Header.Set("If-Modified-Since", cur.LastModified)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, cur, false, connectors.Sayf("The feed does not answer: %s", shortErr(err))
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified {
		return nil, cur, true, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, cur, false, connectors.Sayf("The feed answered %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, cur, false, connectors.Sayf("The feed broke off: %s", shortErr(err))
	}
	f, err = parse(body)
	if err != nil {
		return nil, cur, false, err
	}
	next = cur
	next.ETag, next.LastModified = res.Header.Get("ETag"), res.Header.Get("Last-Modified")
	return f, next, false, nil
}

func shortErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func (feedConn) Check(ctx context.Context, in connectors.Instance) (connectors.Account, error) {
	f, _, _, err := fetch(ctx, in.HTTP, in.Config["url"], cursor{})
	if err != nil {
		return connectors.Account{}, err
	}
	label := strings.TrimSpace(f.Title)
	if label == "" {
		u, _ := url.Parse(in.Config["url"])
		label = u.Host + u.Path
	}
	return connectors.Account{Label: label}, nil
}

func (feedConn) Interval(in connectors.Instance) time.Duration {
	n, err := strconv.Atoi(in.Config["every"])
	if err != nil || n <= 0 {
		n = defaultEvery
	}
	return time.Duration(max(n, minEvery)) * time.Minute
}

// Poll asks the feed; items whose id the cursor does not know are new. The first poll only learns the feed: what is
// already in it when the person connects is not news.
func (feedConn) Poll(ctx context.Context, in connectors.Instance, from connectors.Cursor) ([]connectors.Event, connectors.Cursor, error) {
	cur, had := readCursor(from)
	f, next, notModified, err := fetch(ctx, in.HTTP, in.Config["url"], cur)
	if err != nil {
		return nil, from, err
	}
	if notModified {
		return nil, from, nil
	}
	known := map[string]bool{}
	for _, id := range cur.Known {
		known[id] = true
	}
	var events []connectors.Event
	next.Known = nil
	for _, it := range f.Items {
		id := itemID(it)
		next.Known = append(next.Known, id)
		if !had || known[id] {
			continue
		}
		events = append(events, connectors.Event{
			Type: EventItem, ID: id, At: it.Published, Actor: it.Author, Title: it.Title, URL: it.Link,
			Ref: "item id=" + id,
		})
	}
	// Ids that left the feed are kept a while, so an item that comes back is not new again.
	for _, id := range cur.Known {
		if len(next.Known) >= knownMax {
			break
		}
		if !contains(next.Known, id) {
			next.Known = append(next.Known, id)
		}
	}
	// The oldest first: the order they were published in.
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, next.encode(), nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// itemID is a short stable id of an item: of its guid, else its link, else its title and date.
func itemID(it item) string {
	key := it.ID
	if key == "" {
		key = it.Link
	}
	if key == "" {
		key = it.Title + "|" + it.Published.String()
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func (feedConn) Read(ctx context.Context, in connectors.Instance, tool string, args map[string]string) (connectors.Result, error) {
	f, _, _, err := fetch(ctx, in.HTTP, in.Config["url"], cursor{})
	if err != nil {
		return connectors.Result{}, err
	}
	from := "the feed " + in.Name
	switch tool {
	case "items":
		limit := 20
		if v := args["limit"]; v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return connectors.Result{}, connectors.ErrBadArgs
			}
			limit = min(n, 100)
		}
		var b strings.Builder
		n := 0
		for _, it := range f.Items {
			if n == limit {
				break
			}
			n++
			fmt.Fprintf(&b, "- %s\n  id=%s", connections.Clip(it.Title, 200), itemID(it))
			if !it.Published.IsZero() {
				fmt.Fprintf(&b, " · %s", it.Published.UTC().Format("2006-01-02 15:04 UTC"))
			}
			if it.Link != "" {
				fmt.Fprintf(&b, " · %s", it.Link)
			}
			b.WriteString("\n")
		}
		return connectors.Result{Text: fmt.Sprintf("%d of %d items; read one with: item id=<id>", n, len(f.Items)), Data: b.String(), From: from}, nil
	case "item":
		id := args["id"]
		if id == "" {
			return connectors.Result{}, connectors.ErrBadArgs
		}
		for _, it := range f.Items {
			if itemID(it) != id && it.ID != id {
				continue
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Title: %s\n", it.Title)
			if it.Author != "" {
				fmt.Fprintf(&b, "Author: %s\n", it.Author)
			}
			if !it.Published.IsZero() {
				fmt.Fprintf(&b, "Published: %s\n", it.Published.UTC().Format(time.RFC3339))
			}
			if it.Link != "" {
				fmt.Fprintf(&b, "Link: %s\n", it.Link)
			}
			b.WriteString("\n" + connections.Clip(it.Text, 20000))
			return connectors.Result{Data: b.String(), From: from}, nil
		}
		return connectors.Result{}, connectors.Sayf("No item %s in the feed now: it may have left it", id)
	}
	return connectors.Result{}, connectors.ErrUnknownTool
}

// The formats as encoding/xml reads them: a tag without a namespace matches any, so Atom's and RSS's both fit.
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Date        string `xml:"http://purl.org/dc/elements/1.1/ date"`
	Author      string `xml:"author"`
	Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Description string `xml:"description"`
	Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type atomEntry struct {
	Title     string     `xml:"title"`
	ID        string     `xml:"id"`
	Links     []atomLink `xml:"link"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Summary string `xml:"summary"`
	Content string `xml:"content"`
}

type document struct {
	XMLName xml.Name
	// RSS 2.0: <rss><channel>…; RSS 1.0: <rdf:RDF><channel/><item/>…
	Channel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
	RDFItems []rssItem `xml:"item"`
	// Atom: <feed><title/><entry/>…
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

func parse(body []byte) (*feed, error) {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	dec.Strict = false
	dec.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		enc, err := htmlindex.Get(label)
		if err != nil {
			return nil, err
		}
		return enc.NewDecoder().Reader(input), nil
	}
	var d document
	if err := dec.Decode(&d); err != nil {
		return nil, connectors.Sayf("This address is not an RSS or Atom feed")
	}
	f := &feed{}
	switch strings.ToLower(d.XMLName.Local) {
	case "rss", "rdf":
		f.Title = d.Channel.Title
		items := d.Channel.Items
		if len(items) == 0 {
			items = d.RDFItems
		}
		for _, it := range items {
			text := it.Content
			if text == "" {
				text = it.Description
			}
			author := it.Creator
			if author == "" {
				author = it.Author
			}
			date := it.PubDate
			if date == "" {
				date = it.Date
			}
			f.Items = append(f.Items, item{ID: strings.TrimSpace(it.GUID), Title: clean(it.Title), Link: strings.TrimSpace(it.Link),
				Author: clean(author), Published: parseDate(date), Text: connections.HTMLText(text)})
		}
	case "feed":
		f.Title = d.Title
		for _, e := range d.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			text := e.Content
			if text == "" {
				text = e.Summary
			}
			date := e.Published
			if date == "" {
				date = e.Updated
			}
			f.Items = append(f.Items, item{ID: strings.TrimSpace(e.ID), Title: clean(e.Title), Link: strings.TrimSpace(link),
				Author: clean(e.Author.Name), Published: parseDate(date), Text: connections.HTMLText(text)})
		}
	default:
		return nil, connectors.Sayf("This address is not an RSS or Atom feed")
	}
	f.Title = clean(f.Title)
	return f, nil
}

func clean(s string) string { return strings.Join(strings.Fields(connections.HTMLText(s)), " ") }

var dateLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC3339Nano, "Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST", "2 Jan 2006 15:04:05 -0700", "2006-01-02T15:04:05Z07:00", "2006-01-02"}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
