package realtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestKeyRouteWhenTheSubscriptionIsOff(t *testing.T) {
	var (
		mu       sync.Mutex
		session  map[string]any
		auth     string
		received []map[string]any
	)
	conns := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/realtime/calls":
			_ = r.ParseMultipartForm(1 << 20)
			mu.Lock()
			_ = json.Unmarshal([]byte(r.FormValue("session")), &session)
			auth = r.Header.Get("Authorization")
			mu.Unlock()
			if !strings.HasPrefix(r.FormValue("sdp"), "v=0") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Location", "/v1/realtime/calls/rtc_key1")
			_, _ = io.WriteString(w, "v=0 key answer")
		case "/v1/realtime":
			if r.URL.Query().Get("call_id") != "rtc_key1" || r.Header.Get("Authorization") != "Bearer sk-test" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			go func() {
				for {
					_, raw, err := c.Read(context.Background())
					if err != nil {
						return
					}
					var m map[string]any
					_ = json.Unmarshal(raw, &m)
					mu.Lock()
					received = append(received, m)
					mu.Unlock()
				}
			}()
			conns <- c
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	s := New(Config{
		Dir:       t.TempDir(),
		Tokens:    TokenFunc(func() (Token, bool) { return Token{Access: "tok"}, true }),
		Key:       func() (string, bool) { return "sk-test", true },
		Endpoints: Endpoints{KeyCall: srv.URL + "/v1/realtime/calls", KeyLive: ws + "/v1/realtime"},
	})
	// Off: the key route, even with Codex signed in.
	if st := s.State(); st.Route != RouteKey || !st.Ready || st.Model != "gpt-realtime-2.1" || st.Voice != "marin" {
		t.Fatalf("state %+v", st)
	}
	started, err := s.Start(context.Background(), CallInput{SDP: "v=0 offer", Mode: ModeAgent, Model: "gpt-realtime-mini", Voice: "cove"})
	if err != nil {
		t.Fatal(err)
	}
	if started.ID != "rtc_key1" || started.Route != RouteKey || started.Model != "gpt-realtime-mini" {
		t.Fatalf("started %+v", started)
	}
	mu.Lock()
	if auth != "Bearer sk-test" || session["model"] != "gpt-realtime-mini" || session["tools"] == nil || session["reasoning"] != nil ||
		session["audio"].(map[string]any)["output"].(map[string]any)["voice"] != "marin" {
		t.Fatalf("auth %q session %v", auth, session)
	}
	mu.Unlock()
	call, _ := s.Call(started.ID)
	var conn *websocket.Conn
	select {
	case conn = <-conns:
	case <-time.After(5 * time.Second):
		t.Fatal("control socket never joined")
	}
	for _, f := range []string{
		`{"type":"session.created"}`,
		`{"type":"conversation.item.input_audio_transcription.completed","transcript":"поменяй заголовок"}`,
		`{"type":"response.function_call_arguments.done","name":"hand_over","call_id":"call_9","arguments":"{\"task\":\"Поменяй заголовок\"}"}`,
		`{"type":"response.output_audio_transcript.delta","delta":"Передал"}`,
	} {
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(f))
	}
	deadline := time.Now().Add(5 * time.Second)
	var evs []Event
	for time.Now().Before(deadline) {
		if evs, _, _ = call.Events(0); len(evs) >= 4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(evs) < 4 || evs[0].Type != "ready" || evs[1].Role != "user" || evs[2].Type != "task" || evs[2].ID != "call_9" ||
		evs[2].Text != "Поменяй заголовок" || evs[3].Text != "Передал" {
		t.Fatalf("events %+v", evs)
	}
	if err := call.Say("Заголовок поменян", "call_9", true); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n >= 4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	// The tool call answered at once, then the result as a note the voice tells.
	if len(received) != 4 || received[0]["item"].(map[string]any)["type"] != "function_call_output" ||
		received[1]["type"] != "response.create" || received[2]["item"].(map[string]any)["role"] != "system" ||
		received[3]["type"] != "response.create" {
		t.Fatalf("received %v", received)
	}
}

func TestNoRouteSaysWhy(t *testing.T) {
	s := New(Config{Dir: t.TempDir(), Tokens: TokenFunc(func() (Token, bool) { return Token{}, false })})
	if _, err := s.Start(context.Background(), CallInput{SDP: "v=0"}); err == nil || !strings.Contains(err.Error(), CodeOff) {
		t.Fatalf("off: %v", err)
	}
	_ = s.SaveSettings(Settings{Subscription: true})
	if _, err := s.Start(context.Background(), CallInput{SDP: "v=0"}); err == nil || !strings.Contains(err.Error(), CodeNoLogin) {
		t.Fatalf("no login: %v", err)
	}
}

func TestTypedWordsAreThePersons(t *testing.T) {
	k := (&keyWire{}).typed("привет")
	if len(k) != 2 || !strings.Contains(string(k[0]), `"role":"user"`) || !strings.Contains(string(k[1]), "response.create") {
		t.Fatalf("key %s", k)
	}
	s := (&subscriptionWire{}).typed("привет")
	if len(s) != 1 || !strings.Contains(string(s[0]), "speakable") || !strings.Contains(string(s[0]), "привет") {
		t.Fatalf("subscription %s", s)
	}
}
