package voice

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"harnessly/internal/files"
)

// testdata/assets.json: what the previous runtime's allow-list made of generated paths, and what its dispatcher and
// handler answered with the model hosts served locally (testdata/gen/assets.mjs).

func TestAssetsAsTheOldRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/assets.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Resolved [][]json.RawMessage `json:"resolved"`
		Requests []struct {
			Path          string            `json:"path"`
			Headers       map[string]string `json:"headers"`
			Status        int               `json:"status"`
			AnswerHeaders map[string]string `json:"answerHeaders"`
			Body          string            `json:"body"`
		} `json:"requests"`
		Cache [][2]string `json:"cache"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	files_ := map[string]string{"config.json": `{"model_type":"whisper"}`, "vocab.txt": "a\nb\n", "encoder_model_q4.onnx": strings.Repeat("onnx-bytes-", 50)}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		switch {
		case strings.Contains(r.URL.Path, "tokenizer.json"):
			w.WriteHeader(404)
		case strings.Contains(r.URL.Path, "ort-wasm-simd-threaded.jsep.mjs"):
			var b bytes.Buffer
			z := gzip.NewWriter(&b)
			_, _ = z.Write([]byte("export const x = 1;\n"))
			_ = z.Close()
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
			_, _ = w.Write(b.Bytes())
		case strings.Contains(r.URL.Path, "ort-wasm-simd-threaded.jsep.wasm"):
			http.Redirect(w, r, "/moved/wasm-bytes", http.StatusFound)
		case strings.HasSuffix(r.URL.Path, "/moved/wasm-bytes"):
			_, _ = w.Write([]byte("\x00asm012345"))
		case files_[file] != "":
			_, _ = w.Write([]byte(files_[file]))
		default:
			w.WriteHeader(500)
		}
	}))
	defer upstream.Close()
	cache := t.TempDir()
	a := &Assets{CacheDir: filepath.Join(cache, "voice-assets"), ModelHost: "https://huggingface.co", RuntimeHost: "https://cdn.jsdelivr.net"}
	for _, r := range g.Resolved {
		var segs []string
		_ = json.Unmarshal(r[0], &segs)
		var want *struct {
			UpstreamURL string `json:"upstreamUrl"`
			CacheKey    string `json:"cacheKey"`
			ContentType string `json:"contentType"`
		}
		_ = json.Unmarshal(r[1], &want)
		got, ok := a.Resolve(segs)
		if ok != (want != nil) || ok && (got.Upstream != want.UpstreamURL || got.CacheKey != want.CacheKey || got.ContentType != want.ContentType) {
			t.Errorf("%q: got %+v %v, want %+v", segs, got, ok, want)
		}
	}
	a.ModelHost, a.RuntimeHost = upstream.URL+"/hf-host", upstream.URL+"/cdn-host"
	_ = os.MkdirAll(filepath.Join(a.CacheDir, "hf", WhisperModel), 0o755)
	_ = os.WriteFile(filepath.Join(a.CacheDir, "hf", WhisperModel, "generation_config.json"), []byte(`{"cached":true}`), 0o644)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header) == 0 || r.Header.Get("Cookie") == "" && r.Header.Get("X-Cli-Token") == "" {
			files.WriteUnauthorized(w) // the route's refusal; here only the generator's unauthenticated request
			return
		}
		parts, _, _ := files.APIParts(r)
		a.Serve(w, r, parts[3:])
	}))
	defer srv.Close()
	for i, c := range g.Requests {
		req, _ := http.NewRequest("GET", srv.URL+c.Path, nil)
		for k, v := range c.Headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		want, _ := base64.StdEncoding.DecodeString(c.Body)
		if res.StatusCode != c.Status || !bytes.Equal(body, want) {
			t.Errorf("#%d %s: got %d %q, want %d %q", i, c.Path, res.StatusCode, body, c.Status, want)
			continue
		}
		for k, v := range c.AnswerHeaders {
			if have := res.Header.Get(k); have != v {
				t.Errorf("#%d %s: %s = %q, want %q", i, c.Path, k, have, v)
			}
		}
		// A length the old runtime left out (chunked) net/http adds for a short body: framing, not content.
		for _, k := range []string{"ETag", "Cache-Control"} {
			if _, ok := c.AnswerHeaders[strings.ToLower(k)]; !ok && res.Header.Get(k) != "" {
				t.Errorf("#%d %s: unexpected %s %s", i, c.Path, k, res.Header.Get(k))
			}
		}
	}
	var tree [][2]string
	_ = filepath.Walk(a.CacheDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(cache, p)
			b, _ := os.ReadFile(p)
			tree = append(tree, [2]string{rel, base64.StdEncoding.EncodeToString(b)})
		}
		return nil
	})
	sort.Slice(tree, func(i, j int) bool { return tree[i][0] < tree[j][0] })
	if !sameTree(tree, g.Cache) {
		t.Errorf("cache\n got %v\nwant %v", tree, g.Cache)
	}
}

func sameTree(a, b [][2]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
