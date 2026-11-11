package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A runtime that hangs holds every request sent to it, each on a tunnel of its own, and the page asks again: without a
// limit the machine's tunnels run out and nobody opens it. So a request waits for the runtime's answer to begin only
// so long, and one person holds only so many requests of a machine at a time.

var (
	// headerWait is how long the runtime may take to start answering an ordinary request, counted from the moment the
	// request was sent whole. A variable so that a test can shorten it.
	headerWait = 60 * time.Second
	// maxPerPerson is how many requests of one machine a person holds at a time, sockets and streams included. The
	// agent's own limit on the machine's tunnels is several times this.
	maxPerPerson = 64
)

// longPollMargin is what a request that says how long it waits gets on top of that.
const longPollMargin = 30 * time.Second

var (
	errHeaderWait = errors.New("machine did not start answering in time")
	errCrowded    = errors.New("too many requests of one person to the machine")
)

// headerLimit is how long the answer to r may take to begin; 0 is no limit. A stream of events may open long before
// its first byte (an MCP call answers when its tool is done), and a long poll answers when what it waits for happens:
// one that names its wait in seconds (?wait=, ?timeout=) gets that and a margin, one that does not is not limited.
// A WebSocket's handshake is an ordinary answer; the socket itself lives outside the limit.
func headerLimit(r *http.Request) time.Duration {
	if d, ok := r.Context().Value(staleWaitKey{}).(time.Duration); ok {
		return d
	}
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return 0
	}
	q := r.URL.Query()
	for _, k := range []string{"wait", "timeout"} {
		if !q.Has(k) {
			continue
		}
		if n, err := strconv.Atoi(q.Get(k)); err == nil && n >= 0 && n < 24*3600 {
			return max(headerWait, time.Duration(n)*time.Second+longPollMargin)
		}
		return 0
	}
	if strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/wait") {
		return 0
	}
	return headerWait
}

// headerDeadline ends a request whose answer does not begin within headerLimit. http.Transport's own
// ResponseHeaderTimeout is one value for every request of the machine; this one is chosen per request.
type headerDeadline struct{ next http.RoundTripper }

func (h headerDeadline) RoundTrip(r *http.Request) (*http.Response, error) {
	limit := headerLimit(r)
	if limit <= 0 {
		return h.next.RoundTrip(r)
	}
	// The context is the request's own, ended by the caller when the request is over; it is cancelled here only when
	// the wait runs out, and never once the answer began: the body, or a socket, lives on it.
	ctx, cancel := context.WithCancelCause(r.Context())
	var mu sync.Mutex
	var timer *time.Timer
	arm := func() {
		mu.Lock()
		if timer == nil {
			timer = time.AfterFunc(limit, func() { cancel(errHeaderWait) })
		}
		mu.Unlock()
	}
	out := r.WithContext(ctx)
	if r.Body == nil || r.Body == http.NoBody {
		arm()
	} else {
		out.Body = &sentBody{ReadCloser: r.Body, sent: arm} // an upload takes as long as it takes
	}
	res, err := h.next.RoundTrip(out)
	mu.Lock()
	late := timer != nil && !timer.Stop()
	mu.Unlock()
	if late || errors.Is(context.Cause(ctx), errHeaderWait) {
		if res != nil {
			res.Body.Close()
		}
		return nil, errHeaderWait
	}
	return res, err
}

// sentBody tells when the request's body was read to its end or closed.
type sentBody struct {
	io.ReadCloser
	sent func()
}

func (b *sentBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.sent()
	}
	return n, err
}

func (b *sentBody) Close() error {
	b.sent()
	return b.ReadCloser.Close()
}

// enter counts a request of a person to a machine; false when the person already holds maxPerPerson of them.
func (s *Service) enter(userID, machineID string) (leave func(), ok bool) {
	key := userID + "/" + machineID
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight == nil {
		s.inflight = map[string]int{}
	}
	if s.inflight[key] >= maxPerPerson {
		return nil, false
	}
	s.inflight[key]++
	return func() {
		s.mu.Lock()
		if s.inflight[key]--; s.inflight[key] <= 0 {
			delete(s.inflight, key)
		}
		s.mu.Unlock()
	}, true
}

// crowded says whether err is the machine's agent refusing one more tunnel.
func crowded(err error) bool {
	return errors.Is(err, errCrowded) || (err != nil && strings.Contains(err.Error(), "too many tunnels"))
}
