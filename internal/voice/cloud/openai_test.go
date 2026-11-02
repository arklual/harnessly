package cloud

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

var testKey = "k" + "ey-test"

type oaiResult struct {
	mu       sync.Mutex
	ready    int
	partials []string
	finals   []string
	fails    []string
}

func (r *oaiResult) events() Events {
	return Events{
		Ready:   func() { r.mu.Lock(); r.ready++; r.mu.Unlock() },
		Partial: func(t string) { r.mu.Lock(); r.partials = append(r.partials, t); r.mu.Unlock() },
		Final:   func(t string) { r.mu.Lock(); r.finals = append(r.finals, t); r.mu.Unlock() },
		Fail:    func(m string) { r.mu.Lock(); r.fails = append(r.fails, m); r.mu.Unlock() },
	}
}

func oaiRead(ctx context.Context, c *websocket.Conn) (map[string]any, error) {
	_, data, err := c.Read(ctx)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	err = json.Unmarshal(data, &m)
	return m, err
}

func oaiWrite(ctx context.Context, c *websocket.Conn, v any) {
	b, _ := json.Marshal(v)
	_ = c.Write(ctx, websocket.MessageText, b)
}

func oaiServer(t *testing.T, handler func(ctx context.Context, c *websocket.Conn)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		handler(r.Context(), c)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// oaiRun runs OpenAI against url; feed writes to the pcm channel and may close it.
func oaiRun(t *testing.T, ctx context.Context, opts Options, feed func(ch chan []byte)) *oaiResult {
	t.Helper()
	res := &oaiResult{}
	pcm := make(chan []byte, 16)
	done := make(chan struct{})
	go func() { OpenAI(ctx, opts, pcm, res.events()); close(done) }()
	feed(pcm)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OpenAI did not return")
	}
	return res
}

func silence(samples int) []byte { return make([]byte, samples*2) }

func TestOpenAIHappyPathOutOfOrder(t *testing.T) {
	var audio atomic.Int64
	upd := make(chan map[string]any, 1)
	url := oaiServer(t, func(ctx context.Context, c *websocket.Conn) {
		oaiWrite(ctx, c, map[string]any{"type": "session.created"})
		for {
			m, err := oaiRead(ctx, c)
			if err != nil {
				return
			}
			switch m["type"] {
			case "session.update":
				upd <- m
			case "input_audio_buffer.append":
				raw, _ := base64.StdEncoding.DecodeString(m["audio"].(string))
				audio.Add(int64(len(raw)))
			case "input_audio_buffer.commit":
				oaiWrite(ctx, c, map[string]any{"type": "input_audio_buffer.committed", "item_id": "a", "previous_item_id": ""})
				oaiWrite(ctx, c, map[string]any{"type": "input_audio_buffer.committed", "item_id": "b", "previous_item_id": "a"})
				oaiWrite(ctx, c, map[string]any{"type": "conversation.item.input_audio_transcription.delta", "item_id": "b", "delta": "world"})
				oaiWrite(ctx, c, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "b", "transcript": "world"})
				oaiWrite(ctx, c, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "a", "transcript": " hello "})
			}
		}
	})
	res := oaiRun(t, context.Background(), Options{Lang: "en", Key: testKey, Endpoint: url, Hints: []string{"Sinopia", "harnessly"}}, func(ch chan []byte) {
		for range 10 {
			ch <- silence(1600)
		}
		close(ch)
	})
	if len(res.finals) != 1 || res.finals[0] != "hello world" || len(res.fails) != 0 {
		t.Fatalf("finals=%q fails=%q", res.finals, res.fails)
	}
	if res.ready != 1 {
		t.Fatalf("ready=%d", res.ready)
	}
	if last := res.partials[len(res.partials)-1]; last != "hello world" {
		t.Fatalf("partials=%q", res.partials)
	}
	// 16000 samples at 16 kHz -> about 24000 samples (48000 bytes) at 24 kHz.
	if got := audio.Load(); got < 47990 || got > 48010 {
		t.Fatalf("audio bytes=%d", got)
	}

	m := <-upd
	in := m["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)
	tr := in["transcription"].(map[string]any)
	if tr["model"] != "gpt-4o-transcribe" || tr["language"] != "en" {
		t.Fatalf("transcription=%v", tr)
	}
	if p := tr["prompt"].(string); !strings.Contains(p, "Sinopia, harnessly") {
		t.Fatalf("prompt=%q", p)
	}
	if in["format"].(map[string]any)["rate"].(float64) != 24000 {
		t.Fatalf("format=%v", in["format"])
	}
}

func TestOpenAISessionLanguage(t *testing.T) {
	auto := openAISession(Options{Lang: "auto"})
	tr := auto["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)["transcription"].(map[string]any)
	if _, ok := tr["language"]; ok {
		t.Fatal("auto must omit language")
	}
	if _, ok := tr["prompt"]; ok {
		t.Fatal("no hints, no prompt")
	}
	ru := openAISession(Options{Lang: "ru", Hints: []string{"Sinopia"}})
	tr = ru["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)["transcription"].(map[string]any)
	if tr["language"] != "ru" || !strings.Contains(tr["prompt"].(string), "Речь о программировании") {
		t.Fatalf("tr=%v", tr)
	}
}

func TestResampleLengthAndContinuity(t *testing.T) {
	const n = 3000
	in := make([]byte, n*2)
	for i := range n {
		binary.LittleEndian.PutUint16(in[i*2:], uint16(int16(i*9-10000)))
	}
	var whole resampler
	want := whole.write(in)
	if got, exp := len(want)/2, 3*(n-1)/2; got < exp-2 || got > exp+2 {
		t.Fatalf("samples=%d want about %d", got, exp)
	}
	// Linear ramp stays a linear ramp: y[m] = x(2m/3).
	for m := 0; m < len(want)/2; m++ {
		y := int16(binary.LittleEndian.Uint16(want[m*2:]))
		exp := float64(2*m)/3*9 - 10000
		if d := float64(y) - exp; d > 1 || d < -1 {
			t.Fatalf("m=%d y=%d exp=%.1f", m, y, exp)
		}
	}
	// Odd splits, including 1-byte chunks, give the same output.
	var split resampler
	var got []byte
	for i, sizes := 0, []int{1, 3, 7, 2, 1, 101, 5}; i < len(in); {
		sz := min(sizes[i%len(sizes)], len(in)-i)
		got = append(got, split.write(in[i:i+sz])...)
		i += sz
	}
	if string(got) != string(want) {
		t.Fatalf("split output differs: %d vs %d bytes", len(got), len(want))
	}
}

func TestOpenAIHandshakeStatus(t *testing.T) {
	for status, want := range map[int]string{http.StatusUnauthorized: "ключ", http.StatusTooManyRequests: "лимит", http.StatusBadGateway: "502"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", status) }))
		res := oaiRun(t, context.Background(), Options{Key: testKey, Endpoint: srv.URL}, func(ch chan []byte) {})
		srv.Close()
		if len(res.fails) != 1 || len(res.finals) != 0 || !strings.Contains(res.fails[0], want) || strings.Contains(res.fails[0], testKey) {
			t.Fatalf("status %d: fails=%q finals=%q", status, res.fails, res.finals)
		}
	}
}

func TestOpenAIErrorEvent(t *testing.T) {
	url := oaiServer(t, func(ctx context.Context, c *websocket.Conn) {
		oaiWrite(ctx, c, map[string]any{"type": "session.created"})
		oaiWrite(ctx, c, map[string]any{"type": "error", "error": map[string]any{"code": "server_error", "message": "boom " + testKey}})
		_, _, _ = c.Read(ctx)
	})
	res := oaiRun(t, context.Background(), Options{Key: testKey, Endpoint: url}, func(ch chan []byte) {})
	if len(res.fails) != 1 || len(res.finals) != 0 || strings.Contains(res.fails[0], "boom") || strings.Contains(res.fails[0], testKey) {
		t.Fatalf("fails=%q finals=%q", res.fails, res.finals)
	}
}

func TestOpenAIAbort(t *testing.T) {
	url := oaiServer(t, func(ctx context.Context, c *websocket.Conn) {
		oaiWrite(ctx, c, map[string]any{"type": "session.created"})
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	res := oaiRun(t, ctx, Options{Key: testKey, Endpoint: url}, func(ch chan []byte) {
		ch <- silence(1600)
		time.Sleep(200 * time.Millisecond)
		cancel()
	})
	if len(res.fails) != 0 || len(res.finals) != 0 {
		t.Fatalf("fails=%q finals=%q", res.fails, res.finals)
	}
}

func TestOpenAICommitEmptyIgnored(t *testing.T) {
	url := oaiServer(t, func(ctx context.Context, c *websocket.Conn) {
		oaiWrite(ctx, c, map[string]any{"type": "session.created"})
		first := true
		for {
			m, err := oaiRead(ctx, c)
			if err != nil {
				return
			}
			switch m["type"] {
			case "input_audio_buffer.append":
				if first { // VAD committed everything already
					first = false
					oaiWrite(ctx, c, map[string]any{"type": "input_audio_buffer.committed", "item_id": "a"})
					oaiWrite(ctx, c, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "a", "transcript": "hi"})
				}
			case "input_audio_buffer.commit":
				oaiWrite(ctx, c, map[string]any{"type": "error", "error": map[string]any{"code": "input_audio_buffer_commit_empty"}})
			}
		}
	})
	res := oaiRun(t, context.Background(), Options{Key: testKey, Endpoint: url}, func(ch chan []byte) {
		ch <- silence(4000)
		close(ch)
	})
	if len(res.finals) != 1 || res.finals[0] != "hi" || len(res.fails) != 0 {
		t.Fatalf("finals=%q fails=%q", res.finals, res.fails)
	}
}
