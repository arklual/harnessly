package link

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Several replicas of the hub (HUB_REPLICAS=on, docs/hub/replicas.md).
//
// A machine dials one replica and its WebSocket lives in that process. The directory (table machine_links) says which
// replica holds which machine: the owner takes a lease (renewed every third of the TTL) and a fencing token, greater
// every time the machine is taken over. A request for a machine on another replica is carried to the owner over an
// internal HTTP hop (POST …/call, a WebSocket for …/tunnel), authenticated with a secret shared by the replicas.
//
// The hop was chosen over LISTEN/NOTIFY because a tunnel is a stream, and a call wants its answer on the same connection
// with the agent's own error; NOTIFY would need a second channel for the answers and cannot carry bytes. The hop needs
// one more thing, an address each replica is reachable at (HUB_REPLICA_ADDR), which a Kubernetes pod always has.
//
// Fencing: a replica that lost its lease (it was paused, or cut from the database) may still hold the agent's socket.
// It learns it on the next renewal and closes the link; until then every write it makes on behalf of the link is
// guarded in SQL by the token (store.*Fenced), so a stale owner writes nothing, and the owner a call reaches checks the
// token the caller saw.

// DefaultLeaseTTL is how long a lease lasts without a renewal.
const DefaultLeaseTTL = 15 * time.Second

const (
	replicaPrefix = "/internal/replica/v1/"
	secretHeader  = "X-Replica-Secret"
	lookupFresh   = time.Second
)

// Cluster is this replica's membership.
type Cluster struct {
	DB     *store.DB
	ID     string
	Addr   string
	Secret string
	TTL    time.Duration
	Now    func() time.Time
	Log    *slog.Logger
	HTTP   *http.Client

	mu   sync.Mutex
	seen map[string]seenLink
	reg  *Registry
}

type seenLink struct {
	l  *Link
	at time.Time
}

// NewID is a random replica id.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "r-" + hex.EncodeToString(b)
}

func (c *Cluster) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Cluster) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return DefaultLeaseTTL
}

func (c *Cluster) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

func (c *Cluster) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// SetCluster turns the registry into a replica of a cluster. Called once, before the first link.
func (r *Registry) SetCluster(c *Cluster) {
	r.cluster = c
	c.mu.Lock()
	c.reg = r
	c.seen = map[string]seenLink{}
	c.mu.Unlock()
}

// Cluster is the registry's cluster, nil for a single process.
func (r *Registry) Cluster() *Cluster { return r.cluster }

// Fence is the fencing token of a link taken in a cluster; 0 for a link of a single process.
func (l *Link) Fence() int64 { return l.fence }

// Owner is the replica that holds the link; "" for a link of a single process.
func (l *Link) Owner() string { return l.owner }

// Remote says whether the link is held by another replica and this one only forwards.
func (l *Link) Remote() bool { return l.remote != nil }

// acquire takes the directory entry of l's machine and starts renewing it.
func (c *Cluster) acquire(r *Registry, l *Link) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fence, err := c.DB.S().AcquireLink(ctx, l.MachineID, c.ID, c.Addr, c.now(), c.ttl())
	if err != nil {
		// The link still works here, only other replicas cannot find it, and its writes are not guarded.
		c.log().Warn("link not entered in the directory", "machine", l.MachineID, "error", err)
		return
	}
	l.owner, l.fence = c.ID, fence
	go c.renew(l)
}

// renew keeps the lease while the link lives; a lost lease, or one that could not be renewed for a whole TTL, ends the
// link, so the agent redials and the machine ends up with one owner.
func (c *Cluster) renew(l *Link) {
	tick := time.NewTicker(c.ttl() / 3)
	defer tick.Stop()
	okAt := time.Now()
	for {
		select {
		case <-l.done:
			return
		case <-tick.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.ttl()/3)
		held, err := c.DB.S().RenewLink(ctx, l.MachineID, c.ID, l.fence, c.now(), c.ttl())
		cancel()
		switch {
		case err == nil && held:
			okAt = time.Now()
		case err == nil:
			c.log().Info("lease lost, closing the link", "machine", l.MachineID, "fence", l.fence)
			l.Close(websocket.StatusCode(hubproto.CloseReplaced), "taken over by another replica")
			return
		case time.Since(okAt) > c.ttl():
			c.log().Warn("lease not renewable, closing the link", "machine", l.MachineID, "error", err)
			l.Close(websocket.StatusCode(hubproto.CloseReplaced), "lease expired")
			return
		}
	}
}

func (c *Cluster) release(l *Link) {
	if l.fence == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.DB.S().ReleaseLink(ctx, l.MachineID, c.ID, l.fence, c.now()); err != nil {
		c.log().Warn("link not released", "machine", l.MachineID, "error", err)
	}
}

// lookup is a link another replica holds, or nil. A machine this replica holds is never remote.
func (c *Cluster) lookup(id string) *Link {
	c.mu.Lock()
	if s, ok := c.seen[id]; ok && time.Since(s.at) < lookupFresh {
		c.mu.Unlock()
		return s.l
	}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	row, ok, err := c.DB.S().LinkOf(ctx, id, c.now())
	var l *Link
	if err == nil && ok && row.Owner != c.ID {
		l = &Link{MachineID: id, owner: row.Owner, fence: row.Fence, done: make(chan struct{}), remote: &remote{c: c, addr: row.Addr}}
	}
	c.mu.Lock()
	c.seen[id] = seenLink{l: l, at: time.Now()}
	c.mu.Unlock()
	return l
}

func (c *Cluster) forget(id string) {
	c.mu.Lock()
	delete(c.seen, id)
	c.mu.Unlock()
}

// remoteOnline lists the machines other replicas hold.
func (c *Cluster) remoteOnline() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := c.DB.S().LiveLinks(ctx, c.now())
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range rows {
		if r.Owner != c.ID {
			out = append(out, r.MachineID)
		}
	}
	return out
}

// remote is the far end of a link held by another replica.
type remote struct {
	c    *Cluster
	addr string
}

func (rm *remote) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, rm.addr+replicaPrefix+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set(secretHeader, rm.c.Secret)
	req.Header.Set("Content-Type", "application/json")
	return rm.c.client().Do(req)
}

type hopCall struct {
	Machine string          `json:"machine"`
	Fence   int64           `json:"fence"`
	Op      string          `json:"op"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type hopAnswer struct {
	Result json.RawMessage `json:"result,omitempty"`
	Agent  *hubproto.Error `json:"agent,omitempty"`
	// Gone: the owner does not hold this link (any more, or with another fence).
	Gone bool `json:"gone,omitempty"`
}

func (l *Link) remoteCall(ctx context.Context, op string, params any) (json.RawMessage, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	res, err := l.remote.request(ctx, http.MethodPost, "call", hopCall{Machine: l.MachineID, Fence: l.fence, Op: op, Params: raw})
	if err != nil {
		l.remote.c.forget(l.MachineID)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrClosed
	}
	defer res.Body.Close()
	var a hopAnswer
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, MaxPayload)).Decode(&a) != nil {
		l.remote.c.forget(l.MachineID)
		return nil, ErrClosed
	}
	switch {
	case a.Gone:
		l.remote.c.forget(l.MachineID)
		return nil, ErrClosed
	case a.Agent != nil:
		return nil, a.Agent
	}
	return a.Result, nil
}

// OpenConn is a connection to the runtime of the machine over the link: a tunnel here, or one carried over the owner
// replica's hop.
func (l *Link) OpenConn(ctx context.Context) (net.Conn, error) {
	if l.remote == nil {
		return l.OpenTunnel(ctx)
	}
	u, err := url.Parse(l.remote.addr)
	if err != nil {
		return nil, ErrClosed
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + replicaPrefix + "tunnel"
	u.RawQuery = url.Values{"machine": {l.MachineID}, "fence": {strconv.FormatInt(l.fence, 10)}}.Encode()
	conn, res, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPHeader: http.Header{secretHeader: {l.remote.c.Secret}}, HTTPClient: l.remote.c.client(),
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		l.remote.c.forget(l.MachineID)
		if res != nil && res.StatusCode == http.StatusConflict {
			return nil, ErrClosed
		}
		return nil, fmt.Errorf("%w: %v", ErrClosed, err)
	}
	conn.SetReadLimit(-1)
	return websocket.NetConn(context.Background(), conn, websocket.MessageBinary), nil
}

// Handler serves the replicas' internal hop. It is mounted at /internal/replica/v1/ and checks the shared secret on
// every request; the ingress should not route the path from outside (the secret is the second lock).
func (c *Cluster) Handler() http.Handler {
	mux := http.NewServeMux()
	guard := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c.Secret == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get(secretHeader)), []byte(c.Secret)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			h(w, r)
		})
	}
	mux.Handle("POST "+replicaPrefix+"call", guard(c.serveCall))
	mux.Handle("GET "+replicaPrefix+"tunnel", guard(c.serveTunnel))
	mux.Handle("POST "+replicaPrefix+"close", guard(c.serveClose))
	return mux
}

// held is the local link of a machine when its fence is the one the caller saw (0: any).
func (c *Cluster) held(id string, fence int64) *Link {
	l := c.reg.local(id)
	if l == nil || (fence != 0 && l.fence != fence) {
		return nil
	}
	return l
}

func (c *Cluster) serveCall(w http.ResponseWriter, r *http.Request) {
	var in hopCall
	if err := json.NewDecoder(io.LimitReader(r.Body, MaxPayload)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	reply := func(a hopAnswer) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a)
	}
	l := c.held(in.Machine, in.Fence)
	if l == nil {
		reply(hopAnswer{Gone: true})
		return
	}
	var params any
	if len(in.Params) > 0 {
		params = in.Params
	}
	raw, err := l.Call(r.Context(), in.Op, params)
	var ae *hubproto.Error
	switch {
	case errors.As(err, &ae):
		reply(hopAnswer{Agent: ae})
	case errors.Is(err, ErrClosed):
		reply(hopAnswer{Gone: true})
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		reply(hopAnswer{Result: raw})
	}
}

func (c *Cluster) serveTunnel(w http.ResponseWriter, r *http.Request) {
	fence, _ := strconv.ParseInt(r.URL.Query().Get("fence"), 10, 64)
	l := c.held(r.URL.Query().Get("machine"), fence)
	if l == nil {
		http.Error(w, "not the owner", http.StatusConflict)
		return
	}
	tn, err := l.OpenTunnel(r.Context())
	if err != nil {
		http.Error(w, "tunnel not opened", http.StatusBadGateway)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		_ = tn.Close()
		return
	}
	conn.SetReadLimit(-1)
	ws := websocket.NetConn(context.Background(), conn, websocket.MessageBinary)
	done := make(chan struct{}, 2)
	pipe := func(dst io.Writer, src io.Reader) { _, _ = io.Copy(dst, src); done <- struct{}{} }
	go pipe(ws, tn)
	go pipe(tn, ws)
	<-done
	_ = tn.Close()
	_ = ws.Close()
}

type hopClose struct {
	Machine string `json:"machine"`
	Code    int    `json:"code"`
	Reason  string `json:"reason"`
}

func (c *Cluster) serveClose(w http.ResponseWriter, r *http.Request) {
	var in hopClose
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if l := c.reg.takeLocal(in.Machine); l != nil {
		l.Close(websocket.StatusCode(in.Code), in.Reason)
	}
	w.WriteHeader(http.StatusNoContent)
}

// closeRemote asks the replica that holds the machine's link to end it (a revoked or switched-off machine).
func (c *Cluster) closeRemote(id string, code int, reason string) {
	l := c.lookup(id)
	if l == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := l.remote.request(ctx, http.MethodPost, "close", hopClose{Machine: id, Code: code, Reason: reason})
	if err != nil {
		c.log().Warn("the owner of a link was not told to close it", "machine", id, "error", err)
		return
	}
	_ = res.Body.Close()
	c.forget(id)
}
