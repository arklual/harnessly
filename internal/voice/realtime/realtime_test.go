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

type fakeOpenAI struct {
	t        *testing.T
	mu       sync.Mutex
	body     map[string]any
	headers  http.Header
	received []map[string]any
	conn     chan *websocket.Conn
}

func newFake(t *testing.T) (*fakeOpenAI, *httptest.Server) {
	f := &fakeOpenAI{t: t, conn: make(chan *websocket.Conn, 1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/codex/realtime/calls":
			raw, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			_ = json.Unmarshal(raw, &f.body)
			f.headers = r.Header.Clone()
			f.mu.Unlock()
			if r.URL.Query().Get("intent") != "quicksilver" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Location", "/v1/realtime/calls/rtc_abc123?x=1")
			_, _ = io.WriteString(w, "v=0 answer")
		case r.URL.Path == "/live/rtc_abc123":
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
					f.mu.Lock()
					f.received = append(f.received, m)
					f.mu.Unlock()
				}
			}()
			f.conn <- c
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func service(t *testing.T, srv *httptest.Server, signed bool) *Service {
	s := New(Config{
		Dir: t.TempDir(),
		Tokens: TokenFunc(func() (Token, bool) {
			return Token{Access: "tok-secret", Account: "acct-1"}, signed
		}),
		Endpoints: Endpoints{Call: srv.URL + "/codex", Live: "ws" + strings.TrimPrefix(srv.URL, "http") + "/live"},
	})
	return s
}

func TestStartIsRefusedWhileOffOrSignedOut(t *testing.T) {
	_, srv := newFake(t)
	s := service(t, srv, false)
	if _, err := s.Start(context.Background(), CallInput{SDP: "v=0"}); err == nil || !strings.Contains(err.Error(), CodeOff) {
		t.Fatalf("off: %v", err)
	}
	_ = s.SaveSettings(Settings{Subscription: true})
	if _, err := s.Start(context.Background(), CallInput{SDP: "v=0"}); err == nil || !strings.Contains(err.Error(), CodeNoLogin) {
		t.Fatalf("signed out: %v", err)
	}
	if st := s.State(); !st.Subscription || st.SignedIn || st.Ready || st.Voice != "cove" {
		t.Fatalf("state %+v", st)
	}
}

func TestCallRelaysTheControlSocket(t *testing.T) {
	f, srv := newFake(t)
	s := service(t, srv, true)
	_ = s.SaveSettings(Settings{Subscription: true, Voice: "sol"})
	st, err := s.Start(context.Background(), CallInput{SDP: "v=0 offer", Mode: ModeAgent, Context: "Доска «Главная»"})
	if err != nil {
		t.Fatal(err)
	}
	if st.ID != "rtc_abc123" || st.SDP != "v=0 answer" {
		t.Fatalf("started %+v", st)
	}
	f.mu.Lock()
	if f.headers.Get("Authorization") != "Bearer tok-secret" || f.headers.Get("chatgpt-account-id") != "acct-1" ||
		f.headers.Get("openai-alpha") != "quicksilver=v2" {
		t.Fatalf("headers %v", f.headers)
	}
	sess := f.body["session"].(map[string]any)
	if sess["model"] != Model || sess["audio"].(map[string]any)["output"].(map[string]any)["voice"] != "sol" ||
		!strings.Contains(sess["instructions"].(string), "Доска «Главная»") {
		t.Fatalf("session %v", sess)
	}
	f.mu.Unlock()

	call, err := s.Call(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Said before the socket joined: queued and sent on join.
	if err := call.Say("готово: доска поменяна", "del_1", true); err != nil {
		t.Fatal(err)
	}
	var conn *websocket.Conn
	select {
	case conn = <-f.conn:
	case <-time.After(5 * time.Second):
		t.Fatal("control socket never joined")
	}
	ctx := context.Background()
	for _, frame := range []string{
		`{"type":"session.started","session":{"id":"sess_1"}}`,
		`{"type":"output_audio.delta","audio":"AAAA"}`,
		`{"type":"input_transcript.added","item":{"text":"поменяй "}}`,
		`{"type":"turn.done","turn":{"role":"user","transcript":"поменяй заголовок"}}`,
		`{"type":"delegation.created","item":{"type":"delegation","id":"del_2","target":"client","content":[{"type":"input_text","text":"Поменяй заголовок"}]}}`,
		`{"type":"error","error":{"message":"bad thing"}}`,
	} {
		if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"ready", "user", "turn", "task", "error"}
	deadline := time.Now().Add(5 * time.Second)
	var evs []Event
	for time.Now().Before(deadline) {
		evs, _, _ = call.Events(0)
		if len(evs) >= len(want) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(evs) < len(want) {
		t.Fatalf("events %+v", evs)
	}
	for i, w := range want {
		if evs[i].Type != w {
			t.Fatalf("event %d = %+v, want %s", i, evs[i], w)
		}
	}
	if evs[3].ID != "del_2" || evs[3].Text != "Поменяй заголовок" || evs[4].Message != "bad thing" {
		t.Fatalf("task/error %+v %+v", evs[3], evs[4])
	}
	long := strings.Repeat("я", 400) // 800 bytes: two frames
	if err := call.Say(long, "", false); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.received)
		f.mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.mu.Lock()
	got := f.received
	f.mu.Unlock()
	if len(got) != 3 || got[0]["type"] != "delegation.context.append" || got[0]["delegation_item_id"] != "del_1" ||
		got[1]["type"] != "session.context.append" || got[1]["channel"] != "commentary" {
		t.Fatalf("received %v", got)
	}
	s.Hang(st.ID)
	if _, _, done := call.Events(0); !done {
		t.Fatal("not ended after hang")
	}
}

func TestChunksKeepRunes(t *testing.T) {
	for _, c := range chunks(strings.Repeat("ё", 300)+"abc", 500) {
		if len(c) > 500 || !strings.HasPrefix(c, "ё") && !strings.HasPrefix(c, "a") {
			t.Fatalf("chunk %q", c)
		}
	}
}

func TestCallIDAndScrub(t *testing.T) {
	if callID("/v1/realtime/calls/calls/rtc_backend_test") != "rtc_backend_test" ||
		callID("https://x/y/123e4567-e89b-12d3-a456-426614174000/z") != "123e4567-e89b-12d3-a456-426614174000" ||
		callID("/nothing/here") != "" {
		t.Fatal("callID")
	}
	if strings.Contains(scrub("auth Bearer abc.def-ghi failed"), "abc") {
		t.Fatal("scrub")
	}
}

func TestHandoffIsHeardAndTheVoiceIsToldNotToPretend(t *testing.T) {
	var heard []string
	c := &Call{mode: ModeChat, handoff: func(step, mode, task, text string) {
		heard = append(heard, mode+"/"+step+"/"+task+"/"+text)
	}, wake: make(chan struct{})}
	c.push(Event{Type: "task", ID: "del_1", Text: "Составь план на день"})
	c.push(Event{Type: "user", Text: "привет"})
	if len(heard) != 1 || heard[0] != "chat/task/del_1/Составь план на день" {
		t.Fatalf("heard %v", heard)
	}
	for _, mode := range []string{ModeChat, ModeAgent} {
		in := Instructions(mode, "")
		if !strings.Contains(in, "Never say that something is being made") || !strings.Contains(in, "delegation") {
			t.Fatalf("%s instructions: %s", mode, in)
		}
	}
	if strings.Contains(Instructions(ModeChat, ""), "Never hand anything over") {
		t.Fatal("the chat's voice must hand over what is to be made")
	}
}

// «GPT туповат» (15.11): the voice guessed what was on the board. It is only ears and mouth now: questions about the
// screen and the work go to the agent, and both modes have the hand-over on the key route.
func TestTheVoiceHandsOverWhatItCannotSee(t *testing.T) {
	agent := Instructions(ModeAgent, "")
	for _, want := range []string{"ears and mouth", "что на доске?", "Never guess what is on the screen"} {
		if !strings.Contains(agent, want) {
			t.Fatalf("agent instructions miss %q", want)
		}
	}
	for _, mode := range []string{ModeAgent, ModeChat} {
		if keySession("gpt-realtime", "marin", mode, "")["tools"] == nil {
			t.Fatalf("%s: no hand-over on the key route", mode)
		}
	}
}

func TestTalkIsKeptAndTextByDefault(t *testing.T) {
	s := New(Config{Dir: t.TempDir()})
	if got := s.State().Talk; got != TalkText {
		t.Fatalf("default %q", got)
	}
	if err := s.SaveSettings(Settings{Talk: TalkVoice}); err != nil {
		t.Fatal(err)
	}
	if got := s.State().Talk; got != TalkVoice {
		t.Fatalf("saved %q", got)
	}
	if st := s.State(); st.Head != HeadH || !st.Barge {
		t.Fatalf("defaults %+v", st)
	}
	if err := s.SaveSettings(Settings{Head: HeadFace, NoBarge: true}); err != nil {
		t.Fatal(err)
	}
	if st := s.State(); st.Head != HeadFace || st.Barge {
		t.Fatalf("saved %+v", st)
	}
}

func TestSubscriptionDefaultIsTheInstallations(t *testing.T) {
	// Another installation: off until the person turns it.
	if New(Config{Dir: t.TempDir()}).Settings().Subscription {
		t.Fatal("off by default elsewhere")
	}
	// The team's deployment: on for a person who never turned it, and their own off stays off.
	s := New(Config{Dir: t.TempDir(), SubscriptionOn: true})
	if !s.Settings().Subscription {
		t.Fatal("on by the deployment's default")
	}
	if err := s.SaveSettings(Settings{Voice: "cove"}); err != nil {
		t.Fatal(err)
	}
	if s.Settings().Subscription {
		t.Fatal("a saved off is the person's own")
	}
}

// A voice saved before the subscription route's list was cut to ChatGPT's own (an API name such as marin) falls back
// to the default: with it the call would be made but never connect.
func TestSubscriptionDropsAPIVoices(t *testing.T) {
	for _, v := range []string{"marin", "cedar", "verse", "alloy"} {
		if pick("", v, Voices[RouteSubscription]) != "cove" {
			t.Fatalf("%s kept on the subscription route", v)
		}
	}
}
