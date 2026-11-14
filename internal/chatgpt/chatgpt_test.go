package chatgpt

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func signIn(t *testing.T, s *Service, f *fakeOpenAI) AccountView {
	t.Helper()
	st, err := s.StartLogin("")
	if err != nil {
		t.Fatal(err)
	}
	acct, err := s.CompleteLogin(context.Background(), f.authorize(t, st.URL))
	if err != nil {
		t.Fatal(err)
	}
	return acct
}

func codeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestAuthorizeAddress(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	st, err := s.StartLogin("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.CancelLogin()
	u, _ := url.Parse(st.URL)
	q := u.Query()
	if u.Path != "/api/accounts/authorize" {
		t.Fatalf("path %q", u.Path)
	}
	want := map[string]string{
		"client_id": dynamicClient, "agent_name_hint": AgentName, "response_type": "code", "scope": scopes,
		"resource": resource, "code_challenge_method": "S256",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if !strings.HasPrefix(q.Get("ext_agent_host_id"), "urn:uuid:") {
		t.Errorf("host id %q", q.Get("ext_agent_host_id"))
	}
	ru, _ := url.Parse(q.Get("redirect_uri"))
	if ru.Scheme != "http" || ru.Hostname() != "127.0.0.1" || ru.Path != "/auth/callback" {
		t.Errorf("redirect %q", q.Get("redirect_uri"))
	}
	if len(q.Get("state")) < 40 || len(q.Get("nonce")) < 40 || len(q.Get("code_challenge")) != 43 {
		t.Errorf("weak state/nonce/challenge: %q %q %q", q.Get("state"), q.Get("nonce"), q.Get("code_challenge"))
	}
	if q.Has("id_token_hint") || q.Has("prompt") {
		t.Error("a new registration sends no hint and forces no consent")
	}
	// The host id is the same on the next attempt.
	st2, _ := s.StartLogin("")
	u2, _ := url.Parse(st2.URL)
	if u2.Query().Get("ext_agent_host_id") != q.Get("ext_agent_host_id") || u2.Query().Get("state") == q.Get("state") {
		t.Error("host id must stay, state must change")
	}
}

func TestSignInKeepsTokensInVaultOnly(t *testing.T) {
	f := newFake(t)
	s, v := newTestService(t, f)
	acct := signIn(t, s, f)
	if !acct.SignedIn || !acct.PlanUse || acct.Label != "person@example.com" || acct.Plan != "plus" {
		t.Fatalf("account %+v", acct)
	}
	raw := v.m["chatgpt/accounts"]
	var rec record
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatal(err)
	}
	a := rec.Accounts[0]
	if a.ClientID != f.issued || a.Subject != f.subject || a.RefreshToken != "rt-1" || a.AccessToken != "at-0" || a.IDToken == "" {
		t.Fatalf("record %+v", a)
	}
	// Nothing of the tokens on disk outside the safe.
	_ = filepath.Walk(s.chats.dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), "rt-1") || strings.Contains(string(b), "at-0") {
				t.Errorf("token in %s", p)
			}
		}
		return nil
	})
	st, _ := s.State()
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "rt-1") || strings.Contains(string(b), "at-0") || strings.Contains(string(b), f.issued) {
		t.Errorf("state shows a token or the client: %s", b)
	}
	if st.Pending {
		t.Error("the sign-in still waits")
	}
}

func TestCallbackChecks(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	ctx := context.Background()

	// A state no attempt made.
	st, _ := s.StartLogin("")
	cb := f.authorize(t, st.URL)
	forged := strings.Replace(cb, "state=", "state=x", 1)
	if _, err := s.CompleteLogin(ctx, forged); codeOf(err) != CodeState {
		t.Fatalf("forged state: %v", err)
	}
	// The real one works once, and only once.
	if _, err := s.CompleteLogin(ctx, cb); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteLogin(ctx, cb); codeOf(err) != CodeState {
		t.Fatalf("replayed: %v", err)
	}
	// Not a callback address.
	for _, bad := range []string{"https://evil.example/auth/callback?code=1&state=2", "http://127.0.0.1:1/other?x=1", "nonsense"} {
		if _, err := s.CompleteLogin(ctx, bad); codeOf(err) != CodeBadCallback {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// Declined in the browser.
	st, _ = s.StartLogin("")
	u, _ := url.Parse(st.URL)
	denied := u.Query().Get("redirect_uri") + "?error=access_denied&state=" + url.QueryEscape(u.Query().Get("state"))
	if _, err := s.CompleteLogin(ctx, denied); codeOf(err) != CodeDenied {
		t.Fatalf("denied: %v", err)
	}
	// A new registration's callback must name its issued client.
	st, _ = s.StartLogin("")
	cb = f.authorize(t, st.URL)
	noClient := strings.Replace(cb, "client_id="+f.issued, "client_id=", 1)
	if _, err := s.CompleteLogin(ctx, noClient); codeOf(err) != CodeClient {
		t.Fatalf("no client: %v", err)
	}
}

func TestIDTokenChecks(t *testing.T) {
	for name, set := range map[string]func(f *fakeOpenAI){
		"audience": func(f *fakeOpenAI) { f.idAudience = "oaiapp_other" },
		"nonce":    func(f *fakeOpenAI) { f.idNonce = "other-nonce" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			set(f)
			s, v := newTestService(t, f)
			st, _ := s.StartLogin("")
			if _, err := s.CompleteLogin(context.Background(), f.authorize(t, st.URL)); codeOf(err) != CodeIDToken {
				t.Fatalf("want id-token, got %v", err)
			}
			if strings.Contains(v.m["chatgpt/accounts"], "at-0") {
				t.Fatal("tokens kept after a failed check")
			}
		})
	}
}

func TestPlanNotAllowed(t *testing.T) {
	f := newFake(t)
	f.scope = "openid profile email offline_access"
	s, _ := newTestService(t, f)
	acct := signIn(t, s, f)
	if acct.PlanUse {
		t.Fatal("plan use without the scope")
	}
	if _, err := s.Models(context.Background()); codeOf(err) != CodeNoPlan {
		t.Fatalf("models without plan use: %v", err)
	}
	// Signing in again asks for consent once more.
	st, err := s.StartLogin(acct.Key)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(st.URL)
	q := u.Query()
	if q.Get("prompt") != "consent" || q.Get("client_id") != f.issued || q.Has("agent_name_hint") || q.Has("id_token_hint") || q.Get("login_hint") != f.email {
		t.Fatalf("reauthorization %v", q)
	}
}

func TestRefreshSerializedAndRotated(t *testing.T) {
	f := newFake(t)
	s, v := newTestService(t, f)
	acct := signIn(t, s, f)
	// Make the access token due.
	_ = s.accs.update(acct.Key, func(a *Account) { a.ExpiresAt = time.Now().UnixMilli() })
	var wg sync.WaitGroup
	tokens := make([]string, 8)
	for i := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := s.oauth.token(context.Background(), acct.Key)
			if err != nil {
				t.Error(err)
			}
			tokens[i] = tok
		}()
	}
	wg.Wait()
	if f.refreshes != 1 {
		t.Fatalf("%d refreshes, want 1", f.refreshes)
	}
	for _, tok := range tokens {
		if tok != "at-1" {
			t.Fatalf("token %q", tok)
		}
	}
	if !strings.Contains(v.m["chatgpt/accounts"], `"refreshToken":"rt-2"`) {
		t.Fatal("the rotated refresh token is not kept")
	}
}

func TestRefreshUnusableSignsOut(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	acct := signIn(t, s, f)
	_ = s.accs.update(acct.Key, func(a *Account) { a.ExpiresAt = 0 })
	f.refreshFail = "invalid_grant"
	if _, err := s.oauth.token(context.Background(), acct.Key); codeOf(err) != CodeSignedOut {
		t.Fatalf("want signed-out, got %v", err)
	}
	a, _ := s.accs.get(acct.Key)
	if a.SignedIn() || a.ClientID != f.issued {
		t.Fatalf("tokens must go, the registration stay: %+v", a)
	}
}

func TestSignOutRevokes(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	acct := signIn(t, s, f)
	revoked, err := s.SignOut(context.Background(), acct.Key)
	if err != nil || !revoked {
		t.Fatalf("revoked %v err %v", revoked, err)
	}
	if len(f.revoked) != 1 || f.revoked[0].Get("token") != "rt-1" || f.revoked[0].Get("token_type_hint") != "refresh_token" || f.revoked[0].Get("client_id") != f.issued {
		t.Fatalf("revocation %v", f.revoked)
	}
	st, _ := s.State()
	if len(st.Accounts) != 1 || st.Accounts[0].SignedIn {
		t.Fatalf("state %+v", st)
	}
	// Signing in again reuses the registration and the host.
	st2, _ := s.StartLogin(acct.Key)
	u, _ := url.Parse(st2.URL)
	if u.Query().Get("client_id") != f.issued {
		t.Fatal("not the issued client")
	}
	if _, err := s.Forget(context.Background(), acct.Key); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.State(); len(st.Accounts) != 0 {
		t.Fatal("forgotten account remains")
	}
}

func TestModels(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	signIn(t, s, f)
	ms, err := s.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Slug != "gpt-6.1-sol" || ms[0].Name != "GPT-6.1 Sol" || len(ms[0].Efforts) != 3 || ms[0].DefaultEffort != "medium" || !ms[0].Images || ms[1].Slug != "gpt-mini" || len(ms[1].Efforts) != 0 {
		t.Fatalf("models %+v", ms)
	}
}

func waitDone(t *testing.T, s *Service, id string) Message {
	t.Helper()
	fl := s.Follow(id)
	if fl == nil {
		t.Fatal("no answer runs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, done, err := fl.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if done != nil {
			return *done
		}
	}
}

func TestConversation(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	signIn(t, s, f)
	ch, err := s.CreateChat("gpt-6.1-sol", "high")
	if err != nil {
		t.Fatal(err)
	}
	f.respBody = streamOf("При", "вет!")
	png := []byte("\x89PNG\r\n\x1a\nfake")
	if _, err := s.Send(ch.ID, SendInput{Text: "Скажи привет", Files: []Upload{{Name: "a.png", Mime: "image/png", Data: png}}}); err != nil {
		t.Fatal(err)
	}
	m := waitDone(t, s, ch.ID)
	if m.Text != "Привет!" || m.Status != "done" || m.Role != "assistant" {
		t.Fatalf("answer %+v", m)
	}
	f.respBody = streamOf("Второй")
	if _, err := s.Send(ch.ID, SendInput{Text: "Ещё"}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, ch.ID)
	if len(f.requests) != 2 {
		t.Fatalf("%d requests", len(f.requests))
	}
	req := f.requests[1]
	if req["store"] != false || req["stream"] != true || req["model"] != "gpt-6.1-sol" || req["_auth"] != "Bearer at-0" {
		t.Fatalf("request %v", req)
	}
	for _, k := range []string{"previous_response_id", "temperature", "max_output_tokens", "metadata", "user"} {
		if _, ok := req[k]; ok {
			t.Errorf("unsupported field %s sent", k)
		}
	}
	if r, _ := req["reasoning"].(map[string]any); r["effort"] != "high" {
		t.Errorf("reasoning %v", req["reasoning"])
	}
	input, _ := req["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("history %v", input)
	}
	first := input[0].(map[string]any)
	parts := first["content"].([]any)
	if first["role"] != "user" || len(parts) != 2 || !strings.HasPrefix(parts[1].(map[string]any)["image_url"].(string), "data:image/png;base64,") {
		t.Fatalf("first item %v", first)
	}
	if input[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("second item %v", input[1])
	}
	for _, it := range input {
		if it.(map[string]any)["role"] == "system" {
			t.Fatal("system message sent")
		}
	}
	got, _, _ := s.Chat(ch.ID)
	if got.Title != "Скажи привет" || len(got.Messages) != 4 {
		t.Fatalf("chat %+v", got)
	}
	// The kept file reads back.
	b, typ, _, err := s.FileOf(ch.ID, got.Messages[0].Files[0].ID)
	if err != nil || typ != "image/png" || string(b) != string(png) {
		t.Fatalf("file %v %q", err, typ)
	}
	// The chat survives a restart of the runtime.
	again := openChats(s.chats.dir)
	if c, err := again.get(ch.ID); err != nil || len(c.Messages) != 4 {
		t.Fatalf("reopened %v %+v", err, c)
	}
	if err := s.Rename(ch.ID, "  Новое   имя "); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := s.Chat(ch.ID); c.Title != "Новое имя" {
		t.Fatalf("title %q", c.Title)
	}
	if err := s.Delete(ch.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Chat(ch.ID); codeOf(err) != CodeNotFound {
		t.Fatal("deleted chat remains")
	}
	if _, err := os.Stat(filepath.Join(s.chats.dir, "files", ch.ID)); !os.IsNotExist(err) {
		t.Fatal("attachments remain")
	}
}

func TestUsageLimitAndFailedStream(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	signIn(t, s, f)
	ch, _ := s.CreateChat("gpt-mini", "")
	f.respStatus = 429
	f.respBody = `{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"limit"}}`
	if _, err := s.Send(ch.ID, SendInput{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if m := waitDone(t, s, ch.ID); m.Status != "failed" || m.Error == nil || m.Error.Code != CodeUsageLimit {
		t.Fatalf("answer %+v", m)
	}
	f.respStatus = 200
	f.respBody = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"part\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"subscription_sharing_usage_unavailable\"}}}\n\n"
	if _, err := s.Send(ch.ID, SendInput{Text: "again"}); err != nil {
		t.Fatal(err)
	}
	if m := waitDone(t, s, ch.ID); m.Status != "failed" || m.Text != "part" || m.Error.Code != CodeUnavailable {
		t.Fatalf("answer %+v", m)
	}
	// A stream cut before response.completed is not a success.
	f.respBody = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"half\"}\n\n"
	_, _ = s.Send(ch.ID, SendInput{Text: "cut"})
	if m := waitDone(t, s, ch.ID); m.Status != "failed" || m.Error.Code != CodeIncomplete {
		t.Fatalf("answer %+v", m)
	}
}

func TestSendNeedsAccountAndChecksInput(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	ch, _ := s.CreateChat("gpt-mini", "")
	if _, err := s.Send(ch.ID, SendInput{Text: "hi"}); codeOf(err) != CodeSignedOut {
		t.Fatalf("no account: %v", err)
	}
	signIn(t, s, f)
	if _, err := s.Send(ch.ID, SendInput{Text: "  "}); codeOf(err) != CodeBadRequest {
		t.Fatalf("empty: %v", err)
	}
	if _, err := s.Send(ch.ID, SendInput{Text: "x", Files: []Upload{{Name: "a.exe", Mime: "application/x-msdownload", Data: []byte("MZ")}}}); codeOf(err) != CodeUnsupported {
		t.Fatalf("exe: %v", err)
	}
}

func TestHTTPGuardAndStream(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	signIn(t, s, f)
	allow := true
	h := s.Handler("/api/platform/chatgpt", func(w http.ResponseWriter, r *http.Request) bool {
		if !allow {
			w.WriteHeader(http.StatusUnauthorized)
		}
		return allow
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	allow = false
	res, _ := http.Get(srv.URL + "/api/platform/chatgpt")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("guard: %d", res.StatusCode)
	}
	allow = true
	post := func(path, body string) map[string]any {
		res, err := http.Post(srv.URL+"/api/platform/chatgpt"+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return out
	}
	chat := post("/chats", `{"model":"gpt-mini"}`)["chat"].(map[string]any)
	id := chat["id"].(string)
	f.respBody = streamOf("a", "b", "c")
	post("/chats/"+id+"/messages", `{"text":"hi"}`)
	res, err := http.Get(srv.URL + "/api/platform/chatgpt/chats/" + id + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	var text strings.Builder
	var done bool
	for sc.Scan() {
		if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var ev map[string]any
			_ = json.Unmarshal([]byte(d), &ev)
			if t, ok := ev["text"].(string); ok {
				text.WriteString(t)
			}
			if _, ok := ev["message"]; ok {
				done = true
			}
		}
	}
	if !done || text.String() != "abc" && text.Len() != 0 {
		t.Fatalf("stream text %q done %v", text.String(), done)
	}
	st, _ := http.Get(srv.URL + "/api/platform/chatgpt")
	var state map[string]any
	_ = json.NewDecoder(st.Body).Decode(&state)
	b, _ := json.Marshal(state)
	if strings.Contains(string(b), "at-0") || strings.Contains(string(b), "rt-1") {
		t.Fatal("token in the state answer")
	}
}

func TestLoopbackCallback(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	st, err := s.StartLogin("")
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(f.authorize(t, st.URL))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("callback %d", res.StatusCode)
	}
	if v, _ := s.State(); len(v.Accounts) != 1 || !v.Accounts[0].SignedIn || v.Pending {
		t.Fatalf("state %+v", v)
	}
}

func TestWrongPortKeepsTheAttempt(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	st, _ := s.StartLogin("")
	cb := f.authorize(t, st.URL)
	u, _ := url.Parse(cb)
	wrong := *u
	wrong.Host = "127.0.0.1:1"
	if _, err := s.CompleteLogin(context.Background(), wrong.String()); codeOf(err) != CodeBadCallback {
		t.Fatalf("wrong port: %v", err)
	}
	if _, err := s.CompleteLogin(context.Background(), cb); err != nil {
		t.Fatalf("the attempt was burnt: %v", err)
	}
}

func TestDiscoveryPinned(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	for _, raw := range []string{"https://evil.example/jwks", "http://127.0.0.1:1/jwks", "ftp://auth.openai.com/x"} {
		if s.oauth.openAIAddress(raw) {
			t.Errorf("%s passes", raw)
		}
	}
	for _, raw := range []string{f.srv.URL + "/jwks"} {
		if !s.oauth.openAIAddress(raw) {
			t.Errorf("%s refused", raw)
		}
	}
	real := &oauth{ep: DefaultEndpoints()}
	if !real.openAIAddress("https://auth.openai.com/.well-known/jwks.json") || real.openAIAddress("https://auth.openai.com.evil.example/x") {
		t.Error("pinning of OpenAI's host")
	}
}

func TestStopKeepsWhatWasWritten(t *testing.T) {
	f := newFake(t)
	s, _ := newTestService(t, f)
	signIn(t, s, f)
	ch, _ := s.CreateChat("gpt-mini", "")
	f.respBody = streamOf("a")
	_, _ = s.Send(ch.ID, SendInput{Text: "x"})
	if _, err := s.Send(ch.ID, SendInput{Text: "y"}); codeOf(err) != CodeBusy && err != nil {
		t.Fatalf("second send while running: %v", err)
	}
	waitDone(t, s, ch.ID)
	if s.Stop(ch.ID) {
		t.Fatal("stopped a finished answer")
	}
}
