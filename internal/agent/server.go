package agent

import (
	"context"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	dialHandshake = 10 * time.Second
	dialPing      = 15 * time.Second
	dialSilent    = 45 * time.Second
	dialMaxPause  = 5 * time.Second
	// The cluster answers a follower's call 409 (try again at once, the next call lands elsewhere) and a pair that is
	// switched off 403 or close code 4003 (wait a minute).
	statusNotLeader   = 409
	statusSwitchedOff = 403
	closeSwitchedOff  = 4003
)

func (a *Agent) handler(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.URL.RequestURI() != ListenPath || !ok || token == "" || VerifyHS256(a.secret, token, a.cfg.ListenAudience, time.Now()) != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		_ = a.serve(conn, nil)
		return
	}
	if a.localToken != "" && r.Method == http.MethodPost && (r.URL.RequestURI() == "/docs/bind" || r.URL.RequestURI() == "/docs/reply") {
		a.docRoute(w, r)
		return
	}
	if a.localToken != "" && r.Method == http.MethodPost && r.URL.RequestURI() == "/design/event" {
		a.designEventRoute(w, r)
		return
	}
	if a.localToken != "" && r.Method == http.MethodPost && (r.URL.RequestURI() == "/state/commit" || r.URL.RequestURI() == "/state/patch") {
		a.stateRoute(w, r)
		return
	}
	switch {
	case r.URL.RequestURI() == "/healthz":
		reply(w, 200, map[string]any{"ok": true, "version": a.cfg.Version})
	case r.Method == http.MethodPost && r.URL.RequestURI() == "/automations/report":
		a.automationReport(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (a *Agent) listen(ctx context.Context) error {
	// Without agent.json the CLIs cannot find the agent, but transcripts must keep flowing: never fatal.
	if token, err := writeLocalState(a.cfg.DataDir, a.cfg.Port); err != nil {
		Log("warn", "agent.json not written, doc threads are off on this machine", F{"error", err.Error()})
	} else {
		a.localToken = token
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(a.cfg.Host, strconv.Itoa(a.cfg.Port)))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.HandlerFunc(a.handler), ReadHeaderTimeout: 10 * time.Second}
	Log("info", "agent listening", F{"host", a.cfg.Host}, F{"port", a.cfg.Port}, F{"user", a.hello.User}, F{"version", a.cfg.Version})
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	a.mu.Lock()
	for l := range a.clients {
		l.close(websocket.StatusGoingAway, "agent restarting")
	}
	a.mu.Unlock()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	return nil
}

// dialCluster keeps one outbound socket to the cluster: the machine calls the cluster, not the other way round.
func (a *Agent) dialCluster(ctx context.Context) {
	pause := time.Second
	for ctx.Err() == nil {
		retryIn := a.dialOnce(ctx, &pause)
		delay := retryIn
		if delay == 0 {
			// The cluster is one address and a call costs nothing: a long pause is how long statuses wait after it is back.
			delay = pause
			pause = min(pause*2, dialMaxPause)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (a *Agent) dialOnce(ctx context.Context, pause *time.Duration) time.Duration {
	u, err := url.Parse(a.cfg.ClusterURL)
	if err != nil {
		Log("warn", "cluster dial failed", F{"error", err.Error()})
		return 0
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = DialPath
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+SignHS256(a.secret, a.cfg.RuntimeID, a.cfg.DialAudience, 5*time.Minute))
	dialCtx, cancel := context.WithTimeout(ctx, dialHandshake)
	conn, res, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPHeader: header, CompressionMode: websocket.CompressionDisabled})
	cancel()
	if err != nil {
		if res != nil && res.StatusCode != http.StatusSwitchingProtocols {
			switch res.StatusCode {
			case statusNotLeader:
				return 300*time.Millisecond + time.Duration(rand.Float64()*700)*time.Millisecond
			case statusSwitchedOff:
				Log("info", "cluster did not take the call", F{"status", res.StatusCode})
				return time.Minute
			default:
				Log("info", "cluster did not take the call", F{"status", res.StatusCode})
				return 0
			}
		}
		Log("warn", "cluster link error", F{"error", err.Error()})
		return 0
	}
	*pause = time.Second
	Log("info", "dialled the cluster", F{"url", u.Scheme + "://" + u.Host})

	// Liveness: a ping every 15 s; 45 s without a pong or a message and the link is dropped.
	heard := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		last := time.Now()
		t := time.NewTicker(dialPing)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-heard:
				last = time.Now()
			case <-t.C:
				if time.Since(last) > dialSilent {
					conn.CloseNow()
					return
				}
				go func() {
					pctx, cancel := context.WithTimeout(ctx, dialSilent)
					defer cancel()
					if conn.Ping(pctx) == nil {
						select {
						case heard <- struct{}{}:
						default:
						}
					}
				}()
			}
		}
	}()
	err = a.serve(conn, heard)
	close(done)
	if websocket.CloseStatus(err) == closeSwitchedOff {
		return time.Minute
	}
	return 0
}
