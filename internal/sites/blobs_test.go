package sites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// blobSuite is what every Blobs must do.
func blobSuite(t *testing.T, b Blobs) {
	ctx := context.Background()
	data := []byte("hello sites")
	h := sum(data)
	if ok, err := b.Has(ctx, h); err != nil || ok {
		t.Fatalf("has before put: %v %v", ok, err)
	}
	if _, _, err := b.Open(ctx, h); err != ErrBlobNotFound {
		t.Fatalf("open before put: %v", err)
	}
	if err := b.Put(ctx, h, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(ctx, h, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("put twice: %v", err)
	}
	if ok, err := b.Has(ctx, h); err != nil || !ok {
		t.Fatalf("has after put: %v %v", ok, err)
	}
	rc, size, err := b.Open(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if size != int64(len(data)) || !bytes.Equal(got, data) {
		t.Fatalf("open: %d %q", size, got)
	}
	if err := b.Delete(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, h); err != nil {
		t.Fatalf("delete twice: %v", err)
	}
	if ok, _ := b.Has(ctx, h); ok {
		t.Fatal("still there")
	}
	if _, err := b.Has(ctx, "../../etc/passwd"); err == nil {
		t.Fatal("a path is not a hash")
	}
}

func TestFolderBlobs(t *testing.T) {
	b, err := NewFolderBlobs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blobSuite(t, b)
	if err := b.Put(context.Background(), sum([]byte("x")), strings.NewReader("short"), 10); err == nil {
		t.Fatal("a short body must fail")
	}
}

// fakeS3 is a path-style S3 that keeps objects in memory; it answers only what the store asks.
func fakeS3(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	objs := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			http.Error(w, "unsigned", http.StatusForbidden)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		key := r.URL.Path
		noKey := func() {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>no</Message></Error>`)
		}
		if strings.TrimSuffix(key, "/") == "/sites" {
			// bucket level: the client asks where the bucket lives
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
			return
		}
		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
				b = unchunk(b)
			}
			objs[key] = b
			w.Header().Set("ETag", `"x"`)
		case http.MethodGet, http.MethodHead:
			b, ok := objs[key]
			if !ok {
				noKey()
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			w.Header().Set("ETag", `"x"`)
			w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
			if r.Method == http.MethodGet {
				_, _ = w.Write(b)
			}
		case http.MethodDelete:
			delete(objs, key)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestS3Blobs(t *testing.T) {
	srv := fakeS3(t)
	b, err := NewS3Blobs(S3Config{Endpoint: srv.URL, Bucket: "sites", Prefix: "p/", AccessKey: "ak", SecretKey: "sk"})
	if err != nil {
		t.Fatal(err)
	}
	blobSuite(t, b)
}

func TestS3BlobsConfig(t *testing.T) {
	for _, c := range []S3Config{
		{Endpoint: "nope", Bucket: "b", AccessKey: "a", SecretKey: "s"},
		{Endpoint: "http://x", AccessKey: "a", SecretKey: "s"},
		{Endpoint: "http://x", Bucket: "b"},
	} {
		if _, err := NewS3Blobs(c); err == nil {
			t.Errorf("%+v should be refused", c)
		}
	}
}

// unchunk decodes the aws-chunked body a client sends over plain http: <hex size>;chunk-signature=…\r\n<data>\r\n….
func unchunk(b []byte) []byte {
	var out []byte
	for len(b) > 0 {
		i := bytes.Index(b, []byte("\r\n"))
		if i < 0 {
			break
		}
		head, _, _ := strings.Cut(string(b[:i]), ";")
		n, err := strconv.ParseInt(head, 16, 64)
		if err != nil || n == 0 {
			break
		}
		b = b[i+2:]
		out = append(out, b[:n]...)
		b = b[n+2:]
	}
	return out
}
