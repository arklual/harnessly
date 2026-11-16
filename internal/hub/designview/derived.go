package designview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/blobserve"
)

// HRN-1320, stage 2 of the read layer. An answer about one revision of a project (a board's page, the list of its
// boards, a file of it) is the same whoever asks and whichever replica answers: the revision does not change. So the
// answer is worked out once, by the code that always did (design.API over the revision's folder), kept in the store
// as a blob and named in the hub's database; from then on every replica gives it straight from the store, without
// putting the project into a folder of its own. A hub that has just started reads one row and one blob for the first
// guest of a link, not the whole project; and since the blob's hash is its validator, a board's page answers 304 too.
//
// What is kept is only what the revision decides: a GET that answered 200, whole, under a size. A request for a range
// that is not kept yet, and an answer too big to keep, go the old way. A kept answer whose blob the store lost is
// worked out again.

// Derived keeps the answers worked out from a revision. Lookup and Keep are the hub's database, Open and Put the store.
type Derived interface {
	Lookup(ctx context.Context, key string) (DerivedAnswer, bool)
	Keep(ctx context.Context, a DerivedAnswer, body []byte) error
	Open(ctx context.Context, spaceID, sha string) (io.ReadCloser, error)
}

// DerivedAnswer is one kept answer: the blob and the headers it is given with.
type DerivedAnswer struct {
	Key, SpaceID, Project string
	Rev                   int64
	Sha                   string
	Size                  int64
	// Type is the Content-Type; Immutable says the address named its contents (kept by the browser for good).
	Type      string
	Immutable bool
	// Headers are the answer's own headers that say what was served (the revision, the pages' limit).
	Headers map[string]string
}

// maxDerived is the biggest answer kept; a bigger one (a video, an archive) is given the old way each time.
const maxDerived = 16 << 20

// keptHeaders are the headers of an answer that travel with it.
var keptHeaders = []string{"X-Design-Rev", "X-Design-Pages", "Content-Security-Policy", "Content-Encoding", "Vary", "Access-Control-Allow-Origin"}

// UseDerived lets the view keep the answers about a revision in the store; build names the code that works them out
// (an answer of another build is not this build's).
func (v *View) UseDerived(d Derived, build string) {
	v.mu.Lock()
	v.derived, v.build = d, build
	v.mu.Unlock()
}

func (v *View) derivedStore() (Derived, string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.derived, v.build
}

// derivedKey names an answer by everything that decides it.
func derivedKey(build, spaceID, project string, at int64, segs, pages []string, pinned bool, r *http.Request) string {
	q := r.URL.Query()
	names := make([]string, 0, len(q))
	for k := range q {
		names = append(names, k)
	}
	sort.Strings(names)
	h := sha256.New()
	fmt.Fprintf(h, "v1\x00%s\x00%s\x00%s\x00%d\x00%t\x00", build, spaceID, project, at, pinned)
	for _, s := range segs {
		fmt.Fprintf(h, "s:%s\x00", s)
	}
	if pages == nil {
		h.Write([]byte("pages:all\x00"))
	} else {
		sorted := append([]string(nil), pages...)
		sort.Strings(sorted)
		for _, p := range sorted {
			fmt.Fprintf(h, "p:%s\x00", p)
		}
	}
	for _, k := range names {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		fmt.Fprintf(h, "q:%s=%s\x00", k, strings.Join(vals, "\x01"))
	}
	// A compressed answer is another answer.
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Write([]byte("gzip\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// serveDerived answers from the kept answers, or works the answer out by work and keeps it; false when the request
// is not one whose answer is kept (work was not called: the caller goes the old way).
func (v *View) serveDerived(w http.ResponseWriter, r *http.Request, spaceID, project string, at int64, segs, pages []string, pinned bool,
	work func(w http.ResponseWriter, r *http.Request) error) (bool, error) {
	d, build := v.derivedStore()
	if d == nil || r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false, nil
	}
	key := derivedKey(build, spaceID, project, at, segs, pages, pinned, r)
	if a, ok := d.Lookup(r.Context(), key); ok {
		for k, val := range a.Headers {
			w.Header().Set(k, val)
		}
		policy := blobserve.Policy{Type: a.Type, Cache: blobserve.Revalidate}
		if a.Immutable {
			policy.Cache = blobserve.Immutable
		}
		err := blobserve.Serve(w, r, blobserve.Blob{Sha: a.Sha, Open: func(ctx context.Context) (io.ReadCloser, error) {
			return d.Open(ctx, spaceID, a.Sha)
		}}, policy)
		if err == nil {
			return true, nil
		}
		for k := range a.Headers {
			w.Header().Del(k)
		}
		if !errors.Is(err, blobserve.ErrNotFound) {
			return true, err
		}
		// The store lost the blob: the answer is worked out again below.
	}
	if r.Method == http.MethodHead || r.Header.Get("Range") != "" {
		return false, nil
	}
	// The answer is worked out whole: what the browser holds already does not decide what is kept.
	inner := r.Clone(r.Context())
	for _, h := range []string{"If-None-Match", "If-Modified-Since", "If-Range", "If-Match", "If-Unmodified-Since"} {
		inner.Header.Del(h)
	}
	// The answer is held until it is whole: one that is kept goes out with the validator the kept copy will have (the
	// blob's hash), so a browser that got it worked out and asks again with its ETag is answered 304 from the store
	// instead of being sent it again under another validator. A bigger one goes on as it is written.
	tee := &keepWriter{w: w, max: maxDerived}
	if err := work(tee, inner); err != nil {
		if !tee.passing && (tee.status != 0 || tee.body.Len() > 0) {
			tee.pass()
		}
		return true, err
	}
	keep := !tee.passing && tee.status == http.StatusOK && tee.body.Len() > 0
	var sha string
	if keep {
		sum := sha256.Sum256(tee.body.Bytes())
		sha = hex.EncodeToString(sum[:])
		w.Header().Set("ETag", blobserve.ETag(sha))
		if blobserve.Holds(r, sha) {
			w.WriteHeader(http.StatusNotModified)
		} else {
			tee.pass()
		}
	} else if !tee.passing {
		tee.pass()
	}
	if keep {
		a := DerivedAnswer{Key: key, SpaceID: spaceID, Project: project, Rev: at, Sha: sha, Size: int64(tee.body.Len()),
			Type: w.Header().Get("Content-Type"), Immutable: strings.Contains(w.Header().Get("Cache-Control"), "immutable"), Headers: map[string]string{}}
		for _, k := range keptHeaders {
			if val := w.Header().Get(k); val != "" {
				a.Headers[k] = val
			}
		}
		if err := d.Keep(context.WithoutCancel(r.Context()), a, tee.body.Bytes()); err != nil {
			v.o.Log.Warn("design view: an answer was not kept", "space", spaceID, "project", project, "rev", at, "error", err)
		}
	}
	return true, nil
}

// keepWriter holds an answer whole, up to max bytes, so it can be kept and given with its hash as the validator; an
// answer that is not a 200 or grows over max is sent on as it is written from then on (pass).
type keepWriter struct {
	w       http.ResponseWriter
	status  int
	over    bool
	passing bool
	max     int
	body    bytes.Buffer
}

func (k *keepWriter) Header() http.Header { return k.w.Header() }

func (k *keepWriter) WriteHeader(code int) {
	if k.status != 0 {
		return
	}
	k.status = code
	if code != http.StatusOK {
		k.pass()
	}
}

func (k *keepWriter) Write(b []byte) (int, error) {
	if k.status == 0 {
		k.status = http.StatusOK
	}
	if k.passing {
		return k.w.Write(b)
	}
	if k.body.Len()+len(b) > k.max {
		k.over = true
		k.pass()
		k.body.Reset()
		return k.w.Write(b)
	}
	return k.body.Write(b)
}

// pass sends on what is held and every write after it.
func (k *keepWriter) pass() {
	if k.passing {
		return
	}
	k.passing = true
	if k.status == 0 {
		k.status = http.StatusOK
	}
	k.w.WriteHeader(k.status)
	if k.body.Len() > 0 {
		_, _ = k.w.Write(k.body.Bytes())
	}
}

func (k *keepWriter) Flush() {
	if !k.passing {
		return
	}
	if f, ok := k.w.(http.Flusher); ok {
		f.Flush()
	}
}
