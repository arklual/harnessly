package engine

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for whisper-server: started with ENGINE_FAKE_SERVER=1 it serves /health and
// /inference on --port the way the real one does, and answers every recording with a fixed text.
func TestMain(m *testing.M) {
	if os.Getenv("ENGINE_FAKE_SERVER") == "1" {
		port := ""
		for i, a := range os.Args {
			if a == "--port" {
				port = os.Args[i+1]
			}
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
		mux.HandleFunc("/inference", func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				http.Error(w, `{"error":"bad form"}`, 400)
				return
			}
			f, _, err := r.FormFile("file")
			if err != nil {
				http.Error(w, `{"error":"no file"}`, 400)
				return
			}
			raw, _ := io.ReadAll(f)
			if len(raw) < 44 || string(raw[:4]) != "RIFF" {
				http.Error(w, `{"error":"not a wav"}`, 400)
				return
			}
			text := " привет мир [" + r.FormValue("language") + "] "
			if ac := r.FormValue("audio_ctx"); ac != "" {
				text += "ctx " + ac
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"text": text})
		})
		_ = http.ListenAndServe("127.0.0.1:"+port, mux)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func tone(seconds float64) []byte {
	n := int(seconds * sampleRate)
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(int16(8000*math.Sin(float64(i)*0.1))))
	}
	return b
}

// fake is an engine whose server is this test binary and whose model is a file of the test's own.
func fake(t *testing.T) *Engine {
	t.Setenv("ENGINE_FAKE_SERVER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	model := filepath.Join(dir, "model.bin")
	_ = os.WriteFile(model, []byte("model"), 0o644)
	return New(Config{Dir: dir, Bin: exe, ModelFile: model, GOOS: "linux", GOARCH: "amd64", Idle: time.Minute})
}

func TestHelpers(t *testing.T) {
	w := wav([]byte{1, 2, 3, 4})
	if len(w) != 48 || string(w[:4]) != "RIFF" || binary.LittleEndian.Uint32(w[40:]) != 4 || binary.LittleEndian.Uint32(w[24:]) != 16000 {
		t.Fatalf("wav header: %v", w[:44])
	}
	if got := sumOf([]byte("aa  other\n"+strings.Repeat("b", 64)+" *whisper-server-linux-amd64\n"), "whisper-server-linux-amd64"); got != strings.Repeat("b", 64) {
		t.Fatalf("sumOf: %q", got)
	}
	for in, want := range map[string]string{" [BLANK_AUDIO] ": "", "(музыка)": "", "привет\n  мир": "привет мир", "[a] привет": "[a] привет"} {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
	if rms(tone(1)) < 0.1 || rms(make([]byte, 3200)) != 0 {
		t.Fatal("rms")
	}
	if len(padPCM(nil)) < sampleRate*2 {
		t.Fatal("padPCM")
	}
	// 20 ms a position and a second more; the whole window (0) when that is not shorter.
	for n, want := range map[int]int{5 * 32000: 300, len(padPCM(nil)): 105, 28 * 32000: 1450, 29 * 32000: 0, 40 * 32000: 0, 32002: 101} {
		if got := quickAudioCtx(n); got != want {
			t.Errorf("quickAudioCtx(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestEligible(t *testing.T) {
	look := func(string) (string, error) { return "", errors.New("none") }
	cases := []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"linux amd64 avx2", Config{GOOS: "linux", GOARCH: "amd64", CPUFlags: func() string { return "flags : sse4_2 avx avx2 fma" }}, true},
		{"linux amd64 without avx2", Config{GOOS: "linux", GOARCH: "amd64", CPUFlags: func() string { return "flags : sse4_2 avx" }}, false},
		{"linux arm64", Config{GOOS: "linux", GOARCH: "arm64"}, true},
		{"linux riscv", Config{GOOS: "linux", GOARCH: "riscv64"}, false},
		{"mac without a server", Config{GOOS: "darwin", GOARCH: "arm64"}, false},
		{"mac with a server of its own", Config{GOOS: "darwin", GOARCH: "arm64", Bin: "/opt/whisper-server"}, true},
		{"windows", Config{GOOS: "windows", GOARCH: "amd64"}, false},
	}
	for _, c := range cases {
		c.cfg.Dir, c.cfg.LookPath = t.TempDir(), look
		if err := New(c.cfg).Eligible(); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestDownloadChecksum(t *testing.T) {
	body := []byte(strings.Repeat("ggml", 1000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "m", "model.bin")
	if err := download(context.Background(), srv.Client(), srv.URL, dest, strings.Repeat("0", 64), 0o644, nil); !errors.Is(err, ErrChecksum) {
		t.Fatalf("wrong sum: %v", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("a file that failed its checksum was kept")
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Fatal(".part was left")
	}
	var last int64
	if err := download(context.Background(), srv.Client(), srv.URL, dest, sum(body), 0o644, func(d, _ int64) { last = d }); err != nil || last != int64(len(body)) {
		t.Fatalf("download: %v %d", err, last)
	}
	if fileSum(dest) != sum(body) {
		t.Fatal("content")
	}
}

type client struct {
	c net.Conn
	r *bufio.Reader
}

func dial(t *testing.T, path string) *client {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &client{c: c, r: bufio.NewReader(c)}
}

func (c *client) send(v map[string]any) {
	b, _ := json.Marshal(v)
	_, _ = c.c.Write(append(b, '\n'))
}

func (c *client) next(t *testing.T) map[string]any {
	t.Helper()
	_ = c.c.SetReadDeadline(time.Now().Add(20 * time.Second))
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (c *client) until(t *testing.T, typ string) map[string]any {
	t.Helper()
	for {
		if m := c.next(t); m["type"] == typ {
			return m
		}
	}
}

func TestDictationOverTheSocket(t *testing.T) {
	e := fake(t)
	defer e.Close()
	sock := filepath.Join(e.cfg.Dir, "asr.sock")
	ln, err := e.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if st, _ := os.Stat(sock); st.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", st.Mode())
	}

	c := dial(t, sock)
	c.send(map[string]any{"cmd": "start", "lang": "en"})
	c.until(t, "ready")
	// Three seconds of sound arrive, then the stop: the answer is the final text in the language asked for.
	c.send(map[string]any{"cmd": "audio", "pcm": base64.StdEncoding.EncodeToString(tone(3))})
	c.send(map[string]any{"cmd": "stop"})
	final := c.until(t, "final")
	if final["text"] != "привет мир [en]" {
		t.Fatalf("final: %v", final)
	}

	// While the person speaks, the partials take the quick pass (an encoder context for the recording so far); the
	// final one the whole window.
	c = dial(t, sock)
	c.send(map[string]any{"cmd": "start", "lang": "ru"})
	c.until(t, "ready")
	c.send(map[string]any{"cmd": "audio", "pcm": base64.StdEncoding.EncodeToString(tone(3))})
	if partial := c.until(t, "partial"); partial["text"] != "привет мир [ru] ctx 200" {
		t.Fatalf("partial: %v", partial)
	}
	c.send(map[string]any{"cmd": "stop"})
	if final := c.until(t, "final"); final["text"] != "привет мир [ru]" {
		t.Fatalf("final: %v", final)
	}

	// Silence is not sent to the model: the final text is empty.
	c = dial(t, sock)
	c.send(map[string]any{"cmd": "start"})
	c.until(t, "ready")
	c.send(map[string]any{"cmd": "audio", "pcm": base64.StdEncoding.EncodeToString(make([]byte, 64000))})
	c.send(map[string]any{"cmd": "stop"})
	if final := c.until(t, "final"); final["text"] != "" {
		t.Fatalf("silence: %v", final)
	}

	c = dial(t, sock)
	c.send(map[string]any{"cmd": "status"})
	if st := c.next(t); st["type"] != "status" || st["ready"] != true {
		t.Fatalf("status: %v", st)
	}
}

func TestFirstUseDownloadsAndTellsHowFar(t *testing.T) {
	t.Setenv("ENGINE_FAKE_SERVER", "1")
	exe, _ := os.Executable()
	binary, _ := os.ReadFile(exe)
	model := []byte(strings.Repeat("m", 300<<10))
	files := map[string][]byte{"whisper-server-linux-amd64": binary, "ggml-base-q5_1.bin": model,
		BinarySums: []byte(sum(binary) + "  whisper-server-linux-amd64\n")}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		for i := 0; i < len(b); i += 64 << 10 {
			_, _ = w.Write(b[i:min(i+64<<10, len(b))])
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
	}))
	defer srv.Close()
	saved := Models["base"]
	Models["base"] = Model{"base", "ggml-base-q5_1.bin", int64(len(model)), sum(model)}
	defer func() { Models["base"] = saved }()

	dir := t.TempDir()
	e := New(Config{Dir: dir, Model: "base", GOOS: "linux", GOARCH: "amd64", ModelBase: srv.URL + "/", BinaryBase: srv.URL + "/",
		LookPath: func(string) (string, error) { return "", errors.New("none") }, CPUFlags: func() string { return " avx2 fma" }})
	defer e.Close()
	sock := filepath.Join(dir, "asr.sock")
	ln, err := e.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	c := dial(t, sock)
	c.send(map[string]any{"cmd": "start", "lang": "ru"})
	var stages []string
	for {
		m := c.next(t)
		if m["type"] == "ready" {
			break
		}
		if m["type"] != "preparing" {
			t.Fatalf("unexpected %v", m)
		}
		stages = append(stages, fmt.Sprint(m["stage"]))
	}
	if !slices.Contains(stages, "binary") {
		t.Fatalf("the download of the server was not reported: %v", stages)
	}
	if fileSum(filepath.Join(dir, "models", "ggml-base-q5_1.bin")) != sum(model) {
		t.Fatal("the model was not kept")
	}
	c.send(map[string]any{"cmd": "audio", "pcm": base64.StdEncoding.EncodeToString(tone(2))})
	c.send(map[string]any{"cmd": "stop"})
	if final := c.until(t, "final"); final["text"] != "привет мир [ru]" {
		t.Fatalf("final: %v", final)
	}

	// The second dictation finds everything in place: no download, ready at once.
	files = map[string][]byte{}
	c = dial(t, sock)
	c.send(map[string]any{"cmd": "start", "lang": "ru"})
	if m := c.next(t); m["type"] != "ready" {
		t.Fatalf("second: %v", m)
	}
}

func TestOfflineFailureIsReportedAndRetried(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	dir := t.TempDir()
	e := New(Config{Dir: dir, Model: "base", GOOS: "linux", GOARCH: "amd64", ModelBase: srv.URL + "/", BinaryBase: srv.URL + "/",
		LookPath: func(string) (string, error) { return "", errors.New("none") }, CPUFlags: func() string { return " avx2 fma" }})
	sock := filepath.Join(dir, "asr.sock")
	ln, err := e.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c := dial(t, sock)
	c.send(map[string]any{"cmd": "start"})
	m := c.until(t, "error")
	if m["code"] != "failed" || m["message"] == "" {
		t.Fatalf("error: %v", m)
	}
	if st := e.Status(); st.State != "failed" {
		t.Fatalf("status: %+v", st)
	}

	// With downloads off nothing is requested at all.
	var hits int
	counting := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer counting.Close()
	off := New(Config{Dir: t.TempDir(), NoDownload: true, Model: "base", GOOS: "linux", GOARCH: "arm64", ModelBase: counting.URL + "/", BinaryBase: counting.URL + "/",
		LookPath: func(string) (string, error) { return "", errors.New("none") }})
	off.Prepare()
	if err := off.Wait(context.Background()); err == nil || hits != 0 {
		t.Fatalf("offline: %v, %d requests", err, hits)
	}
}
