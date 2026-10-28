package chatmove

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var secretA, secretB = []byte("source-secret-source-secret-0001"), []byte("target-secret-target-secret-0002")

func TestEnvelopeRoundTripAndBindings(t *testing.T) {
	env, err := Encrypt(secretA, "op1", Export, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Decrypt(secretA, "op1", Export, env); err != nil || string(got) != `{"v":1}` {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if strings.Contains(env.CT, "v") && strings.Contains(string(mustJSON(env)), `{"v":1}`) {
		t.Fatal("plaintext is visible in the envelope")
	}
	for name, try := range map[string]func() error{
		"another secret":    func() error { _, e := Decrypt(secretB, "op1", Export, env); return e },
		"another operation": func() error { _, e := Decrypt(secretA, "op2", Export, env); return e },
		"another direction": func() error { _, e := Decrypt(secretA, "op1", Import, env); return e },
		"tampered": func() error {
			bad := env
			bad.CT = "AAAA" + env.CT[4:]
			_, e := Decrypt(secretA, "op1", Export, bad)
			return e
		},
		"another cipher": func() error { bad := env; bad.Enc = "none"; _, e := Decrypt(secretA, "op1", Export, bad); return e },
	} {
		if try() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// The bundle names no account, provider, model, session, machine or path of the source: its fields are closed.
func TestBundleHasNoIdentityFields(t *testing.T) {
	raw := mustJSON(Bundle{V: 1, Title: "t", History: "{}\n", Project: &Project{RepoKey: "h/r", RelDir: "web", Commit: "abcdef1"}})
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for k := range keys {
		if k != "v" && k != "title" && k != "history" && k != "project" {
			t.Errorf("unexpected bundle field %q", k)
		}
	}
	var project map[string]json.RawMessage
	_ = json.Unmarshal(keys["project"], &project)
	for k := range project {
		if k != "repoKey" && k != "relDir" && k != "commit" {
			t.Errorf("unexpected project field %q", k)
		}
	}
	for _, field := range []string{"provider", "model", "account", "session", "sessionId", "machine", "machineId", "cwd", "email", "profile"} {
		extra := strings.Replace(string(raw), `{"v":1`, `{"v":1,"`+field+`":"x"`, 1)
		if _, err := DecodeBundle([]byte(extra)); err == nil {
			t.Errorf("a bundle with %q was accepted", field)
		}
	}
	if _, err := DecodeBundle(raw); err != nil {
		t.Fatalf("a good bundle was refused: %v", err)
	}
}

func TestBundleValidation(t *testing.T) {
	good := Bundle{V: 1, History: "x\n", Project: &Project{RepoKey: "h/r", Commit: "abcdef1"}}
	bad := map[string]Bundle{
		"version":      {V: 2, History: "x"},
		"empty":        {V: 1},
		"commit":       {V: 1, History: "x", Project: &Project{RepoKey: "h/r", Commit: "not-hex!"}},
		"relative dir": {V: 1, History: "x", Project: &Project{RepoKey: "h/r", Commit: "abcdef1", RelDir: "../x"}},
		"absolute dir": {V: 1, History: "x", Project: &Project{RepoKey: "h/r", Commit: "abcdef1", RelDir: "/etc"}},
	}
	if good.Validate() != nil {
		t.Fatal("good bundle refused")
	}
	for name, b := range bad {
		if b.Validate() == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@gitlab.example.com:harnessly/harnessly.git":         "gitlab.example.com/harnessly/harnessly",
		"https://gitlab.example.com/harnessly/harnessly.git":     "gitlab.example.com/harnessly/harnessly",
		"ssh://git@gitlab.example.com:2222/Harnessly/Harnessly/": "gitlab.example.com/harnessly/harnessly",
		"https://user:pw@github.com/a/b":                         "github.com/a/b",
		"":                                                       "",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// --- a repository for the tests ---------------------------------------------------------------------------------

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func repo(t *testing.T, dir, remote string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "a.txt")
	run(t, dir, "commit", "-q", "-m", "one")
	return run(t, dir, "rev-parse", "HEAD")
}

// --- fake conversations -------------------------------------------------------------------------------------------

type fakeConvs struct {
	mu      sync.Mutex
	info    map[string]Info
	history string
	fence   string
	opens   []Open
	failOpn error
	deleted []string
	nextID  int
}

func newFake(cwd string) *fakeConvs {
	return &fakeConvs{info: map[string]Info{"tab-src": {Title: "Work", Cwd: cwd, HasHistory: true}}, history: `{"kind":"message","role":"user","content":"plain-history-marker"}` + "\n", fence: "100"}
}

func (f *fakeConvs) Info(id string) (Info, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.info[id]
	return i, ok
}
func (f *fakeConvs) Snapshot(string, int64) ([]byte, string, error) {
	return []byte(f.history), f.fence, nil
}
func (f *fakeConvs) Fence(string) (string, bool) { return f.fence, true }
func (f *fakeConvs) Open(in Open) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOpn != nil {
		return "", f.failOpn
	}
	f.nextID++
	id := "tab-new" + strconv.Itoa(f.nextID)
	f.opens = append(f.opens, in)
	f.info[id] = Info{Title: in.Title, Cwd: in.Cwd}
	return id, nil
}
func (f *fakeConvs) Delete(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.info, id)
	f.deleted = append(f.deleted, id)
}

var offers = []Agent{{Provider: "claude", Short: "Claude", Models: []Model{{ID: "opus", Label: "Opus", Default: true}}}}

func service(t *testing.T, conv *fakeConvs, secret []byte, roots ...string) *Service {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "moves.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Conv: conv, Agents: func() []Agent { return offers }, Roots: roots, Secret: secret, Store: store, Home: t.TempDir()}
}

func exportFor(t *testing.T, s *Service, op string, without bool) ExportResponse {
	t.Helper()
	out, ref := s.Export(context.Background(), op, "tab-src", without)
	if ref != nil {
		t.Fatalf("export refused: %+v", ref)
	}
	return out
}

// reseal is what the cluster does between two runtimes: opens the source's envelope and seals it for the target.
func reseal(t *testing.T, op string, e Envelope) Envelope {
	t.Helper()
	plain, err := Decrypt(secretA, op, Export, e)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Encrypt(secretB, op, Import, plain)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPreflightChecks(t *testing.T) {
	base := t.TempDir()
	commit := repo(t, filepath.Join(base, "proj"), "https://host.example/team/proj.git")
	s := service(t, newFake(""), secretB, base)
	p := Project{RepoKey: "host.example/team/proj", Commit: commit}

	got := s.Preflight(context.Background(), PreflightRequest{Project: &p, Provider: "claude", Model: "opus"})
	if !got.FilesOK || firstFailed(got.Checks) != nil || len(got.Agents) != 1 {
		t.Fatalf("ready target: %+v", got)
	}
	cases := map[string]struct {
		req  PreflightRequest
		code string
	}{
		"project": {PreflightRequest{Project: &Project{RepoKey: "host.example/other", Commit: commit}}, CodeNoProject},
		"commit":  {PreflightRequest{Project: &Project{RepoKey: p.RepoKey, Commit: strings.Repeat("a", 40)}}, CodeCommit},
		"agent":   {PreflightRequest{Provider: "codex", Model: "x"}, CodeAgent},
		"model":   {PreflightRequest{Provider: "claude", Model: "nope"}, CodeModel},
	}
	for name, c := range cases {
		got := s.Preflight(context.Background(), c.req)
		bad := firstFailed(got.Checks)
		if bad == nil || bad.Code != c.code {
			t.Errorf("%s: %+v", name, got.Checks)
		}
	}
	// A dirty checkout blocks the files, and only them.
	if err := os.WriteFile(filepath.Join(base, "proj", "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = s.Preflight(context.Background(), PreflightRequest{Project: &p})
	if got.FilesOK || firstFailed(got.Checks).Code != CodeDirty {
		t.Fatalf("dirty target: %+v", got)
	}
}

func TestExportRefusals(t *testing.T) {
	base := t.TempDir()
	repo(t, filepath.Join(base, "proj"), "git@host.example:team/proj.git")
	conv := newFake(filepath.Join(base, "proj"))
	s := service(t, conv, secretA, base)

	conv.info["tab-src"] = Info{Title: "x", Cwd: filepath.Join(base, "proj"), HasHistory: true, Busy: true}
	if _, ref := s.Export(context.Background(), "op1", "tab-src", false); ref == nil || ref.Code != CodeBusy {
		t.Fatalf("busy: %+v", ref)
	}
	conv.info["tab-src"] = Info{Title: "x", Cwd: filepath.Join(base, "proj")}
	if _, ref := s.Export(context.Background(), "op1", "tab-src", false); ref == nil || ref.Code != CodeNoHistory {
		t.Fatalf("no history: %+v", ref)
	}
	conv.info["tab-src"] = Info{Title: "x", Cwd: filepath.Join(base, "proj"), HasHistory: true}
	if err := os.WriteFile(filepath.Join(base, "proj", "new.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ref := s.Export(context.Background(), "op1", "tab-src", false)
	if ref == nil || ref.Code != CodeDirtySource || ref.Status != http.StatusConflict {
		t.Fatalf("uncommitted changes must be refused: %+v", ref)
	}
	// Without local files the same chat goes, and the bundle names no project.
	out, ref := s.Export(context.Background(), "op1", "tab-src", true)
	if ref != nil {
		t.Fatalf("without files: %+v", ref)
	}
	plain, err := Decrypt(secretA, "op1", Export, out.BundleEnc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeBundle(plain)
	if err != nil || b.Project != nil {
		t.Fatalf("bundle %+v %v", b, err)
	}
	if _, ref := s.Export(context.Background(), "op1", "tab-missing", true); ref == nil || ref.Status != http.StatusNotFound {
		t.Fatalf("missing: %+v", ref)
	}
}

func TestMoveEndToEndIsIdempotentAndLeavesNoIdentity(t *testing.T) {
	base := t.TempDir()
	srcDir := filepath.Join(base, "src", "proj")
	commit := repo(t, srcDir, "git@host.example:team/proj.git")
	dstDir := filepath.Join(base, "dst", "proj")
	run(t, base, "clone", "-q", srcDir, dstDir)
	run(t, dstDir, "remote", "set-url", "origin", "https://host.example/team/proj")

	src := service(t, newFake(srcDir), secretA, filepath.Join(base, "src"))
	dstConv := newFake("")
	dst := service(t, dstConv, secretB, filepath.Join(base, "dst"))

	exp := exportFor(t, src, "op-1", false)
	if exp.Fence != "100" {
		t.Fatalf("fence %q", exp.Fence)
	}
	req := ImportRequest{OpID: "op-1", BundleEnc: reseal(t, "op-1", exp.BundleEnc), Provider: "claude", Model: ModelRef{ID: "opus"}}
	first, ref := dst.Import(context.Background(), req)
	if ref != nil || first.Existing {
		t.Fatalf("import: %+v %+v", first, ref)
	}
	again, ref := dst.Import(context.Background(), req) // a double click
	if ref != nil || !again.Existing || again.ConversationID != first.ConversationID {
		t.Fatalf("second import: %+v %+v", again, ref)
	}
	if len(dstConv.opens) != 1 {
		t.Fatalf("one op must start one session, started %d", len(dstConv.opens))
	}
	open := dstConv.opens[0]
	wantTree := filepath.Join(dstDir, ".worktrees", "move-op-1")
	if open.Cwd != wantTree {
		t.Fatalf("the session starts in %q, want the target's own worktree %q", open.Cwd, wantTree)
	}
	if head := run(t, wantTree, "rev-parse", "HEAD"); head != commit {
		t.Fatalf("worktree at %s, want %s", head, commit)
	}
	if open.Title != "Work" || string(open.History) != dstConv.history && !strings.Contains(string(open.History), `"role":"user"`) {
		t.Fatalf("open %+v", open)
	}
	// What the target's session gets names nothing of the source.
	for _, secret := range []string{string(secretA), srcDir, "tab-src"} {
		if strings.Contains(string(open.History), secret) || strings.Contains(open.Title, secret) || strings.Contains(open.Cwd, "src/proj") {
			t.Errorf("the session carries %q", secret)
		}
	}

	// The source: held, then sealed; a sealed one takes no messages and a second op cannot seal it.
	if ref := src.Hold("op-1", "tab-src", exp.Fence); ref != nil {
		t.Fatalf("hold: %+v", ref)
	}
	if !src.Store.Blocked("tab-src") {
		t.Fatal("a held source takes messages")
	}
	to := Pointer{MachineID: "vm2", Name: "Ромина ВМ", ConversationID: first.ConversationID}
	if ref := src.Seal("op-1", "tab-src", to); ref != nil {
		t.Fatalf("seal: %+v", ref)
	}
	if ref := src.Seal("op-1", "tab-src", to); ref != nil {
		t.Fatalf("seal is idempotent: %+v", ref)
	}
	if p := src.Store.Sealed("tab-src"); p == nil || p.Name != "Ромина ВМ" || !src.Store.Blocked("tab-src") {
		t.Fatalf("pointer %+v", p)
	}
	if ref := src.Seal("op-2", "tab-src", to); ref == nil || ref.Code != CodeAlreadyMoved {
		t.Fatalf("another op sealed a moved chat: %+v", ref)
	}
	if _, ref := src.Export(context.Background(), "op-3", "tab-src", false); ref == nil || ref.Code != CodeAlreadyMoved {
		t.Fatalf("a moved chat exported again: %+v", ref)
	}
}

// Anything that fails before the switch leaves the source active and the target clean.
func TestFailuresBeforeTheSwitchLeaveTheSourceActive(t *testing.T) {
	base := t.TempDir()
	srcDir := filepath.Join(base, "src", "proj")
	repo(t, srcDir, "git@host.example:team/proj.git")
	dstDir := filepath.Join(base, "dst", "proj")
	run(t, base, "clone", "-q", srcDir, dstDir)
	run(t, dstDir, "remote", "set-url", "origin", "git@host.example:team/proj.git")
	srcConv := newFake(srcDir)
	src := service(t, srcConv, secretA, filepath.Join(base, "src"))
	dstConv := newFake("")
	dst := service(t, dstConv, secretB, filepath.Join(base, "dst"))
	exp := exportFor(t, src, "op-f", false)
	req := ImportRequest{OpID: "op-f", BundleEnc: reseal(t, "op-f", exp.BundleEnc), Provider: "claude", Model: ModelRef{ID: "opus"}}

	// The target's agent fails to start: no worktree, no branch, no op left.
	dstConv.failOpn = ErrAgent
	if _, ref := dst.Import(context.Background(), req); ref == nil || ref.Code != CodeAgent {
		t.Fatalf("import: %+v", ref)
	}
	if _, err := os.Stat(filepath.Join(dstDir, ".worktrees", "move-op-f")); err == nil {
		t.Fatal("a failed import left its worktree")
	}
	if out := run(t, dstDir, "branch", "--list", "agent/move-op-f"); out != "" {
		t.Fatalf("a failed import left its branch: %q", out)
	}
	if src.Store.Blocked("tab-src") {
		t.Fatal("the source was touched")
	}
	// A wrong bundle, a wrong model and a wrong agent are refused before anything is made.
	bad := req
	bad.BundleEnc.CT = "AAAA" + bad.BundleEnc.CT[4:]
	if _, ref := dst.Import(context.Background(), bad); ref == nil || ref.Code != CodeBadBundle {
		t.Fatalf("tampered: %+v", ref)
	}
	dstConv.failOpn = nil
	bad = req
	bad.Model.ID = "nope"
	if _, ref := dst.Import(context.Background(), bad); ref == nil || ref.Code != CodeModel {
		t.Fatalf("model: %+v", ref)
	}
	if len(dstConv.opens) != 0 {
		t.Fatal("a refused import started a session")
	}
	// A project the target does not have: refused with the failing check, so the person is offered «без локальных файлов».
	noRepo := service(t, newFake(""), secretB, filepath.Join(base, "nowhere"))
	if _, ref := noRepo.Import(context.Background(), req); ref == nil || ref.Code != CodeNoProject || ref.Status != http.StatusConflict {
		t.Fatalf("no repository: %+v", ref)
	}
	noFiles := req
	noFiles.WithoutFiles = true
	got, ref := noRepo.Import(context.Background(), noFiles)
	if ref != nil || got.ConversationID == "" {
		t.Fatalf("without files: %+v %+v", got, ref)
	}

	// The source changed after the export: the hold fails and nothing is held.
	srcConv.fence = "101"
	if ref := src.Hold("op-f", "tab-src", exp.Fence); ref == nil || ref.Code != CodeChanged {
		t.Fatalf("hold: %+v", ref)
	}
	srcConv.fence = "100"
	srcConv.info["tab-src"] = Info{Title: "Work", Cwd: srcDir, HasHistory: true, Busy: true}
	if ref := src.Hold("op-f", "tab-src", exp.Fence); ref == nil || ref.Code != CodeBusy {
		t.Fatalf("busy hold: %+v", ref)
	}
	if src.Store.Blocked("tab-src") {
		t.Fatal("a failed hold blocked the source")
	}

	// Discard removes what an import made; it is idempotent. A release lifts a hold.
	srcConv.info["tab-src"] = Info{Title: "Work", Cwd: srcDir, HasHistory: true}
	ok, ref := dst.Import(context.Background(), req)
	if ref != nil {
		t.Fatalf("import: %+v", ref)
	}
	if ref := dst.Discard(context.Background(), "op-f"); ref != nil {
		t.Fatal(ref)
	}
	if _, alive := dstConv.Info(ok.ConversationID); alive {
		t.Fatal("discard left the conversation")
	}
	if _, err := os.Stat(filepath.Join(dstDir, ".worktrees", "move-op-f")); err == nil {
		t.Fatal("discard left the worktree")
	}
	if ref := dst.Discard(context.Background(), "op-f"); ref != nil {
		t.Fatal(ref)
	}
	if ref := src.Hold("op-f", "tab-src", exp.Fence); ref != nil {
		t.Fatalf("hold: %+v", ref)
	}
	if ref := src.Release("op-f", "tab-src"); ref != nil || src.Store.Blocked("tab-src") {
		t.Fatalf("release: %+v", ref)
	}
}

func TestHoldRunsOut(t *testing.T) {
	conv := newFake("")
	s := service(t, conv, secretA)
	now := time.Now()
	s.Store.now = func() time.Time { return now }
	if ref := s.Hold("op-h", "tab-src", "100"); ref != nil {
		t.Fatal(ref)
	}
	if !s.Store.Blocked("tab-src") {
		t.Fatal("not held")
	}
	now = now.Add(HoldTTL + time.Second)
	if s.Store.Blocked("tab-src") {
		t.Fatal("a hold nobody sealed froze the source for good")
	}
}

func TestStoreSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	a, _ := OpenStore(path)
	if err := a.seal("tab-a", "op", Pointer{MachineID: "vm", ConversationID: "tab-b"}); err != nil {
		t.Fatal(err)
	}
	if err := a.putOp("op", opRecord{TargetID: "tab-b"}); err != nil {
		t.Fatal(err)
	}
	b, err := OpenStore(path)
	if err != nil || !b.Blocked("tab-a") || b.Sealed("tab-a").ConversationID != "tab-b" {
		t.Fatalf("%v", err)
	}
	if r, ok := b.op("op"); !ok || r.TargetID != "tab-b" {
		t.Fatal("op lost")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	var nilStore *Store
	if nilStore.Blocked("x") || nilStore.Sealed("x") != nil {
		t.Fatal("a nil store blocks")
	}
}

func TestHandlerSteps(t *testing.T) {
	conv := newFake("")
	s := service(t, conv, secretA)
	h := s.Handler()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		return w
	}
	if w := call("GET", "/api/v2/move/describe", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET %d", w.Code)
	}
	if w := call("POST", "/api/v2/move/nope", `{}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown step %d", w.Code)
	}
	if w := call("POST", "/api/v2/move/describe", `{"conversationId":"tab-missing"}`); w.Code != http.StatusNotFound {
		t.Fatalf("describe %d", w.Code)
	}
	w := call("POST", "/api/v2/move/describe", `{"conversationId":"tab-src"}`)
	var d DescribeResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Title != "Work" || !d.HasHistory {
		t.Fatalf("describe %d %s", w.Code, w.Body)
	}
	w = call("POST", "/api/v2/move/export", `{"opId":"op-x","conversationId":"tab-src","withoutFiles":true}`)
	var e ExportResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.BundleEnc.Enc != "aes-256-gcm" {
		t.Fatalf("export %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "plain-history-marker") {
		t.Fatal("the history travels in clear")
	}
	if w := call("POST", "/api/v2/move/hold", `{"opId":"op-x","conversationId":"tab-src","fence":"100"}`); w.Code != 200 {
		t.Fatalf("hold %d", w.Code)
	}
	if w := call("POST", "/api/v2/move/seal", `{"opId":"op-x","conversationId":"tab-src","to":{"machineId":"vm2","name":"B","conversationId":"tab-n"}}`); w.Code != 200 {
		t.Fatalf("seal %d %s", w.Code, w.Body)
	}
	if w := call("POST", "/api/v2/move/export", `{"opId":"op-y","conversationId":"tab-src"}`); w.Code != http.StatusConflict {
		t.Fatalf("a moved chat was exported: %d", w.Code)
	}
}
