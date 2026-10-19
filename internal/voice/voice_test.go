package voice

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testdata/golden.json: what the page got from the previous runtime's voice socket in each scenario
// (testdata/gen, under node), with the same audio, ffmpeg and fake recognition service.

type fakeService struct {
	mu   sync.Mutex
	mode string
	ln   net.Listener
	path string
}

func (f *fakeService) listen(t *testing.T) {
	ln, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.ln = ln
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
}

func (f *fakeService) serve(conn net.Conn) {
	defer conn.Close()
	f.mu.Lock()
	mode := f.mode
	f.mu.Unlock()
	emit := func(line string) { _, _ = conn.Write([]byte(line + "\n")) }
	r := bufio.NewReaderSize(conn, 1<<20)
	lang, total, audios := "", 0, 0
	h := sha256.New()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		var msg struct{ Cmd, Lang, Pcm string }
		_ = json.Unmarshal([]byte(line), &msg)
		switch msg.Cmd {
		case "start":
			lang = msg.Lang
			if mode == "error-before-ready" {
				emit(`{"type":"error","message":"Модель не загружена"}`)
			} else if mode != "silent" {
				emit(`{"type":"ready"}`)
			}
			if mode == "malformed" {
				emit("not json\n\n   \n{\"type\":\"partial\",\"text\":\"a\"}\n[1]\n{\"type\":\"unknown\"}")
			}
		case "audio":
			audios++
			if audios == 1 && mode == "error-after-ready" {
				emit(`{"type":"error","message":42}`)
				continue
			}
			if audios == 1 && mode == "close-after-ready" {
				// Closing with audio still unread would make the kernel answer with a reset instead of an end of
				// stream; the service the goldens recorded ended its side cleanly.
				if cw, ok := conn.(interface{ CloseWrite() error }); ok {
					_ = cw.CloseWrite()
				}
				_, _ = io.Copy(io.Discard, r)
				return
			}
			if audios == 1 && mode == "no-text" {
				emit(`{"type":"partial"}`)
			}
			pcm, _ := base64.StdEncoding.DecodeString(msg.Pcm)
			h.Write(pcm)
			before := total / 16000
			total += len(pcm)
			for k := before + 1; k <= total/16000; k++ {
				emit(fmt.Sprintf(`{"type":"partial","text":"pcm %d %s"}`, k*16000, lang))
			}
		case "stop":
			if mode == "no-text" {
				emit(`{"type":"final"}`)
			} else {
				emit(fmt.Sprintf(`{"type":"final","text":"pcm %d %s %s"}`, total, lang, hex.EncodeToString(h.Sum(nil))))
			}
			return
		}
	}
}

var pcmHash = regexp.MustCompile(` [0-9a-f]{64}"`)

func TestDictationAsTheOldRuntime(t *testing.T) {
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		FfmpegVersion string `json:"ffmpegVersion"`
		FailingFfmpeg string `json:"failingFfmpeg"`
		Scenarios     []struct {
			Name      string              `json:"name"`
			Mode      string              `json:"mode"`
			Ffmpeg    string              `json:"ffmpeg"`
			Steps     [][]json.RawMessage `json:"steps"`
			Messages  []string            `json:"messages"`
			CloseCode *int                `json:"closeCode"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	version, _ := exec.Command(real, "-version").Output()
	sameFfmpeg := strings.SplitN(string(version), "\n", 2)[0] == g.FfmpegVersion
	dir := t.TempDir()
	failing := filepath.Join(dir, "failing-ffmpeg")
	if err := os.WriteFile(failing, []byte(g.FailingFfmpeg), 0o755); err != nil {
		t.Fatal(err)
	}
	audio, err := os.ReadFile("testdata/tone.webm")
	if err != nil {
		t.Fatal(err)
	}
	svc := &fakeService{path: filepath.Join(dir, "asr.sock")}
	svc.listen(t)
	defer func() { _ = svc.ln.Close() }()

	for _, sc := range g.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			svc.mu.Lock()
			svc.mode = sc.Mode
			svc.mu.Unlock()
			if sc.Mode == "down" {
				_ = svc.ln.Close()
				defer svc.listen(t)
			}
			bin := map[string]string{"real": real, "failing": failing, "missing": filepath.Join(dir, "none"), "relative": "ffmpeg"}[sc.Ffmpeg]
			srv := httptest.NewServer(New(Config{Socket: svc.path, FFmpeg: bin}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/voice", nil)
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var got []string
			closeCode := 0
			changed := make(chan struct{}, 64)
			go func() {
				for {
					_, data, err := ws.Read(ctx)
					mu.Lock()
					if err != nil {
						var ce websocket.CloseError
						if errors.As(err, &ce) {
							closeCode = int(ce.Code)
						} else {
							closeCode = -1
						}
						mu.Unlock()
						changed <- struct{}{}
						return
					}
					got = append(got, string(data))
					mu.Unlock()
					changed <- struct{}{}
				}
			}()
			waitFor := func(test func() bool, d time.Duration) {
				deadline := time.After(d)
				for {
					mu.Lock()
					ok := test()
					mu.Unlock()
					if ok {
						return
					}
					select {
					case <-changed:
					case <-deadline:
						return
					}
				}
			}
			has := func(words ...string) func() bool {
				return func() bool {
					for _, m := range got {
						for _, w := range words {
							if strings.Contains(m, w) {
								return true
							}
						}
					}
					return false
				}
			}
			send := func(typ websocket.MessageType, b []byte) { _ = ws.Write(ctx, typ, b) }
			for _, step := range sc.Steps {
				var kind string
				_ = json.Unmarshal(step[0], &kind)
				switch kind {
				case "text":
					var s string
					_ = json.Unmarshal(step[1], &s)
					send(websocket.MessageText, []byte(s))
				case "binary":
					var n int
					_ = json.Unmarshal(step[1], &n)
					b := make([]byte, n)
					for i := range b {
						b[i] = 7
					}
					send(websocket.MessageBinary, b)
				case "audio":
					for i := 0; i < len(audio); i += 1000 {
						send(websocket.MessageBinary, audio[i:min(i+1000, len(audio))])
					}
				case "close":
					_ = ws.Close(websocket.StatusNormalClosure, "")
					time.Sleep(300 * time.Millisecond)
				case "wait":
					var what string
					if json.Unmarshal(step[1], &what) != nil {
						var ms int
						_ = json.Unmarshal(step[1], &ms)
						time.Sleep(time.Duration(ms) * time.Millisecond)
						continue
					}
					switch what {
					case "ready":
						waitFor(has(`"ready"`, `"error"`), 5*time.Second)
					case "end":
						waitFor(has(`"final"`, `"error"`), 8*time.Second)
					case "closed":
						waitFor(func() bool { return closeCode != 0 }, 5*time.Second)
					}
				}
			}
			_ = ws.Close(websocket.StatusNormalClosure, "")
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			want := sc.Messages
			have := append([]string(nil), got...)
			if !sameFfmpeg {
				for i := range want {
					want[i] = pcmHash.ReplaceAllString(want[i], `"`)
				}
				for i := range have {
					have[i] = pcmHash.ReplaceAllString(have[i], `"`)
				}
			}
			if strings.Join(have, "\n") != strings.Join(want, "\n") {
				t.Errorf("messages\n got %q\nwant %q", have, want)
			}
			if sc.CloseCode != nil && closeCode != *sc.CloseCode {
				t.Errorf("closed with %d, want %d", closeCode, *sc.CloseCode)
			}
		})
	}
}

func TestReadyTimeout(t *testing.T) {
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	sock := filepath.Join(t.TempDir(), "asr.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // a service that never answers
		}
	}()
	srv := httptest.NewServer(New(Config{Socket: sock, FFmpeg: real, ReadyTimeout: 200 * time.Millisecond}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"start"}`))
	_, data, err := ws.Read(ctx)
	if err != nil || string(data) != `{"type":"error","message":"Модель распознавания не успела загрузиться","code":"unavailable"}` {
		t.Fatalf("%s %v", data, err)
	}
}
