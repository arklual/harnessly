package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"harnessly/internal/agent"
)

// serveIngest stands in for the cluster the agent dials: the first call is answered 409 (a follower), the next one is
// taken and the script runs over it.
func serveIngest(addr string, secret []byte, audience, runtimeID string, script func(*client)) {
	var calls atomic.Int32
	done := make(chan struct{})
	first := time.Now()
	srv := &http.Server{Addr: addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims := ""
		if parts := strings.Split(token, "."); len(parts) == 3 {
			raw, _ := b64url(parts[1])
			claims = string(raw)
		}
		fmt.Printf("dial %d path=%s token=%v claims-sub=%v after=%s\n", n, r.URL.Path,
			agent.VerifyHS256(secret, token, audience, time.Now()) == nil, strings.Contains(claims, `"sub":"`+runtimeID+`"`),
			time.Since(first).Round(100*time.Millisecond))
		if n == 1 {
			w.WriteHeader(409)
			return
		}
		if n > 2 {
			w.WriteHeader(503)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		script(newClient(conn))
		conn.Close(websocket.StatusNormalClosure, "")
		close(done)
	})}
	go func() { _ = srv.ListenAndServe() }()
	select {
	case <-done:
	case <-time.After(5 * time.Minute):
		fmt.Fprintln(os.Stderr, "the agent never dialled")
	}
}
