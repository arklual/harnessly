package webhook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// What a webhook keeps on the machine, under the connection's folder: index.jsonl, a line per request taken (when, its
// key, the kinds, title and link), and bodies/<key>.json with the body and the headers that say what it is (never a
// signature or a token). The newest keepMax requests stay, none older than keepFor. The hub keeps none of it.

const (
	keepMax = 200
	keepFor = 30 * 24 * time.Hour
	// replayWindow: a request whose key was taken this recently is a replay.
	replayWindow = 72 * time.Hour
)

// keptHeaders are the headers kept with a body: what kind of request it is, never how it was signed.
var keptHeaders = []string{"Content-Type", "User-Agent", "X-GitHub-Event", "X-GitHub-Delivery", "X-Gitlab-Event",
	"X-Gitlab-Event-UUID", "Sentry-Hook-Resource", "Request-ID", "X-Request-Id"}

var (
	storeMu sync.Mutex
	keyRe   = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type record struct {
	Key   string    `json:"key"`
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`
	Title string    `json:"title,omitempty"`
	URL   string    `json:"url,omitempty"`
}

type body struct {
	record
	Header map[string]string `json:"header"`
	Body   json.RawMessage   `json:"body"`
}

type store struct{ dir string }

func (st store) index() string            { return filepath.Join(st.dir, "index.jsonl") }
func (st store) bodyFile(k string) string { return filepath.Join(st.dir, "bodies", k+".json") }

func (st store) read() []record {
	f, err := os.Open(st.index())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var r record
		if json.Unmarshal(sc.Bytes(), &r) == nil && keyRe.MatchString(r.Key) {
			out = append(out, r)
		}
	}
	return out
}

func (st store) write(list []record) error {
	var b bytes.Buffer
	for _, r := range list {
		raw, _ := json.Marshal(r)
		b.Write(raw)
		b.WriteByte('\n')
	}
	tmp := st.index() + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.index())
}

// keep takes a verified request once: a key taken within the replay window is ErrDuplicate. It keeps the body, then
// answers the events of its items.
func (st store) keep(key string, now time.Time, req connectors.Incoming, items []item) ([]connectors.Event, error) {
	if st.dir == "" {
		return nil, errors.New("webhook: no folder for the connection")
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	list := st.read()
	for _, r := range list {
		if r.Key == key && now.Sub(r.At) < replayWindow {
			return nil, connectors.ErrDuplicate
		}
	}
	if err := os.MkdirAll(filepath.Join(st.dir, "bodies"), 0o700); err != nil {
		return nil, err
	}
	rec := record{Key: key, At: now.UTC(), Kind: "ping"}
	if len(items) > 0 {
		rec.Kind, rec.Title, rec.URL = items[0].Kind, items[0].Title, items[0].URL
		if len(items) > 1 {
			rec.Kind += " +" + strconv.Itoa(len(items)-1)
		}
	}
	h := map[string]string{}
	for _, name := range keptHeaders {
		if v := req.Header.Get(name); v != "" {
			h[name] = v
		}
	}
	raw, _ := json.Marshal(body{record: rec, Header: h, Body: json.RawMessage(req.Body)})
	if err := os.WriteFile(st.bodyFile(key), raw, 0o600); err != nil {
		return nil, err
	}
	list = append(list, rec)
	var drop []record
	cut := 0
	for cut < len(list) && (len(list)-cut > keepMax || now.Sub(list[cut].At) > keepFor) {
		drop = append(drop, list[cut])
		cut++
	}
	if err := st.write(list[cut:]); err != nil {
		_ = os.Remove(st.bodyFile(key))
		return nil, err
	}
	for _, r := range drop {
		_ = os.Remove(st.bodyFile(r.Key))
	}
	var out []connectors.Event
	for _, it := range items {
		id := key
		if it.Sub != "" {
			id = key + ":" + it.Sub
		}
		f := maps.Clone(it.Facts)
		if f == nil {
			f = map[string]string{}
		}
		f["kind"] = it.Kind
		out = append(out, connectors.Event{Type: EventReceived, ID: id, At: now, Actor: it.Actor, Title: it.Title,
			URL: it.URL, Thread: it.Thread, Ref: "event id=" + key, Facts: f})
	}
	return out, nil
}

// list is the newest requests first.
func (st store) list(limit int) ([]record, error) {
	storeMu.Lock()
	list := st.read()
	storeMu.Unlock()
	slices.Reverse(list)
	if len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func (st store) get(key string) (body, error) {
	if !keyRe.MatchString(key) {
		return body{}, connectors.ErrNotFound
	}
	raw, err := os.ReadFile(st.bodyFile(key))
	if err != nil {
		return body{}, connectors.ErrNotFound
	}
	var b body
	if err := json.Unmarshal(raw, &b); err != nil {
		return body{}, err
	}
	return b, nil
}

func sortedKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }
