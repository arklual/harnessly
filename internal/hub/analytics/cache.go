package analytics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
)

// The heavy screens (the overview, the KPIs and their series, the waste, the effects of changes) sum a scope's sessions
// over weeks: seconds on a large organization. Their answers are kept per person and address for a short while
// (respCache.fresh) and then served as they are while a fresh one is computed behind (stale-while-revalidate), so a
// screen opened again or a scope switched back opens at once. The key carries a fingerprint of the person's rights
// (roles, tenants, teams), so a changed role never sees an answer computed for the old one.

const (
	// cacheStale is how long an answer past its freshness is still served while a new one is computed.
	cacheStale = 10 * time.Minute
	// cacheBytes bounds the bodies kept; the oldest go first.
	cacheBytes = 48 << 20
	// cacheRefresh bounds a refresh computed behind a served answer.
	cacheRefresh = 2 * time.Minute
)

type cachedResp struct {
	at     time.Time
	status int
	header http.Header
	body   []byte
	// refreshing is a refresh behind this answer under way.
	refreshing bool
}

type cacheCall struct {
	done chan struct{}
	res  *cachedResp
	err  error
}

type respCache struct {
	fresh time.Duration
	now   func() time.Time

	mu       sync.Mutex
	m        map[string]*cachedResp
	size     int
	inflight map[string]*cacheCall
}

func newRespCache(fresh time.Duration, now func() time.Time) *respCache {
	return &respCache{fresh: fresh, now: now, m: map[string]*cachedResp{}, inflight: map[string]*cacheCall{}}
}

// put keeps an answer, dropping the oldest ones past the byte bound.
func (c *respCache) put(key string, e *cachedResp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old := c.m[key]; old != nil {
		c.size -= len(old.body)
	}
	c.m[key] = e
	c.size += len(e.body)
	if c.size <= cacheBytes {
		return
	}
	keys := make([]string, 0, len(c.m))
	for k := range c.m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return c.m[a].at.Compare(c.m[b].at) })
	for _, k := range keys {
		if c.size <= cacheBytes*3/4 {
			break
		}
		c.size -= len(c.m[k].body)
		delete(c.m, k)
	}
}

// do computes the answer for key once however many requests ask for it at the same time.
func (c *respCache) do(key string, f func() (*cachedResp, error)) (*cachedResp, error) {
	c.mu.Lock()
	if call := c.inflight[key]; call != nil {
		c.mu.Unlock()
		<-call.done
		return call.res, call.err
	}
	call := &cacheCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()
	call.res, call.err = f()
	if call.err == nil && call.res.status == http.StatusOK {
		c.put(key, call.res)
	}
	c.mu.Lock()
	delete(c.inflight, key)
	c.mu.Unlock()
	close(call.done)
	return call.res, call.err
}

// recorder takes an answer down instead of sending it.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}

func capture(h orgs.Handler, r *http.Request, p *auth.Principal) (*cachedResp, error) {
	rec := &recorder{header: http.Header{}}
	if err := h(rec, r, p); err != nil {
		return nil, err
	}
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return &cachedResp{status: rec.status, header: rec.header, body: rec.body.Bytes()}, nil
}

func (e *cachedResp) write(w http.ResponseWriter, age time.Duration) {
	for k, v := range e.header {
		w.Header()[k] = slices.Clone(v)
	}
	w.Header().Set("X-Hub-Cache-Age", fmt.Sprintf("%d", int(age.Seconds())))
	w.WriteHeader(e.status)
	_, _ = w.Write(e.body)
}

// rightsOf is a fingerprint of what the person may see: their roles, tenants and teams, an installation admin, the
// token they came with.
func (s *Service) rightsOf(ctx context.Context, p *auth.Principal) (string, error) {
	st := s.db.S()
	h := sha256.New()
	tok := ""
	if p.Token != nil {
		tok = p.Token.ID
	}
	fmt.Fprintf(h, "%s|%t|%s|", p.User.ID, p.Admin, tok)
	gs, err := st.GrantsOfUser(ctx, p.User.ID)
	if err != nil {
		return "", err
	}
	g := make([]string, len(gs))
	for i, x := range gs {
		g[i] = x.ScopeKind + ":" + x.ScopeID + ":" + x.Role
	}
	sps, err := st.SpacesByUser(ctx, p.User.ID)
	if err != nil {
		return "", err
	}
	for _, x := range sps {
		g = append(g, "space:"+x.ID+":"+x.Role)
	}
	ts, err := st.TeamsOfUser(ctx, p.User.ID)
	if err != nil {
		return "", err
	}
	for _, x := range ts {
		g = append(g, fmt.Sprintf("team:%s:%t", x.TeamID, x.IsPrimary))
	}
	slices.Sort(g)
	for _, x := range g {
		fmt.Fprintf(h, "%s|", x)
	}
	return hex.EncodeToString(h.Sum(nil)[:12]), nil
}

// cached serves h through the cache (see the top of the file); without one it is h.
func (s *Service) cached(h orgs.Handler) orgs.Handler {
	if s.cache == nil {
		return h
	}
	c := s.cache
	return func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
		if r.Header.Get("Cache-Control") == "no-cache" {
			return h(w, r, p)
		}
		fp, err := s.rightsOf(r.Context(), p)
		if err != nil {
			return err
		}
		key := fp + " " + r.URL.RequestURI()
		now := c.now()
		c.mu.Lock()
		e := c.m[key]
		var age time.Duration
		refresh := false
		if e != nil {
			age = now.Sub(e.at)
			switch {
			case age < c.fresh:
			case age < c.fresh+cacheStale:
				if !e.refreshing {
					e.refreshing, refresh = true, true
				}
			default:
				e = nil
			}
		}
		c.mu.Unlock()
		if e != nil {
			if refresh {
				go s.refresh(c, key, h, r, p, e)
			}
			e.write(w, age)
			return nil
		}
		res, err := c.do(key, func() (*cachedResp, error) {
			res, err := capture(h, r, p)
			if res != nil {
				res.at = c.now()
			}
			return res, err
		})
		if err != nil {
			return err
		}
		res.write(w, 0)
		return nil
	}
}

func (s *Service) refresh(c *respCache, key string, h orgs.Handler, r *http.Request, p *auth.Principal, old *cachedResp) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), cacheRefresh)
	defer cancel()
	_, err := c.do(key, func() (*cachedResp, error) {
		res, err := capture(h, r.Clone(ctx), p)
		if res != nil {
			res.at = c.now()
		}
		return res, err
	})
	if err != nil {
		s.log.Warn("analytics cache refresh", "path", r.URL.Path, "err", err)
	}
	c.mu.Lock()
	old.refreshing = false
	if err != nil && c.m[key] == old {
		// The answer could not be computed again (a role taken away, the scope gone): it is not served any more.
		c.size -= len(old.body)
		delete(c.m, key)
	}
	c.mu.Unlock()
}
