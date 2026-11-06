package voice

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/voice/cloud"
)

// dictate runs one dictation against a server and returns the messages up to the final or the error.
func dictate(t *testing.T, cfg Config, start string) []string {
	t.Helper()
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	audio, err := os.ReadFile("testdata/tone.webm")
	if err != nil {
		t.Fatal(err)
	}
	cfg.FFmpeg = real
	srv := httptest.NewServer(New(cfg))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/voice", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	_ = ws.Write(ctx, websocket.MessageText, []byte(start))
	var got []string
	stopped := false
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (got %q)", err, got)
		}
		got = append(got, string(data))
		s := string(data)
		if strings.Contains(s, `"ready"`) && !stopped {
			stopped = true
			for i := 0; i < len(audio); i += 1000 {
				_ = ws.Write(ctx, websocket.MessageBinary, audio[i:min(i+1000, len(audio))])
			}
			_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"stop"}`))
		}
		if strings.Contains(s, `"final"`) || strings.Contains(s, `"error"`) {
			return got
		}
	}
}

func fakeCloud(heard string, pcmSeen *atomic.Int64, opts *cloud.Options) cloud.Func {
	return func(ctx context.Context, o cloud.Options, pcm <-chan []byte, ev cloud.Events) {
		*opts = o
		ev.Ready()
		for b := range pcm {
			pcmSeen.Add(int64(len(b)))
		}
		ev.Partial("открой")
		ev.Final(heard)
	}
}

// A cloud engine answers through the same socket protocol, gets the key, the language and the words, and the person's
// words are put over its text.
func TestCloudEngineWithDictionary(t *testing.T) {
	var seen atomic.Int64
	var opts cloud.Options
	cfg := Config{
		Cloud:       map[string]cloud.Func{"openai": fakeCloud("открой синопию в харнесли", &seen, &opts)},
		Credentials: func(string) (string, string, error) { return "k" + "ey-test", "", nil },
	}
	got := dictate(t, cfg, `{"type":"start","lang":"ru","engine":"openai","dict":[{"word":"Sinopia","heard":[]},{"word":"harnessly","heard":[]}]}`)
	last := got[len(got)-1]
	if last != `{"type":"final","text":"открой Sinopia в harnessly"}` {
		t.Fatalf("messages: %q", got)
	}
	if got[0] != `{"type":"ready"}` || seen.Load() == 0 {
		t.Fatalf("ready/audio: %q, %d bytes", got, seen.Load())
	}
	if opts.Key != "k"+"ey-test" || opts.Lang != "ru" || len(opts.Hints) != 2 || opts.Hints[0] != "Sinopia" {
		t.Fatalf("options: %+v", opts)
	}
}

// Without a saved key the dictation fails with the words that point to Settings; the audio goes nowhere.
func TestCloudEngineWithoutKey(t *testing.T) {
	var seen atomic.Int64
	var opts cloud.Options
	cfg := Config{
		Cloud:       map[string]cloud.Func{"yandex": fakeCloud("x", &seen, &opts)},
		Credentials: func(string) (string, string, error) { return "", "", errors.New("no key") },
	}
	got := dictate(t, cfg, `{"type":"start","lang":"ru","engine":"yandex"}`)
	if len(got) != 1 || !strings.Contains(got[0], `"code":"failed"`) || !strings.Contains(got[0], "Модели и ключи") {
		t.Fatalf("messages: %q", got)
	}
	if seen.Load() != 0 {
		t.Fatal("audio reached the engine")
	}
}

// The ChatGPT plan's model has no key to add: without the sign-in the words say what it needs, and the audio goes nowhere.
func TestChatGPTEngineWithoutLogin(t *testing.T) {
	var seen atomic.Int64
	var opts cloud.Options
	cfg := Config{
		Cloud:       map[string]cloud.Func{"chatgpt": fakeCloud("x", &seen, &opts)},
		Credentials: func(string) (string, string, error) { return "", "", errors.New("no login") },
	}
	got := dictate(t, cfg, `{"type":"start","lang":"ru","engine":"chatgpt"}`)
	if len(got) != 1 || !strings.Contains(got[0], `"code":"failed"`) || !strings.Contains(got[0], "codex login") {
		t.Fatalf("messages: %q", got)
	}
	if seen.Load() != 0 {
		t.Fatal("audio reached the engine")
	}
}

// The engine gets the sign-in's token and account, and the person's words are put over its text.
func TestChatGPTEngineWithLogin(t *testing.T) {
	var seen atomic.Int64
	var opts cloud.Options
	cfg := Config{
		Cloud: map[string]cloud.Func{"chatgpt": fakeCloud("открой синопию", &seen, &opts)},
		Credentials: func(engine string) (string, string, error) {
			if engine != "chatgpt" {
				t.Errorf("engine %q", engine)
			}
			return "tok", "acc", nil
		},
	}
	got := dictate(t, cfg, `{"type":"start","lang":"ru","engine":"chatgpt","dict":[{"word":"Sinopia","heard":[]}]}`)
	if got[len(got)-1] != `{"type":"final","text":"открой Sinopia"}` || opts.Key != "tok" || opts.Folder != "acc" || seen.Load() == 0 {
		t.Fatalf("messages: %q, options: %+v, %d bytes", got, opts, seen.Load())
	}
}

func TestCloudEngineUnknown(t *testing.T) {
	got := dictate(t, Config{Credentials: func(string) (string, string, error) { return "k" + "ey-test", "", nil }},
		`{"type":"start","engine":"whisperx"}`)
	if len(got) != 1 || !strings.Contains(got[0], `"code":"failed"`) {
		t.Fatalf("messages: %q", got)
	}
}

// A cloud engine that fails says so in its words, with the code that keeps the page from trying the local one.
func TestCloudEngineFails(t *testing.T) {
	cfg := Config{
		Cloud: map[string]cloud.Func{"openai": func(ctx context.Context, o cloud.Options, pcm <-chan []byte, ev cloud.Events) {
			ev.Fail("Ключ не принят")
		}},
		Credentials: func(string) (string, string, error) { return "k" + "ey-test", "", nil },
	}
	got := dictate(t, cfg, `{"type":"start","lang":"ru","engine":"openai"}`)
	if got[len(got)-1] != `{"type":"error","message":"Ключ не принят","code":"failed"}` {
		t.Fatalf("messages: %q", got)
	}
}
