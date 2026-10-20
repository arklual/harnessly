package share

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The owner's drafts and published links, under <data>/shares, in the previous runtime's formats:
//
//	shares/            0700
//	  drafts/<id>.json  0600  a captured, redacted snapshot; one hour
//	  index.json        0600  {schema, records}
//	  links/<id>.json   0600  a published snapshot; deleted on revoke and on expiry
//	  views.jsonl       0600  {id6, viewer, at}
//
// A revoked or expired link keeps its record (a tombstone) so the owner sees what happened; its content is gone. Only
// the first six characters of an id ever reach a log line.

const (
	draftTTL       = 60 * 60 * 1000.0
	dayMs          = 24 * 60 * 60 * 1000.0
	maxLifetime    = 90 * dayMs
	tombstoneKeep  = 30 * dayMs
	viewersKept    = 20
	maxTranscript  = 256 * 1024 * 1024
	statusLive     = "published"
	statusRevoked  = "revoked"
	statusExpired  = "expired"
	errGone        = "Ссылка отозвана или истекла"
	errLinkMissing = "Ссылка не найдена"
)

// ShareIDRe is a draft's or a link's id: 32 random bytes in base64url.
var ShareIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func newShareID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Store is the drafts and the links of this runtime.
type Store struct {
	DataDir string
	// Now is the clock (tests freeze it); nil is the real one.
	Now func() time.Time
	// NewID makes the id of a draft or a link (tests count); nil is 32 random bytes.
	NewID func() string

	// mu: every read-modify-write of the index is one at a time; views and owner actions interleave.
	mu sync.Mutex
}

func (s *Store) now() float64 {
	t := time.Now()
	if s.Now != nil {
		t = s.Now()
	}
	return float64(t.UnixMilli())
}

func (s *Store) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return newShareID()
}

func (s *Store) sharesDir() string            { return filepath.Join(s.DataDir, "shares") }
func (s *Store) draftsDir() string            { return filepath.Join(s.sharesDir(), "drafts") }
func (s *Store) linksDir() string             { return filepath.Join(s.sharesDir(), "links") }
func (s *Store) indexFile() string            { return filepath.Join(s.sharesDir(), "index.json") }
func (s *Store) viewsFile() string            { return filepath.Join(s.sharesDir(), "views.jsonl") }
func (s *Store) contentFile(id string) string { return filepath.Join(s.linksDir(), id+".json") }
func logID(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// --- drafts ----------------------------------------------------------------------------------------------------------

// SaveDraft writes a new draft of a snapshot and returns it.
func (s *Store) SaveDraft(tabID string, snapshot, report *jsvalue.Object) (*jsvalue.Object, error) {
	now := s.now()
	d := jsvalue.NewObject("schema", 1, "id", s.newID(), "tabId", tabID, "createdAt", now, "expiresAt", now+draftTTL,
		"snapshot", snapshot, "report", report)
	if err := os.MkdirAll(s.draftsDir(), 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(s.sharesDir(), 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(s.draftsDir(), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(s.draftsDir(), d.String("id")+".json"), jsvalue.Stringify(d, ""), 0o600); err != nil {
		return nil, err
	}
	return d, nil
}

// DraftView is what the owner's page gets of a draft.
func DraftView(d *jsvalue.Object) *jsvalue.Object {
	return jsvalue.NewObject("id", get(d, "id"), "title", get(d.Object("snapshot"), "title"), "createdAt", get(d, "createdAt"),
		"expiresAt", get(d, "expiresAt"), "report", get(d, "report"))
}

// ReadDraft is a live draft; nil when the id is malformed, unknown or expired (an expired file is removed on the spot).
// An error is a draft file that is not JSON (the previous runtime failed the request).
func (s *Store) ReadDraft(id string) (*jsvalue.Object, error) {
	if !ShareIDRe.MatchString(id) {
		return nil, nil
	}
	file := filepath.Join(s.draftsDir(), id+".json")
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, nil
	}
	v, err := jsvalue.Parse([]byte(decodeFile(raw)))
	if err != nil {
		return nil, err
	}
	d, ok := v.(*jsvalue.Object)
	if !ok {
		return nil, errors.New("draft is not an object")
	}
	if exp, ok := d.Number("expiresAt"); ok && exp <= s.now() {
		_ = os.Remove(file)
		return nil, nil
	}
	return d, nil
}

// RemoveDraft drops a published draft: its snapshot now lives in the store.
func (s *Store) RemoveDraft(id string) {
	if ShareIDRe.MatchString(id) {
		_ = os.Remove(filepath.Join(s.draftsDir(), id+".json"))
	}
}

// SweepDrafts removes the drafts written more than an hour ago.
func (s *Store) SweepDrafts() {
	entries, err := os.ReadDir(s.draftsDir())
	if err != nil {
		return
	}
	now := s.now()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		file := filepath.Join(s.draftsDir(), e.Name())
		st, err := os.Stat(file)
		if err == nil && float64(st.ModTime().UnixNano())/1e6+draftTTL <= now {
			_ = os.RemoveAll(file)
		}
	}
}

// --- links -----------------------------------------------------------------------------------------------------------

// isNull: the field is present and null (what `=== null` sees).
func isNull(o *jsvalue.Object, k string) bool {
	v, ok := o.Get(k)
	return ok && v == nil
}

// number is a field as JavaScript arithmetic sees it: NaN for anything but a number (null is 0).
func number(o *jsvalue.Object, k string) float64 {
	v, ok := o.Get(k)
	if !ok {
		return math.NaN()
	}
	switch x := v.(type) {
	case float64:
		return x
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	}
	return math.NaN()
}

func statusOf(r *jsvalue.Object, now float64) string {
	if !isNull(r, "revokedAt") {
		return statusRevoked
	}
	if !isNull(r, "expiredAt") || number(r, "expiresAt") <= now {
		return statusExpired
	}
	return statusLive
}

// ShareView is what the owner's page gets of a link.
func ShareView(r *jsvalue.Object, now float64) *jsvalue.Object {
	status := statusOf(r, now)
	var url any
	if status == statusLive {
		url = jsTemplate(get(r, "routeBase")) + jsTemplate(get(r, "id"))
	}
	return jsvalue.NewObject("id", get(r, "id"), "url", url, "status", status, "tabId", get(r, "tabId"), "title", get(r, "title"),
		"version", get(r, "version"), "publishedAt", get(r, "publishedAt"), "updatedAt", get(r, "updatedAt"), "expiresAt", get(r, "expiresAt"),
		"maxExpiresAt", jsNumber(number(r, "publishedAt")+maxLifetime), "revokedAt", get(r, "revokedAt"), "views", get(r, "views"),
		"viewers", get(r, "viewers"), "report", get(r, "report"))
}

// jsNumber is a computed number as JSON.stringify writes it: NaN and infinities as null.
func jsNumber(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return f
}

func jsTemplate(v any) string { return template(v) }

func (s *Store) ensureDirs() error {
	if err := os.MkdirAll(s.linksDir(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.sharesDir(), 0o700); err != nil {
		return err
	}
	return os.Chmod(s.linksDir(), 0o700)
}

func (s *Store) readIndex() (*jsvalue.Object, error) {
	raw, err := os.ReadFile(s.indexFile())
	if errors.Is(err, fs.ErrNotExist) {
		return jsvalue.NewObject("schema", 1, "records", []any{}), nil
	}
	if err != nil {
		return nil, err
	}
	v, err := jsvalue.Parse([]byte(decodeFile(raw)))
	if err != nil {
		return nil, err
	}
	idx, ok := v.(*jsvalue.Object)
	if !ok || get(idx, "schema") != 1.0 {
		return nil, errors.New("unsupported share index")
	}
	if _, isArr := get(idx, "records").([]any); !isArr {
		return nil, errors.New("unsupported share index")
	}
	return idx, nil
}

func records(idx *jsvalue.Object) []*jsvalue.Object {
	var out []*jsvalue.Object
	for _, r := range idx.Array("records") {
		if o, ok := r.(*jsvalue.Object); ok {
			out = append(out, o)
		}
	}
	return out
}

// writePrivate replaces a file atomically: a crash mid-write leaves the previous one, never half of one.
func (s *Store) writePrivate(file string, data []byte) error {
	tmp := file + "." + strconv.Itoa(os.Getpid()) + "." + strconv.FormatInt(int64(s.now()), 10) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

func (s *Store) writeIndex(idx *jsvalue.Object) error {
	return s.writePrivate(s.indexFile(), jsvalue.Stringify(idx, ""))
}

// sweep takes the content of expired links and drops old tombstones; it says whether the index changed.
func (s *Store) sweep(idx *jsvalue.Object, now float64) bool {
	changed := false
	for _, r := range records(idx) {
		if isNull(r, "revokedAt") && isNull(r, "expiredAt") && number(r, "expiresAt") <= now {
			r.Set("expiredAt", now)
			_ = os.Remove(s.contentFile(jsTemplate(get(r, "id"))))
			changed = true
		}
	}
	all := idx.Array("records")
	kept := make([]any, 0, len(all))
	for _, rv := range all {
		r, ok := rv.(*jsvalue.Object)
		if !ok {
			continue // a non-object record has no fields: its end is undefined and it is dropped
		}
		key := "revokedAt"
		if v, present := r.Get("revokedAt"); !present || v == nil {
			key = "expiredAt"
		}
		if isNull(r, key) || number(r, key)+tombstoneKeep > now {
			kept = append(kept, r)
		}
	}
	if len(kept) != len(all) {
		idx.Set("records", kept)
		changed = true
	}
	return changed
}

func expiryFrom(now, days, publishedAt float64) float64 {
	return math.Min(now+days*dayMs, publishedAt+maxLifetime)
}

// IsExpiryDays: 7, 30 or 90, as a number.
func IsExpiryDays(v any) bool {
	f, ok := v.(float64)
	return ok && (f == 7 || f == 30 || f == 90)
}

// Mutation is a change to a link: the link's view, or a refusal's status and message.
type Mutation struct {
	Share  *jsvalue.Object
	Status int
	Error  string
}

func findRecord(idx *jsvalue.Object, id string) *jsvalue.Object {
	for _, r := range records(idx) {
		if v, ok := r.Get("id"); ok && v == id {
			return r
		}
	}
	return nil
}

// Publish turns a draft into a frozen link, or with replaceID puts it behind an existing live link (same URL, version
// + 1). The draft's snapshot is stored as it is: it was redacted at capture and is redacted again on every read.
func (s *Store) Publish(draft *jsvalue.Object, routeBase string, days float64, replaceID string) (Mutation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if err := s.ensureDirs(); err != nil {
		return Mutation{}, err
	}
	idx, err := s.readIndex()
	if err != nil {
		return Mutation{}, err
	}
	s.sweep(idx, now)
	snapshot := draft.Object("snapshot")
	var record *jsvalue.Object
	if replaceID != "" {
		existing := findRecord(idx, replaceID)
		if existing == nil {
			return Mutation{Status: 404, Error: errLinkMissing}, nil
		}
		if statusOf(existing, now) != statusLive {
			return Mutation{Status: 410, Error: errGone}, nil
		}
		if !jsvalue.SameValue(get(existing, "tabId"), get(draft, "tabId")) {
			return Mutation{Status: 409, Error: "Ссылка открыта для другого диалога"}, nil
		}
		existing.Set("version", jsNumber(number(existing, "version")+1))
		existing.Set("updatedAt", now)
		existing.Set("title", get(snapshot, "title"))
		existing.Set("report", get(draft, "report"))
		existing.Set("expiresAt", jsNumber(expiryFrom(now, days, number(existing, "publishedAt"))))
		record = existing
	} else {
		record = jsvalue.NewObject("id", s.newID(), "tabId", get(draft, "tabId"), "title", get(snapshot, "title"),
			"ownerSub", get(snapshot.Object("owner"), "sub"), "routeBase", routeBase, "version", 1, "publishedAt", now, "updatedAt", now,
			"expiresAt", expiryFrom(now, days, now), "revokedAt", nil, "expiredAt", nil, "views", 0, "viewers", []any{}, "report", get(draft, "report"))
		idx.Set("records", append(idx.Array("records"), record))
	}
	id := record.String("id")
	if err := s.writePrivate(s.contentFile(id), jsvalue.Stringify(snapshot, "")); err != nil {
		return Mutation{}, err
	}
	if err := s.writeIndex(idx); err != nil {
		return Mutation{}, err
	}
	agent.Log("info", "[share] published "+logID(id)+" v"+template(get(record, "version")))
	return Mutation{Share: ShareView(record, now)}, nil
}

// List is the owner's links, the latest change first; with a tab id only that conversation's.
func (s *Store) List(tabID *string) ([]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	idx, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	if s.sweep(idx, now) {
		if err := s.ensureDirs(); err != nil {
			return nil, err
		}
		if err := s.writeIndex(idx); err != nil {
			return nil, err
		}
	}
	var picked []*jsvalue.Object
	for _, r := range records(idx) {
		if tabID == nil || get(r, "tabId") == *tabID {
			picked = append(picked, r)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		d := number(picked[j], "updatedAt") - number(picked[i], "updatedAt")
		return d < 0 // a NaN difference keeps the order, as a comparator returning NaN does
	})
	out := make([]any, len(picked))
	for i, r := range picked {
		out[i] = ShareView(r, now)
	}
	return out, nil
}

// Extend sets a new expiry counted from now, capped at 90 days after the first publication.
func (s *Store) Extend(id string, days float64) (Mutation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	idx, err := s.readIndex()
	if err != nil {
		return Mutation{}, err
	}
	s.sweep(idx, now)
	r := findRecord(idx, id)
	if r == nil {
		return Mutation{Status: 404, Error: errLinkMissing}, nil
	}
	if statusOf(r, now) != statusLive {
		if err := s.writeIndex(idx); err != nil {
			return Mutation{}, err
		}
		return Mutation{Status: 410, Error: errGone}, nil
	}
	r.Set("expiresAt", jsNumber(expiryFrom(now, days, number(r, "publishedAt"))))
	if err := s.writeIndex(idx); err != nil {
		return Mutation{}, err
	}
	return Mutation{Share: ShareView(r, now)}, nil
}

// Revoke removes a link's content at once; the next read answers 410. Revoking twice is fine.
func (s *Store) Revoke(id string) (Mutation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	idx, err := s.readIndex()
	if err != nil {
		return Mutation{}, err
	}
	s.sweep(idx, now)
	r := findRecord(idx, id)
	if r == nil {
		return Mutation{Status: 404, Error: errLinkMissing}, nil
	}
	_ = os.Remove(s.contentFile(id))
	if isNull(r, "revokedAt") {
		r.Set("revokedAt", now)
	}
	if err := s.writeIndex(idx); err != nil {
		return Mutation{}, err
	}
	agent.Log("info", "[share] revoked "+logID(id))
	return Mutation{Share: ShareView(r, now)}, nil
}

// ReadShared is what a viewer gets: the snapshot of a live link (and its record), or 410 for a revoked or expired one,
// 404 for anything else. It counts the view.
func (s *Store) ReadShared(id, viewer string) (int, *jsvalue.Object, *jsvalue.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ShareIDRe.MatchString(id) {
		return 404, nil, nil, nil
	}
	now := s.now()
	idx, err := s.readIndex()
	if err != nil {
		return 0, nil, nil, err
	}
	swept := s.sweep(idx, now)
	r := findRecord(idx, id)
	if r == nil || statusOf(r, now) != statusLive {
		if swept {
			if err := s.writeIndex(idx); err != nil {
				return 0, nil, nil, err
			}
		}
		if r == nil {
			return 404, nil, nil, nil
		}
		return 410, nil, nil, nil
	}
	raw, err := os.ReadFile(s.contentFile(id))
	if err != nil {
		return 410, nil, nil, nil
	}
	r.Set("views", jsNumber(number(r, "views")+1))
	viewers := []any{jsvalue.NewObject("sub", viewer, "at", now)}
	for _, v := range r.Array("viewers") {
		if vo, ok := v.(*jsvalue.Object); ok && get(vo, "sub") == viewer {
			continue
		}
		viewers = append(viewers, v)
	}
	if len(viewers) > viewersKept {
		viewers = viewers[:viewersKept]
	}
	r.Set("viewers", viewers)
	if err := s.writeIndex(idx); err != nil {
		return 0, nil, nil, err
	}
	line := jsvalue.Stringify(jsvalue.NewObject("id6", logID(id), "viewer", viewer, "at", now), "")
	f, err := os.OpenFile(s.viewsFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, nil, nil, err
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return 0, nil, nil, errors.Join(werr, cerr)
	}
	v, err := jsvalue.Parse([]byte(decodeFile(raw)))
	if err != nil {
		return 0, nil, nil, err
	}
	snap, _ := v.(*jsvalue.Object)
	if snap == nil {
		snap = jsvalue.NewObject()
	}
	return 200, r, snap, nil
}
