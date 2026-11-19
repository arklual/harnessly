package mediagen

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// fakeHF is a Higgsfield API: it keeps what it was asked and answers like docs.higgsfield.ai says.
type fakeHF struct {
	t        *testing.T
	srv      *httptest.Server
	key      string
	price    string
	polls    atomic.Int32
	doneAt   atomic.Int32 // the poll that finds the request completed
	mu       sync.Mutex
	submits  []map[string]any
	idem     []string
	canceled atomic.Bool
	uploads  atomic.Int32
	status   string // a terminal status other than completed
	started  atomic.Bool
}

func newFake(t *testing.T) *fakeHF {
	f := &fakeHF{t: t, key: "kid:ksecret", price: "0.42"}
	f.doneAt.Store(2)
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Key "+f.key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"detail":"Invalid credentials"}`)
			return false
		}
		return true
	}
	mux.HandleFunc("/estimate/", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = io.WriteString(w, `{"usd":"`+f.price+`"}`)
		}
	})
	mux.HandleFunc("/files/generate-upload-url", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = io.WriteString(w, `{"public_url":"`+f.srv.URL+`/cdn/frame.png","upload_url":"`+f.srv.URL+`/put/frame","upload_headers":{"Content-Type":"image/png","x-amz-tagging":"retention=temporary"}}`)
		}
	})
	mux.HandleFunc("/put/frame", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("the key went to the storage")
		}
		f.uploads.Add(1)
	})
	mux.HandleFunc("/requests/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			if f.started.Load() {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.canceled.Store(true)
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(r.URL.Path, "/status"):
			n := f.polls.Add(1)
			switch {
			case f.canceled.Load():
				_, _ = io.WriteString(w, `{"status":"canceled","request_id":"rid-1"}`)
			case f.status != "":
				_, _ = io.WriteString(w, `{"status":"`+f.status+`","request_id":"rid-1","error":"Generation failed"}`)
			case n >= f.doneAt.Load():
				_, _ = io.WriteString(w, `{"status":"completed","request_id":"rid-1","video":{"url":"`+f.srv.URL+`/cdn/clip.mp4"}}`)
			default:
				_, _ = io.WriteString(w, `{"status":"in_progress","request_id":"rid-1"}`)
			}
		}
	})
	mux.HandleFunc("/cdn/clip.mp4", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("the key went to the CDN")
		}
		_, _ = io.WriteString(w, "FAKE-MP4-BYTES")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.submits = append(f.submits, body)
		f.idem = append(f.idem, r.Header.Get("Idempotency-Key"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"queued","request_id":"rid-1"}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type fakeSecrets map[string]string

func (f fakeSecrets) Has(n string) bool { _, ok := f[n]; return ok }
func (f fakeSecrets) Reveal(n string) (string, error) {
	return f[n], nil
}

func service(t *testing.T, f *fakeHF) *Service {
	s := &Service{Dir: t.TempDir(), Secrets: fakeSecrets{SecretName: f.key}, AllowHTTP: true, PollFirst: time.Millisecond, PollMax: 2 * time.Millisecond}
	// The safe's key goes to the fake instead of api.higgsfield.ai.
	s.Registry = fakeRegistry{base: f.srv.URL, key: f.key}
	return s
}

type fakeRegistry struct{ base, key string }

func (r fakeRegistry) List() ([]extproviders.View, error) {
	return []extproviders.View{{ID: "higgsfield", Name: "Higgsfield", API: extproviders.APIHiggsfield, HasKey: r.key != "", Default: true}}, nil
}
func (r fakeRegistry) Get(id string) (extproviders.Provider, error) {
	return extproviders.Provider{ID: id, Name: "Higgsfield", BaseURL: r.base, API: extproviders.APIHiggsfield, KeyRef: "k"}, nil
}
func (r fakeRegistry) Key(extproviders.Provider) (string, error) { return r.key, nil }

func writePNG(t *testing.T, dir string) string {
	p := filepath.Join(dir, "frame.png")
	if err := os.WriteFile(p, []byte("\x89PNG fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImageToVideoEndToEnd(t *testing.T) {
	f := newFake(t)
	s := service(t, f)
	work := t.TempDir()
	frame := writePNG(t, work)
	copyTo := filepath.Join(work, "clips")
	var states []State
	var mu sync.Mutex
	s.Changed = func(sn Snapshot) { mu.Lock(); states = append(states, sn.State); mu.Unlock() }
	snap, err := s.Start(Request{Kind: extproviders.KindVideo, Model: "kling", Prompt: "slow push in", Frames: []string{frame},
		Seconds: 5, Conv: "chat-1", CopyTo: copyTo, Name: "scene-a", SaveOK: func(string) bool { return true },
		FrameOK: func(string) bool { return true }, Origin: map[string]any{"scene": "retro_convertible", "camera": "Cam.Hero"}})
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != Queued {
		t.Fatalf("first state %s", snap.State)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	end, _ := s.Wait(ctx, snap.ID)
	if end.State != Done {
		t.Fatalf("state %s: %s", end.State, end.Error)
	}
	if end.EstimateUSD != 0.42 || !strings.Contains(end.Meta, "$0.42") {
		t.Errorf("price not on the job: %v %q", end.EstimateUSD, end.Meta)
	}
	if len(end.Files) != 1 || !strings.HasSuffix(end.Files[0].Copy, "scene-a-1.mp4") {
		t.Fatalf("files %+v", end.Files)
	}
	if b, _ := os.ReadFile(end.Files[0].Copy); string(b) != "FAKE-MP4-BYTES" {
		t.Errorf("clip %q", b)
	}
	// The frame went up to the storage and the request names its public address.
	if f.uploads.Load() != 1 {
		t.Errorf("uploads %d", f.uploads.Load())
	}
	if len(f.submits) != 1 {
		t.Fatalf("submits %d", len(f.submits))
	}
	body := f.submits[0]
	if body["image_url"] != f.srv.URL+"/cdn/frame.png" || body["prompt"] != "slow push in" || body["duration"] != float64(5) || body["sound"] != "off" {
		t.Errorf("body %+v", body)
	}
	if !strings.HasPrefix(f.idem[0], "harnessly-") {
		t.Errorf("idempotency key %q", f.idem[0])
	}
	// The origin file says where the clip came from.
	raw, err := os.ReadFile(end.Files[0].OriginFile)
	if err != nil {
		t.Fatal(err)
	}
	var org Origin
	if err := json.Unmarshal(raw, &org); err != nil {
		t.Fatal(err)
	}
	if org.Provider != "higgsfield" || org.Endpoint != "kling-video/v3.0/std/image-to-video" || org.Prompt != "slow push in" ||
		len(org.Sources) != 1 || org.Sources[0] != frame || org.RequestID != "rid-1" || org.CostUSD != 0.42 || org.Extra["scene"] != "retro_convertible" {
		t.Errorf("origin %+v", org)
	}
	mu.Lock()
	defer mu.Unlock()
	if states[0] != Queued || states[len(states)-1] != Done {
		t.Errorf("states %v", states)
	}
	// The job is read back by a new service of the same folder.
	again := &Service{Dir: s.Dir}
	if got, ok := again.Get(snap.ID); !ok || got.State != Done || len(got.Files) != 1 {
		t.Errorf("reloaded %+v %v", got, ok)
	}
}

func TestOverCapIsNotSent(t *testing.T) {
	f := newFake(t)
	f.price = "3.50"
	s := service(t, f)
	snap, err := s.Start(Request{Prompt: "a car drives", Model: "ltx"})
	if err != nil {
		t.Fatal(err)
	}
	end, _ := s.Wait(context.Background(), snap.ID)
	if end.State != Failed || !strings.Contains(end.Error, "over the cap") || !strings.Contains(end.Error, "Nothing was sent") {
		t.Fatalf("%s: %s", end.State, end.Error)
	}
	if len(f.submits) != 0 {
		t.Errorf("a job over its cap was sent")
	}
	// With the person's yes the cap is raised.
	snap, _ = s.Start(Request{Prompt: "a car drives", Model: "ltx", MaxUSD: 5})
	if end, _ = s.Wait(context.Background(), snap.ID); end.State != Done {
		t.Fatalf("%s: %s", end.State, end.Error)
	}
}

func TestDailyCap(t *testing.T) {
	f := newFake(t)
	f.price = "1.50"
	s := service(t, f)
	s.DailyUSD = 2
	for i, want := range []State{Done, Failed} {
		snap, err := s.Start(Request{Prompt: "x clip", Model: "ltx"})
		if err != nil {
			t.Fatal(err)
		}
		end, _ := s.Wait(context.Background(), snap.ID)
		if end.State != want {
			t.Fatalf("job %d: %s %s", i, end.State, end.Error)
		}
	}
}

func TestNoKey(t *testing.T) {
	s := &Service{Dir: t.TempDir(), Registry: fakeRegistry{}, Secrets: fakeSecrets{}}
	if _, err := s.Start(Request{Prompt: "x clip"}); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err %v", err)
	}
	// What the agent is told: the way out through the safe, not a crash.
	out := Tools{S: s}.Call(context.Background(), ToolGenerate, jsvalue.NewObject("prompt", "a clip"))
	text := toolText(out)
	if !strings.Contains(text, "harnessly secret ask higgsfield-key") || !strings.Contains(text, "Settings → Providers") {
		t.Errorf("answer %q", text)
	}
}

func toolText(o *jsvalue.Object) string {
	v, _ := o.Get("content")
	list, _ := v.([]any)
	var b strings.Builder
	for _, c := range list {
		if co, ok := c.(*jsvalue.Object); ok {
			b.WriteString(str(co, "text"))
		}
	}
	return b.String()
}

func TestKeyFromTheSafeWithoutAProvider(t *testing.T) {
	f := newFake(t)
	s := &Service{Dir: t.TempDir(), Secrets: fakeSecrets{SecretName: f.key}, AllowHTTP: true}
	tg, err := s.resolve("")
	if err != nil || tg.key != f.key || tg.base != extproviders.HiggsfieldBase {
		t.Fatalf("%+v %v", tg, err)
	}
	s.Secrets = fakeSecrets{SecretName: "not-a-pair"}
	if _, err := s.resolve(""); !errors.Is(err, ErrNoKey) {
		t.Errorf("a key that is no id:secret was taken: %v", err)
	}
}

func TestRefusedKeyAndModeration(t *testing.T) {
	f := newFake(t)
	s := service(t, f)
	s.Registry = fakeRegistry{base: f.srv.URL, key: "bad:key"}
	snap, _ := s.Start(Request{Prompt: "x clip"})
	end, _ := s.Wait(context.Background(), snap.ID)
	if end.State != Failed || !strings.Contains(end.Error, "did not accept the key") {
		t.Errorf("%s: %s", end.State, end.Error)
	}
	s = service(t, f)
	f.status = "nsfw"
	snap, _ = s.Start(Request{Prompt: "x clip"})
	end, _ = s.Wait(context.Background(), snap.ID)
	if end.State != Failed || !strings.Contains(end.Error, "moderation") {
		t.Errorf("%s: %s", end.State, end.Error)
	}
}

func TestCancelWhileQueued(t *testing.T) {
	f := newFake(t)
	f.doneAt.Store(1 << 30)
	s := service(t, f)
	snap, _ := s.Start(Request{Prompt: "a clip"})
	for i := 0; i < 500; i++ {
		if got, _ := s.Get(snap.ID); got.State == Running {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	end, err := s.Cancel(context.Background(), snap.ID)
	if err != nil || end.State != Canceled || !f.canceled.Load() {
		t.Fatalf("%+v %v canceled=%v", end, err, f.canceled.Load())
	}
	// A request the provider has begun cannot be stopped; the job goes on.
	f2 := newFake(t)
	f2.doneAt.Store(1 << 30)
	f2.started.Store(true)
	s2 := service(t, f2)
	snap, _ = s2.Start(Request{Prompt: "a clip"})
	for i := 0; i < 500; i++ {
		if got, _ := s2.Get(snap.ID); got.State == Running {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := s2.Cancel(context.Background(), snap.ID); err == nil || !strings.Contains(err.Error(), "cannot be canceled") {
		t.Errorf("cancel of a running request: %v", err)
	}
	// The job goes on to its end before the test's directory is removed under it.
	f2.doneAt.Store(0)
	for i := 0; i < 2500; i++ {
		if got, _ := s2.Get(snap.ID); got.State.Ended() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Error("the request that could not be canceled never ended")
}

func TestPlanChecksTheModelsLimits(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		want string
	}{
		{"image model without frames", Request{Prompt: "p", Model: "kling-video/v3.0/std/image-to-video"}, "give frames"},
		{"text model with frames", Request{Prompt: "p", Model: "kling-video/v3.0/std/text-to-video", Frames: []string{"https://x.test/a.png"}}, "takes no picture"},
		{"seconds out of range", Request{Prompt: "p", Model: "ltx", Seconds: 7}, "takes [6 8 10]"},
		{"no last frame", Request{Prompt: "p", Model: "kling", Frames: []string{"https://x.test/a.png", "https://x.test/b.png"}}, "first frame only"},
		{"empty prompt", Request{Model: "ltx"}, "prompt is empty"},
		{"not a model", Request{Prompt: "p", Model: "Not A Model"}, "no model"},
		{"picture model for video", Request{Prompt: "p", Model: "soul"}, "no model"},
	}
	for _, c := range cases {
		r, err := Normalize(c.req)
		if err == nil {
			_, err = makePlan(r)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (want %q)", c.name, err, c.want)
		}
	}
	// The cheapest default: no seconds → about five (Kling), the first length (LTX).
	r, _ := Normalize(Request{Prompt: "p", Model: "ltx"})
	pl, err := makePlan(r)
	if err != nil || pl.body["duration"] != 6 || pl.body["generate_audio"] != false {
		t.Errorf("%+v %v", pl.body, err)
	}
	r, _ = Normalize(Request{Prompt: "p", Frames: []string{"https://x.test/a.png"}})
	if pl, err = makePlan(r); err != nil || pl.endpoint != extproviders.DefaultVideoModel || pl.body["duration"] != 5 {
		t.Errorf("%+v %v", pl, err)
	}
	// A model of the provider's the catalog lacks goes by its ID with the common fields.
	r, _ = Normalize(Request{Prompt: "p", Model: "wan/v2.7/text-to-video", Seconds: 4})
	if pl, err = makePlan(r); err != nil || pl.endpoint != "wan/v2.7/text-to-video" || pl.body["duration"] != 4 {
		t.Errorf("%+v %v", pl, err)
	}
}

func TestFrameFilesNeedLeave(t *testing.T) {
	f := newFake(t)
	s := service(t, f)
	frame := writePNG(t, t.TempDir())
	if _, err := s.Start(Request{Prompt: "p clip", Frames: []string{frame}}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("a frame file with no reader check was taken: %v", err)
	}
	if _, err := s.Start(Request{Prompt: "p clip", Frames: []string{"relative.png"}, FrameOK: func(string) bool { return true }}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("a relative frame: %v", err)
	}
	if _, err := s.Start(Request{Prompt: "p clip", Frames: []string{frame}, FrameOK: func(string) bool { return false }}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("a frame outside the readable places: %v", err)
	}
}

func TestToolReportReadsAsACard(t *testing.T) {
	f := newFake(t)
	s := service(t, f)
	out := Tools{S: s}.Call(context.Background(), ToolGenerate, jsvalue.NewObject("prompt", "a clip", "model", "ltx"))
	text := toolText(out)
	if !strings.HasPrefix(text, "job_id: ") || !strings.Contains(text, "Done: 1 video(s)") || !strings.Contains(text, "1. ") || !strings.Contains(text, ".mp4") {
		t.Errorf("report %q", text)
	}
	est := Tools{S: s}.Call(context.Background(), ToolGenerate, jsvalue.NewObject("prompt", "a clip", "estimate_only", true))
	if et := toolText(est); !strings.Contains(et, "$0.42") || !strings.Contains(et, "Nothing was made") {
		t.Errorf("estimate %q", et)
	}
	n := len(f.submits)
	_ = Tools{S: s}.Call(context.Background(), ToolGenerate, jsvalue.NewObject("prompt", "a clip", "estimate_only", true))
	if len(f.submits) != n {
		t.Errorf("an estimate was sent as a request")
	}
}
