package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// the bucket answers 409 OperationAborted while another write of the same key is under way (two machines
// putting the same blob at once, Selectel S3). A put waits and finds the blob there; a look waits and looks again.
func TestABusyBucketKeyIsWaitedFor(t *testing.T) {
	was := busyWaits
	busyWaits = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { busyWaits = was }()

	body := "the same bytes"
	sum := sha256.Sum256([]byte(body))
	sha := hex.EncodeToString(sum[:])
	var mu sync.Mutex
	var log []string
	stored, heads := false, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		log = append(log, r.Method)
		switch r.Method {
		case http.MethodHead:
			heads++
			switch {
			case heads == 3: // the look of a reader while the other write is under way
				w.WriteHeader(http.StatusConflict)
			case stored:
				w.Header().Set("Content-Length", "14")
				w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
				w.Header().Set("ETag", `"x"`)
				w.WriteHeader(http.StatusOK)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		case http.MethodPut:
			stored = true // the other machine's write lands; this one is told the key is busy
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>OperationAborted</Code><Message>A conflicting conditional operation is currently in progress against this resource. Try again.</Message></Error>`))
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	defer srv.Close()
	b, err := NewS3BlobsWith(S3Config{Endpoint: srv.URL, Region: "us-east-1", Bucket: "bucket", AccessKey: "a", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := b.Put(ctx, "design", sha, strings.NewReader(body)); err != nil {
		t.Fatalf("a put that met another write of the same blob: %v (requests %v)", err, log)
	}
	ok, err := b.Has(ctx, "design", sha)
	if err != nil || !ok {
		t.Fatalf("a look while the key was busy: %v %v (requests %v)", ok, err, log)
	}
}
