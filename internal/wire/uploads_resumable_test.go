package wire_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// patternReader makes n bytes without holding them.
type patternReader struct{ left, at int64 }

func (p *patternReader) Read(b []byte) (int, error) {
	if p.left <= 0 {
		return 0, io.EOF
	}
	n := int64(len(b))
	if n > p.left {
		n = p.left
	}
	for i := int64(0); i < n; i++ {
		b[i] = byte((p.at + i) * 31 % 251)
	}
	p.left -= n
	p.at += n
	return int(n), nil
}

func resumableRig(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	srv := &wire.Server{Uploads: &wire.Uploads{Dir: dir, Limits: wire.DefaultLimits}}
	h := httptest.NewServer(srv)
	t.Cleanup(h.Close)
	return h, dir
}

func call(t *testing.T, method, url string, body io.Reader, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return res, out
}

func begin(t *testing.T, h *httptest.Server, name string, size int64, typ string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "size": size, "type": typ})
	res, out := call(t, "POST", h.URL+"/api/v2/uploads", bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if res.StatusCode != 201 {
		t.Fatalf("begin: %d %v", res.StatusCode, out)
	}
	return out
}

func TestResumableUploadBeyondTheOldLimitSurvivesABreak(t *testing.T) {
	h, dir := resumableRig(t)
	size := int64(120 << 20) // past the 50 MB a one-shot PUT used to take
	s := begin(t, h, "../evil/big.zip", size, "application/zip")
	id := s["id"].(string)
	chunk := int64(s["chunkSize"].(float64))
	base := h.URL + "/api/v2/uploads/" + id

	var offset int64
	send := func(from, to int64, cut int64) *http.Response {
		r := io.LimitReader(&patternReader{left: to - from, at: from}, to-from)
		if cut > 0 { // a connection that breaks inside the chunk: the body ends early
			r = io.LimitReader(&patternReader{left: to - from, at: from}, cut)
		}
		req, _ := http.NewRequest("PATCH", base, r)
		req.ContentLength = to - from
		req.Header.Set("Upload-Offset", strconv.FormatInt(from, 10))
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			defer res.Body.Close()
		}
		if err != nil {
			return nil
		}
		return res
	}
	// A chunk is cut short: whatever arrived stays, HEAD tells where to go on from.
	send(0, chunk, chunk/2)
	time.Sleep(200 * time.Millisecond)
	head, _ := call(t, "HEAD", base, nil, nil)
	offset, _ = strconv.ParseInt(head.Header.Get("Upload-Offset"), 10, 64)
	if offset <= 0 || offset > chunk/2 {
		t.Fatalf("offset after a break = %d", offset)
	}
	// A wrong offset is refused with the right one.
	var out map[string]any
	var res *http.Response
	for range 50 { // the runtime may still be finishing the cut chunk
		res, out = call(t, "PATCH", base, strings.NewReader("x"), map[string]string{"Upload-Offset": "7"})
		if res.StatusCode != 429 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if res.StatusCode != 409 || int64(out["offset"].(float64)) != offset {
		t.Fatalf("mismatch: %d %v", res.StatusCode, out)
	}
	for offset < size {
		end := min(offset+chunk, size)
		r := send(offset, end, 0)
		if r == nil || r.StatusCode != 200 {
			t.Fatalf("chunk at %d: %v", offset, r)
		}
		offset = end
	}
	// Not complete is refused, complete is renamed into place.
	res, out = call(t, "POST", base+"/complete", nil, nil)
	if res.StatusCode != 200 || out["name"] != "big.zip" || out["kind"] != "file" {
		t.Fatalf("complete: %d %v", res.StatusCode, out)
	}
	got, err := os.Open(filepath.Join(dir, id+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	// Byte-for-byte what was sent.
	sum := sha256.New()
	_, _ = io.Copy(sum, &patternReader{left: size})
	check := sha256.New()
	n, _ := io.Copy(check, got)
	if n != size || !bytes.Equal(sum.Sum(nil), check.Sum(nil)) {
		t.Fatalf("the stored file (%d of %d bytes) differs from the sent one", n, size)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("temporary leftovers: %v", entries)
	}
	// The finished upload is served by the same GET as before.
	if res, _ := call(t, "HEAD", base, nil, nil); res.StatusCode != 200 {
		t.Fatalf("head of a finished upload: %d", res.StatusCode)
	}
}

func TestResumableUploadRefusals(t *testing.T) {
	h, dir := resumableRig(t)
	post := func(v map[string]any) (*http.Response, map[string]any) {
		b, _ := json.Marshal(v)
		return call(t, "POST", h.URL+"/api/v2/uploads", bytes.NewReader(b), nil)
	}
	if res, _ := post(map[string]any{"name": "..", "size": 1}); res.StatusCode != 400 {
		t.Fatalf("name ..: %d", res.StatusCode)
	}
	if res, out := post(map[string]any{"name": "p.png", "size": 11 << 20, "type": "image/png"}); res.StatusCode != 413 || out["error"] != "too-large" {
		t.Fatalf("big picture: %d %v", res.StatusCode, out)
	}
	if res, out := post(map[string]any{"name": "a.bin", "size": int64(1) << 60}); res.StatusCode != 507 || out["error"] != "no-space" {
		t.Fatalf("no space: %d %v", res.StatusCode, out)
	}
	s := begin(t, h, "a.txt", 4, "text/plain")
	base := h.URL + "/api/v2/uploads/" + s["id"].(string)
	if res, out := call(t, "PATCH", base, strings.NewReader("abcdef"), map[string]string{"Upload-Offset": "0"}); res.StatusCode != 413 {
		t.Fatalf("a chunk past the end: %d %v", res.StatusCode, out)
	}
	if res, out := call(t, "POST", base+"/complete", nil, nil); res.StatusCode != 409 || out["error"] != "incomplete" {
		t.Fatalf("complete early: %d %v", res.StatusCode, out)
	}
	if res, _ := call(t, "DELETE", base, nil, nil); res.StatusCode != 204 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("left after delete: %v", entries)
	}
	if res, _ := call(t, "PATCH", h.URL+"/api/v2/uploads/up_AAAAAAAAAAAAAAAA", strings.NewReader("x"), map[string]string{"Upload-Offset": "0"}); res.StatusCode != 404 {
		t.Fatalf("unknown id: %d", res.StatusCode)
	}
}
