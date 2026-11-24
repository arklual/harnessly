package mediagen

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/extproviders"
)

// fakeMCP is Higgsfield's MCP server as the connection answers it: tools/call results with text or structured content.
type fakeMCP struct {
	mu        sync.Mutex
	calls     []string
	args      map[string]map[string]any
	credits   float64
	polls     int
	unlim     bool
	noPlan    bool
	cdn       *httptest.Server
	connected bool
}

func newFakeMCP(t *testing.T) *fakeMCP {
	f := &fakeMCP{credits: 6, connected: true, args: map[string]map[string]any{}}
	f.cdn = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			_, _ = io.Copy(io.Discard, r.Body)
			return
		}
		_, _ = io.WriteString(w, "media-bytes")
	}))
	t.Cleanup(f.cdn.Close)
	return f
}

func (f *fakeMCP) Connected() bool { return f.connected }

func text(s string, isErr bool) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}, "isError": isErr})
	return raw
}

func structured(v any) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}, "structuredContent": v})
	return raw
}

func (f *fakeMCP) Call(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tool)
	f.args[tool] = args
	if f.noPlan {
		return text("FNF API GET /mcp/workspaces failed (403)", true), nil
	}
	switch {
	case strings.HasPrefix(tool, "estimate_"):
		// As the live MCP: medias must be an array when present (a null fails its validation), and the price comes
		// back as {"cost": {"credits": …}}.
		var wire struct {
			Params map[string]json.RawMessage `json:"params"`
		}
		raw, _ := json.Marshal(args)
		_ = json.Unmarshal(raw, &wire)
		if m, ok := wire.Params["medias"]; ok && (len(m) == 0 || m[0] != '[') {
			return text("Input validation error: Invalid arguments for tool "+tool+": params: Invalid input", true), nil
		}
		return structured(map[string]any{"cost": map[string]any{"credits": f.credits, "credits_exact": f.credits}}), nil
	case tool == "media_upload":
		return structured(map[string]any{"media_id": "11111111-2222-4333-8444-555555555555", "upload_url": f.cdn.URL + "/put"}), nil
	case tool == "media_confirm":
		return text("confirmed", false), nil
	case tool == "models_explore":
		return structured(map[string]any{"model": map[string]any{"medias": []any{map[string]any{"roles": []any{"start_image", "end_image"}}}}}), nil
	case strings.HasPrefix(tool, "generate_"):
		if f.unlim {
			if p, _ := args["params"].(map[string]any); p["use_unlim"] == nil {
				return structured(map[string]any{"unlim_choice": map[string]any{"question": "Use unlim?"}}), nil
			}
		}
		return text(`Submitted. {"job_id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "status": "queued"}`, false), nil
	case tool == "job_status":
		f.polls++
		if f.polls < 2 {
			return structured(map[string]any{"status": "in_progress", "poll_after_seconds": 5}), nil
		}
		return structured(map[string]any{"status": "completed", "inputs": map[string]any{"url": f.cdn.URL + "/in.png"},
			"results": []any{map[string]any{"thumbnail_url": f.cdn.URL + "/thumb.jpg", "url": f.cdn.URL + "/out/result.mp3"}}}), nil
	}
	return text("no such tool", true), nil
}

func (f *fakeMCP) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func mcpService(t *testing.T, f *fakeMCP) *Service {
	return &Service{Dir: t.TempDir(), MCP: f, AllowHTTP: true, PollFirst: time.Millisecond, PollMax: 2 * time.Millisecond}
}

func waitEnd(t *testing.T, s *Service, id string) Snapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, _ := s.Wait(ctx, id)
	if !snap.State.Ended() {
		t.Fatalf("job did not end: %+v", snap)
	}
	return snap
}

func TestAudioThroughTheMCP(t *testing.T) {
	f := newFakeMCP(t)
	s := mcpService(t, f)
	snap, err := s.Start(Request{Kind: extproviders.KindAudio, Model: "music", Prompt: "a calm piano tune", Seconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	end := waitEnd(t, s, snap.ID)
	if end.State != Done || len(end.Files) != 1 || !strings.HasSuffix(end.Files[0].Path, ".mp3") {
		t.Fatalf("%+v", end)
	}
	if end.Credits != 6 || math.Abs(end.EstimateUSD-6*USDPerCredit) > 1e-9 || !strings.Contains(end.Meta, "6 credits") {
		t.Errorf("price: %v %v %q", end.Credits, end.EstimateUSD, end.Meta)
	}
	if b, _ := os.ReadFile(end.Files[0].Path); string(b) != "media-bytes" {
		t.Errorf("downloaded %q", b)
	}
	p := f.args["generate_audio"]["params"].(map[string]any)
	if p["model"] != "suno_music" || p["duration"] != 30 || p["count"] != nil {
		t.Errorf("params %v", p)
	}
}

func TestVideoFallsBackToTheMCPWithoutAKeyAndUploadsFrames(t *testing.T) {
	f := newFakeMCP(t)
	s := mcpService(t, f)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	_ = os.WriteFile(a, []byte("png"), 0o644)
	_ = os.WriteFile(b, []byte("png"), 0o644)
	snap, err := s.Start(Request{Kind: extproviders.KindVideo, Prompt: "the cat turns", Frames: []string{a, b}, Seconds: 5,
		FrameOK: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Provider != "Higgsfield (MCP)" {
		t.Errorf("provider %q", snap.Provider)
	}
	end := waitEnd(t, s, snap.ID)
	if end.State != Done {
		t.Fatalf("%+v", end)
	}
	p := f.args["generate_video"]["params"].(map[string]any)
	medias, _ := p["medias"].([]any)
	if p["model"] != "seedance_2_5" || len(medias) != 2 {
		t.Fatalf("params %v", p)
	}
	if r := medias[1].(map[string]any)["role"]; r != "end_image" {
		t.Errorf("last frame role %v", r)
	}
	ups := 0
	for _, c := range f.called() {
		if c == "media_upload" {
			ups++
		}
	}
	if ups != 2 {
		t.Errorf("uploads %d: %v", ups, f.called())
	}
}

func TestMCPOverCapIsNotSent(t *testing.T) {
	f := newFakeMCP(t)
	f.credits = 100 // ≈ $7
	s := mcpService(t, f)
	snap, err := s.Start(Request{Kind: extproviders.KindImage, Prompt: "a poster"})
	if err != nil {
		t.Fatal(err)
	}
	end := waitEnd(t, s, snap.ID)
	if end.State != Failed || !strings.Contains(end.Error, "over the cap") {
		t.Fatalf("%+v", end)
	}
	for _, c := range f.called() {
		if strings.HasPrefix(c, "generate_") {
			t.Fatal("sent over the cap")
		}
	}
}

func TestMCPTrialPaysNothingAndAsksFirst(t *testing.T) {
	f := newFakeMCP(t)
	f.unlim, f.credits = true, 100
	s := mcpService(t, f)
	// Credits over the cap, but the trial pays: it is sent with use_unlim.
	snap, err := s.Start(Request{Kind: extproviders.KindVideo, Prompt: "waves", Allowance: "trial"})
	if err != nil {
		t.Fatal(err)
	}
	if end := waitEnd(t, s, snap.ID); end.State != Done || end.EstimateUSD != 0 {
		t.Fatalf("%+v", end)
	}
	// Without the person's word, Higgsfield asks, and the job stops with the question.
	f.credits = 5
	snap, _ = s.Start(Request{Kind: extproviders.KindVideo, Prompt: "waves"})
	end := waitEnd(t, s, snap.ID)
	if end.State != Failed || !strings.Contains(end.Error, "allowance") {
		t.Fatalf("%+v", end)
	}
}

func TestMCPWithoutAPlanSaysHowToStartOne(t *testing.T) {
	f := newFakeMCP(t)
	f.noPlan = true
	s := mcpService(t, f)
	_, err := s.Estimate(context.Background(), Request{Kind: extproviders.KindVideo, Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "higgsfield.ai/mcp") {
		t.Fatalf("%v", err)
	}
}

func TestAudioNeedsTheConnection(t *testing.T) {
	s := &Service{Dir: t.TempDir(), MCP: &fakeMCP{}}
	if _, err := s.Start(Request{Kind: extproviders.KindAudio, Prompt: "hello"}); !errors.Is(err, ErrNoMCP) {
		t.Fatalf("%v", err)
	}
	s.MCP = nil
	if _, err := s.Start(Request{Kind: extproviders.KindVideo, Prompt: "hello"}); !errors.Is(err, ErrNoKey) {
		t.Fatalf("%v", err)
	}
}

func TestMCPModels(t *testing.T) {
	for _, c := range []struct {
		kind extproviders.MediaKind
		in   string
		want string
		bad  bool
	}{
		{extproviders.KindVideo, "", "seedance_2_5", false},
		{extproviders.KindVideo, "Kling", "kling3_0", false},
		{extproviders.KindVideo, "ltx", "", true},
		{extproviders.KindVideo, "kling-video/v3.0/std/image-to-video", "", true},
		{extproviders.KindImage, "", "gpt_image_2_6", false},
		{extproviders.KindAudio, "sfx", "mirelo_text_to_audio", false},
		{extproviders.KindAudio, "seed_audio", "seed_audio", false},
	} {
		got, err := mcpModel(c.kind, c.in)
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("%s %q: %q %v", c.kind, c.in, got, err)
		}
	}
}

func TestResultURLsSkipInputsAndThumbnails(t *testing.T) {
	var data any
	_ = json.Unmarshal([]byte(`{"status":"completed","medias":[{"url":"https://x/in.png"}],"result":{"preview_url":"https://x/p.jpg","raw":{"url":"https://x/out.mp4"}}}`), &data)
	if got := resultURLs(data, false); len(got) != 1 || got[0] != "https://x/out.mp4" {
		t.Fatalf("%v", got)
	}
}

// The answer of job_status for a picture as Higgsfield's MCP gave it live: the small webp beside the picture.
func TestResultURLsTakeThePictureNotItsSmallCopy(t *testing.T) {
	var data any
	raw := `{"generation":{"id":"857329c9","model":"z_image","results":{"minUrl":"https://cdn.example/hf_1_min.webp","rawUrl":"https://cdn.example/hf_1.png"},"status":"completed","type":"image"}}`
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	if got := resultURLs(data, false); len(got) != 1 || got[0] != "https://cdn.example/hf_1.png" {
		t.Fatalf("got %v", got)
	}
	if st, _ := findStr(data, "status"); mcpStatus(st) != "completed" {
		t.Fatalf("status %q", st)
	}
}
