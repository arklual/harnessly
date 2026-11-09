package host

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// sessionCookiesFile keeps the session cookies of a browser closed on purpose (idle, an update, a stop) until the next
// one starts: Chrome drops them on a normal exit, and a sign-in that lives in one would be lost to every shutdown.
const sessionCookiesFile = "session-cookies.json"

// cookieParams are the fields of a Cookie that Storage.setCookies takes back.
var cookieParams = []string{"name", "value", "path", "secure", "httpOnly", "sameSite", "priority", "sourceScheme", "sourcePort"}

// sessionKeeper saves the session cookies once per browser.
type sessionKeeper struct {
	dir   string
	relay *relay
	mu    sync.Mutex
	saved bool
}

func (k *sessionKeeper) save(ctx context.Context) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.saved {
		return
	}
	k.saved = true
	raw, err := k.relay.call(ctx, "Storage.getCookies", nil)
	if err != nil {
		return
	}
	var got struct {
		Cookies []map[string]json.RawMessage `json:"cookies"`
	}
	if json.Unmarshal(raw, &got) != nil {
		return
	}
	keep := []map[string]json.RawMessage{}
	for _, c := range got.Cookies {
		var session bool
		_ = json.Unmarshal(c["session"], &session)
		// A partitioned cookie cannot be set back to its partition: it stays lost, as it would be anyway.
		if !session || c["partitionKey"] != nil {
			continue
		}
		var domain string
		var secure bool
		_ = json.Unmarshal(c["domain"], &domain)
		_ = json.Unmarshal(c["secure"], &secure)
		if domain == "" {
			continue
		}
		p := map[string]json.RawMessage{}
		for _, f := range cookieParams {
			if v, ok := c[f]; ok {
				p[f] = v
			}
		}
		if strings.HasPrefix(domain, ".") {
			p["domain"] = c["domain"]
		} else {
			// A host-only cookie set by domain would become a domain cookie: it is set back by its address.
			scheme := "http"
			if secure {
				scheme = "https"
			}
			path := "/"
			_ = json.Unmarshal(c["path"], &path)
			u, _ := json.Marshal(scheme + "://" + domain + path)
			p["url"] = u
		}
		keep = append(keep, p)
	}
	file := filepath.Join(k.dir, sessionCookiesFile)
	if len(keep) == 0 {
		_ = os.Remove(file)
		return
	}
	out, err := json.Marshal(keep)
	if err != nil {
		return
	}
	tmp := file + ".tmp"
	if os.WriteFile(tmp, out, 0600) == nil {
		_ = os.Rename(tmp, file)
	}
}

// restore sets back what save kept and forgets it: a crash after this start loses the cookies, not twice.
func (k *sessionKeeper) restore(ctx context.Context) error {
	file := filepath.Join(k.dir, sessionCookiesFile)
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	_ = os.Remove(file)
	var cookies []json.RawMessage
	if json.Unmarshal(raw, &cookies) != nil || len(cookies) == 0 {
		return nil
	}
	_, err = k.relay.call(ctx, "Storage.setCookies", map[string]any{"cookies": cookies})
	return err
}

// DefaultDownloadLimit is the largest file a page may download unless the runtime says otherwise.
const DefaultDownloadLimit = 100 << 20

// downloadsTotal is how many files of the limit the downloads folder holds, the downloads under way included.
const downloadsTotal = 5

// downloadsAtOnce is how many downloads may be under way at once.
const downloadsAtOnce = 3

var downloadGUID = regexp.MustCompile(`^[0-9A-Za-z-]{1,64}$`)

// downloads keeps a page's downloads in the profile's downloads/ (never the person's ~/Downloads), each file up to
// limit bytes and the folder up to downloadsTotal of them; 0 refuses every download.
type downloads struct {
	dir   string
	limit int64
	relay *relay

	mu     sync.Mutex
	active map[string]bool
}

func (d *downloads) setup(ctx context.Context) error {
	if d.limit <= 0 {
		_, err := d.relay.call(ctx, "Browser.setDownloadBehavior", map[string]any{"behavior": "deny"})
		return err
	}
	if err := PrivateDir(d.dir); err != nil {
		return err
	}
	_, err := d.relay.call(ctx, "Browser.setDownloadBehavior", map[string]any{
		"behavior": "allowAndName", "downloadPath": d.dir, "eventsEnabled": true})
	return err
}

func (d *downloads) event(method string, params json.RawMessage) {
	var p struct {
		GUID     string  `json:"guid"`
		Total    float64 `json:"totalBytes"`
		Received float64 `json:"receivedBytes"`
		State    string  `json:"state"`
	}
	if json.Unmarshal(params, &p) != nil || !downloadGUID.MatchString(p.GUID) {
		return
	}
	d.mu.Lock()
	if d.active == nil {
		d.active = map[string]bool{}
	}
	crowded := false
	switch {
	case method == "Browser.downloadWillBegin":
		crowded = len(d.active) >= downloadsAtOnce
		if !crowded {
			d.active[p.GUID] = true
		}
	case p.State == "completed" || p.State == "canceled":
		delete(d.active, p.GUID)
	}
	d.mu.Unlock()
	switch {
	case crowded:
		d.cancel(p.GUID)
	case method == "Browser.downloadWillBegin" && d.used() >= d.limit*downloadsTotal:
		d.cancel(p.GUID)
	case method == "Browser.downloadProgress" && p.State == "canceled":
		_ = os.Remove(filepath.Join(d.dir, p.GUID))
	case method == "Browser.downloadProgress" && (p.Total > float64(d.limit) || p.Received > float64(d.limit)):
		d.cancel(p.GUID)
	// The files under way are on disk as they grow: together they stay within the folder's bound.
	case method == "Browser.downloadProgress" && p.State == "inProgress" && d.used() > d.limit*downloadsTotal:
		d.cancel(p.GUID)
	}
}

func (d *downloads) cancel(guid string) {
	d.mu.Lock()
	delete(d.active, guid)
	d.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = d.relay.call(ctx, "Browser.cancelDownload", map[string]any{"guid": guid})
	_ = os.Remove(filepath.Join(d.dir, guid))
	_ = os.Remove(filepath.Join(d.dir, guid+".crdownload"))
}

func (d *downloads) used() int64 {
	var total int64
	_ = filepath.WalkDir(d.dir, func(_ string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if info, e := e.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
