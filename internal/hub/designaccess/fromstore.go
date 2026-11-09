package designaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/storerev"
)

// A guest link reads its project from the hub's store, not from the author's machine: the hub puts the
// revision into a folder of its own and answers with the runtime's code (internal/hub/designview). The machine is
// asked only for what the store cannot say: a project the store has nothing of yet (the screen «still loading from the
// owner's machine»), and a link pinned to a version of git, made before the library moved to the store.

// Viewer is the hub's own read of a project from the store (designview.View).
type Viewer interface {
	// Head is the project's newest revision; 0 when the store has nothing of it.
	Head(ctx context.Context, spaceID, project string) (int64, error)
	// Wait returns the head once it is past since, or as it is when ctx ends.
	Wait(ctx context.Context, spaceID, project string, since int64) (int64, error)
	// Serve answers a public read of the project at a revision.
	Serve(w http.ResponseWriter, r *http.Request, spaceID, project string, at int64, segs, pages []string, pinned bool) error
	// Write commits a file of the project through the hub (designview.View.Write).
	Write(ctx context.Context, spaceID, project, rel string, base int64, data []byte, author string) (Written, error)
	// Edit saves an edit of the deck or video editor through the hub and writes the editor's answer to w
	// (designview.View.Edit); an error comes before anything is written.
	Edit(ctx context.Context, w http.ResponseWriter, r *http.Request, spaceID, project, author, route string) error
}

// UseView makes guest links read their projects from the store; it is set once, before the hub serves.
func (s *Service) UseView(v Viewer) { s.view = v }

// storeRev is the revision the store answers a scope at: the pinned one, or the project's newest; false when the
// store cannot answer it.
func (s *Service) storeRev(ctx context.Context, m store.Machine, req hubproto.DesignPublicTokenRequest) (int64, bool, error) {
	if s.view == nil || m.SpaceID == "" {
		return 0, false, nil
	}
	if req.Rev != "" {
		n, ok := storerev.Of(req.Rev)
		return n, ok, nil
	}
	head, err := s.view.Head(ctx, m.SpaceID, req.Project)
	if err != nil {
		return 0, false, err
	}
	return head, head > 0, nil
}

// fromStore answers what fetch would ask the machine for; done false when the machine has to be asked.
func (s *Service) fromStore(ctx context.Context, machine string, req hubproto.DesignPublicTokenRequest, segs []string, r *http.Request, pass []string) (*http.Response, bool, error) {
	if s.view == nil {
		return nil, false, nil
	}
	m, err := s.db.S().MachineByID(ctx, machine)
	if err != nil {
		return nil, false, nil
	}
	if req.Pin {
		return s.pinFromStore(ctx, m, req, segs)
	}
	at, ok, err := s.storeRev(ctx, m, req)
	if err != nil {
		s.log.Warn("design: the store did not answer a guest read, the machine is asked", "project", req.Project, "error", err)
		return nil, false, nil
	}
	if !ok {
		return nil, false, nil
	}
	method := http.MethodGet
	if r != nil && r.Method == http.MethodHead {
		method = http.MethodHead
	}
	in, err := http.NewRequestWithContext(ctx, method, "http://hub/", nil)
	if err != nil {
		return nil, true, err
	}
	if r != nil {
		for _, h := range pass {
			if v := r.Header.Get(h); v != "" {
				in.Header.Set(h, v)
			}
		}
	}
	res, err := serveToResponse(in, func(w http.ResponseWriter, r *http.Request) error {
		return s.view.Serve(w, r, m.SpaceID, req.Project, at, segs, req.Pages, req.Rev != "")
	})
	return res, true, err
}

// pinFromStore answers the two questions a new link asks: may a link be made (__share) and which version a pinned one
// holds (__rev, the project's newest revision). The apps' checks that may block a link run on a machine: the owner's
// is asked when it is there, and a machine that is off lets the link be made, as it always has.
func (s *Service) pinFromStore(ctx context.Context, m store.Machine, req hubproto.DesignPublicTokenRequest, segs []string) (*http.Response, bool, error) {
	if len(segs) != 1 || (segs[0] != "__rev" && segs[0] != "__share") {
		return nil, false, nil
	}
	head, ok, err := s.storeRev(ctx, m, hubproto.DesignPublicTokenRequest{Project: req.Project})
	if err != nil || !ok {
		return nil, false, nil
	}
	if s.reg != nil && s.reg.Get(m.ID) != nil {
		if res, err := s.machineFetch(ctx, m.ID, req, []string{"__share"}, nil); err == nil {
			if res.StatusCode == http.StatusConflict {
				return res, true, nil
			}
			res.Body.Close()
		}
	}
	body := []byte(`{"blocked":false}`)
	if segs[0] == "__rev" {
		body, _ = json.Marshal(struct {
			Rev string `json:"rev"`
		}{storerev.ID(head)})
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json; charset=utf-8")
	return &http.Response{StatusCode: http.StatusOK, Header: h, ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(string(body)))}, true, nil
}

// serveToResponse runs a handler and hands its answer over as a response whose body streams while the handler writes.
// An error before the handler answered is returned as it is.
func serveToResponse(r *http.Request, serve func(http.ResponseWriter, *http.Request) error) (*http.Response, error) {
	pr, pw := io.Pipe()
	w := &pipeWriter{h: http.Header{}, pw: pw, pr: pr, got: make(chan *http.Response, 1)}
	failed := make(chan error, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				err := fmt.Errorf("design view: %v", v)
				failed <- err
				pw.CloseWithError(err)
			}
		}()
		err := serve(w, r)
		if err != nil && !w.sent() {
			failed <- err
			pw.CloseWithError(err)
			return
		}
		w.WriteHeader(http.StatusOK) // a handler that wrote nothing answered 200 with no body
		pw.CloseWithError(err)
	}()
	select {
	case res := <-w.got:
		return res, nil
	case err := <-failed:
		return nil, err
	case <-r.Context().Done():
		pr.Close()
		return nil, r.Context().Err()
	}
}

type pipeWriter struct {
	h    http.Header
	pw   *io.PipeWriter
	pr   *io.PipeReader
	got  chan *http.Response
	once sync.Once
	mu   sync.Mutex
	done bool
}

func (p *pipeWriter) Header() http.Header { return p.h }

func (p *pipeWriter) sent() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

func (p *pipeWriter) WriteHeader(code int) {
	p.once.Do(func() {
		p.mu.Lock()
		p.done = true
		p.mu.Unlock()
		size := int64(-1)
		if v, err := strconv.ParseInt(p.h.Get("Content-Length"), 10, 64); err == nil {
			size = v
		}
		p.got <- &http.Response{StatusCode: code, Header: p.h.Clone(), ContentLength: size, Body: p.pr}
	})
}

func (p *pipeWriter) Write(b []byte) (int, error) {
	p.WriteHeader(http.StatusOK)
	return p.pw.Write(b)
}

// ---- version and events ------------------------------------------------------------------------------------------

const eventsFor = 5 * time.Minute

// guestVersion is the version a link shows now: the store's revision of its project, or the one it is pinned to.
func (s *Service) guestVersion(w http.ResponseWriter, r *http.Request, g guestReq) error {
	at, ok, err := s.storeRev(r.Context(), g.m, g.scope())
	if err != nil {
		return err
	}
	if !ok {
		return gNoProject
	}
	guestJSON(w, 200, map[string]any{"version": at, "rev": storerev.ID(at), "pinned": g.l.Version == "pinned"})
	return nil
}

// guestEvents is the live link's stream «the project is at revision N»: one event at once, one more each time the
// store has a newer revision, for a few minutes (the page's EventSource comes back). A pinned link, or a project the
// store has nothing of, has nothing to say: 204, and the page stops asking.
func (s *Service) guestEvents(w http.ResponseWriter, r *http.Request, g guestReq) error {
	if g.l.Version == "pinned" {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	head, ok, err := s.storeRev(r.Context(), g.m, g.scope())
	if err != nil || !ok {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	s.streamRevs(w, r, g.m.SpaceID, g.l.Project, head)
	return nil
}

// streamRevs is the stream «the project is at revision N» from head on: one event at once, one more each time the
// store has a newer revision, for a few minutes, then a hint to come back.
func (s *Service) streamRevs(w http.ResponseWriter, r *http.Request, spaceID, project string, head int64) {
	flusher, _ := w.(http.Flusher)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(rev int64) bool {
		if _, err := fmt.Fprintf(w, "event: rev\ndata: %d\n\n", rev); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	if !send(head) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), eventsFor)
	defer cancel()
	for {
		next, err := s.view.Wait(ctx, spaceID, project, head)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return
		}
		if ctx.Err() != nil {
			_, _ = fmt.Fprint(w, "retry: 2000\n\n")
			return
		}
		if next > head {
			head = next
			if !send(head) {
				return
			}
		}
	}
}
