package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// fakeRuntime stands in for the runtime next to the agent: it records every call the agent makes, answers the routes
// the agent uses and serves the status and sync sockets.
type fakeRuntime struct {
	api    string
	secret []byte
	mu     sync.Mutex
	out    io.Writer
}

func (f *fakeRuntime) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(f.out, format+"\n", args...)
}

// cookieOK checks the session cookie the agent signs: HS256 with the pair's secret, subject machine-agent.
func (f *fakeRuntime) cookieOK(r *http.Request, name string) bool {
	c, err := r.Cookie(name)
	if err != nil {
		return false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return false
	}
	mac := hmac.New(sha256.New, f.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[2] {
		return false
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	return json.Unmarshal(raw, &claims) == nil && claims["sub"] == "machine-agent"
}

func (f *fakeRuntime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	auth := f.cookieOK(r, "session-token")
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		f.record("ws %s cookie=%v", r.URL.RequestURI(), auth)
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		kind := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		go func() {
			for i := 0; ; i++ {
				msgs := []string{`{"type":"pong"}`, fmt.Sprintf(`{"type":"%s:update","n":%d,"html":"<b>&</b>"}`, kind, i)}
				for _, m := range msgs {
					if conn.Write(context.Background(), websocket.MessageText, []byte(m)) != nil {
						return
					}
				}
				time.Sleep(time.Second)
			}
		}()
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			f.record("ws-in %s %s", kind, canonical(data))
		}
	}
	body, _ := io.ReadAll(r.Body)
	headers := []string{}
	for h, v := range r.Header {
		if h == "Content-Type" || strings.HasPrefix(h, "X-") {
			headers = append(headers, h+"="+strings.Join(v, ","))
		}
	}
	sort.Strings(headers)
	f.record("http %s %s cookie=%v %v %s", r.Method, r.URL.RequestURI(), auth, headers, canonical(body))
	switch {
	case r.URL.Path == f.api+"/machines":
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"cpu": 3, "release": "x"}`))
	case r.URL.Path == f.api+"/docs/notify":
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	case r.URL.Path == f.api+"/docs/deliver":
		w.WriteHeader(502)
		_, _ = w.Write([]byte(`not json`))
	case r.URL.Path == f.api+"/docs/resolve-pane":
		_, _ = w.Write([]byte(`{"tabId":"tab-1","sessionName":"s1","agentSessionId":null,"provider":"codex","cwd":"/w"}`))
	case r.URL.Path == "/health":
		_, _ = w.Write([]byte(`{"ok":true}`))
	default:
		_, _ = w.Write([]byte(`{"done":true}`))
	}
}

func serveRuntime(addr, api string, secret []byte, logFile string) {
	out, err := os.Create(logFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = http.ListenAndServe(addr, &fakeRuntime{api: api, secret: secret, out: out})
}
