package proxy_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/proxy"
)

func shortHeaderWait(t *testing.T, d time.Duration) {
	t.Helper()
	t.Cleanup(proxy.SetHeaderWait(d))
}

type slowFixture struct {
	s       *hubtest.Stack
	owner   *hubtest.Person
	id      string
	release chan struct{}
	// held hears of every request that reached /hang; the proxy counted it before passing it on.
	held chan struct{}
}

// slowSetup is a machine whose runtime does not answer /hang (and /poll, /hang/wait) until released, streams /events
// and echoes on /ws.
func slowSetup(t *testing.T) *slowFixture {
	t.Helper()
	f := &slowFixture{release: make(chan struct{}), held: make(chan struct{}, 64)}
	hang := func(w http.ResponseWriter, r *http.Request) {
		select {
		case f.held <- struct{}{}:
		default:
		}
		select {
		case <-f.release:
		case <-r.Context().Done():
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/m/{id}/hang", hang)
	mux.HandleFunc("/m/{id}/hang/wait", hang)
	mux.HandleFunc("/m/{id}/ok", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") })
	mux.HandleFunc("/m/{id}/poll", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = io.WriteString(w, "polled")
	})
	mux.HandleFunc("/m/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for {
			if _, err := io.WriteString(w, "tick\n"); err != nil {
				return
			}
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
	mux.HandleFunc("/m/{id}/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, msg, err := c.Read(r.Context())
			if err != nil || c.Write(r.Context(), typ, msg) != nil {
				return
			}
		}
	})
	f.s = hubtest.New(t)
	f.owner = f.s.SignIn("owner@example.com")
	spaceID, _ := f.owner.NewSpace("Команда")
	f.id = f.owner.Machine(spaceID, mux).MachineID
	t.Cleanup(func() { close(f.release) })
	return f
}

func TestRuntimeThatDoesNotAnswerIsGivenUpOn(t *testing.T) {
	shortHeaderWait(t, 200*time.Millisecond)
	f := slowSetup(t)
	start := time.Now()
	r := f.owner.Do("GET", "/m/"+f.id+"/hang", nil)
	if r.Code != http.StatusGatewayTimeout || r.Err() != "machine_timeout" {
		t.Fatalf("a runtime that hangs: %s", r)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("gave up after %v", took)
	}
	// A request with a body waits as long from the moment the body went out.
	if r := f.owner.Do("POST", "/m/"+f.id+"/hang", map[string]any{"a": 1}); r.Code != http.StatusGatewayTimeout {
		t.Errorf("a POST to a runtime that hangs: %s", r)
	}
}

func TestStreamsSocketsAndLongPollsOutliveTheHeaderWait(t *testing.T) {
	shortHeaderWait(t, 150*time.Millisecond)
	f := slowSetup(t)

	t.Run("event stream", func(t *testing.T) {
		req, _ := http.NewRequest("GET", f.s.URL+"/m/"+f.id+"/events", nil)
		res, err := f.owner.Client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		br := bufio.NewReader(res.Body)
		deadline := time.Now().Add(600 * time.Millisecond)
		for time.Now().Before(deadline) {
			if line, err := br.ReadString('\n'); err != nil || line != "tick\n" {
				t.Fatalf("the stream ended early: %q %v", line, err)
			}
		}
	})

	t.Run("websocket", func(t *testing.T) {
		u, _ := url.Parse(f.s.URL)
		var cookie []string
		for _, c := range f.owner.Client.Jar.Cookies(u) {
			cookie = append(cookie, c.Name+"="+c.Value)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, "ws://"+u.Host+"/m/"+f.id+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookie, "; ")}}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		time.Sleep(500 * time.Millisecond)
		if err := c.Write(ctx, websocket.MessageText, []byte("late")); err != nil {
			t.Fatal(err)
		}
		if _, got, err := c.Read(ctx); err != nil || string(got) != "late" {
			t.Fatalf("the socket after the header wait: %q %v", got, err)
		}
	})

	t.Run("long poll", func(t *testing.T) {
		if r := f.owner.Do("GET", "/m/"+f.id+"/poll?wait=1", nil); r.Code != 200 || string(r.Body) != "polled" {
			t.Errorf("a long poll that says its wait: %s", r)
		}
	})
}

func TestOnePersonHoldsOnlySoManyRequestsOfAMachine(t *testing.T) {
	const maxPerPerson = 3
	t.Cleanup(proxy.SetMaxPerPerson(maxPerPerson))
	f := slowSetup(t)
	other := f.s.SignIn("other@example.com")

	var wg sync.WaitGroup
	for range maxPerPerson {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.owner.Do("GET", "/m/"+f.id+"/hang/wait", nil)
		}()
	}
	// Each request is counted before it goes on to the machine, so once all of them reached it the person is full.
	for range maxPerPerson {
		select {
		case <-f.held:
		case <-time.After(30 * time.Second):
			t.Fatal("the requests did not reach the machine")
		}
	}
	r := f.owner.Do("GET", "/m/"+f.id+"/ok", nil)
	if r.Code != http.StatusServiceUnavailable || r.Err() != "machine_busy" || !strings.Contains(string(r.Body), "Машина перегружена") {
		t.Errorf("one request too many: %s", r)
	}
	// Somebody else is not in the way of it: a stranger is refused for who they are, not for the crowd.
	if r := other.Do("GET", "/m/"+f.id+"/ok", nil); r.Code == http.StatusServiceUnavailable {
		t.Errorf("another person: %s", r)
	}
	f.release <- struct{}{}
	f.s.Eventually("a place to be free again", func() bool {
		return f.owner.Do("GET", "/m/"+f.id+"/ok", nil).Code == 200
	})
	for range maxPerPerson - 1 {
		f.release <- struct{}{}
	}
	wg.Wait()
}
