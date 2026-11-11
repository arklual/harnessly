package filesync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// testPart is the part size of the tests' store: a few parts make a big file.
const testPart = 64 << 10

// slowFinish makes the test store answer a complete with 202.
var slowFinish atomic.Bool

func uploadStore(t *testing.T, failPart *atomic.Int32, parts *atomic.Int32) *httptest.Server {
	t.Helper()
	srv, err := filestore.OpenFolder(t.TempDir(), func(*http.Request, string) (filestore.Access, error) {
		return filestore.Access{Member: "vm-1", Room: filestore.KindUploads}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	srv.SetPartSize(testPart)
	h := srv.Handler()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete") && slowFinish.Load() {
			// The store finishes, the caller is told it takes a while.
			h.ServeHTTP(httptest.NewRecorder(), r)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/parts/") {
			parts.Add(1)
			if strings.HasSuffix(r.URL.Path, "/parts/1") && failPart.Load() > 0 {
				failPart.Add(-1)
				http.Error(w, "the line broke", http.StatusBadGateway)
				return
			}
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(hs.Close)
	return hs
}

func TestPutLargeGoesOnFromThePartsTheStoreHas(t *testing.T) {
	saved := partTries
	partTries = []time.Duration{0, 0}
	t.Cleanup(func() { partTries = saved })
	var failPart, parts atomic.Int32
	failPart.Store(int32(len(partTries)))
	hs := uploadStore(t, &failPart, &parts)
	c := NewClient(hs.URL, filestore.KindUploads, nil, nil)
	ctx := context.Background()

	lim, err := c.UploadLimits(ctx)
	if err != nil || lim.MaxSize != 0 || lim.PartSize != testPart {
		t.Fatalf("limits %+v %v", lim, err)
	}
	data := make([]byte, 2*testPart+7)
	_, _ = rand.Read(data)
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	// Part 1 fails every try: the upload stops there, with part 0 kept.
	if err := c.PutLarge(ctx, sha, int64(len(data)), bytes.NewReader(data)); err == nil {
		t.Fatal("an upload whose part failed answered done")
	}
	first := parts.Load()
	if first != 1+int32(len(partTries)) {
		t.Fatalf("%d part requests before the stop", first)
	}
	// The next call sends only parts 1 and 2.
	if err := c.PutLarge(ctx, sha, int64(len(data)), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if sent := parts.Load() - first; sent != 2 {
		t.Fatalf("%d parts sent again, want 2", sent)
	}
	var got bytes.Buffer
	if err := c.GetBlob(ctx, sha, &got); err != nil || !bytes.Equal(got.Bytes(), data) {
		t.Fatalf("the blob: %v, %d bytes", err, got.Len())
	}
	// Contents the store holds are not sent again.
	before := parts.Load()
	if err := c.PutLarge(ctx, sha, int64(len(data)), bytes.NewReader(data)); err != nil || parts.Load() != before {
		t.Fatalf("held contents: %v, %d parts sent", err, parts.Load()-before)
	}
}

// A store that takes a while to put the contents together answers 202; the client asks until they are a blob.
func TestPutLargeWaitsForAFinishThatTakesAWhile(t *testing.T) {
	saved := pollEvery
	pollEvery = 10 * time.Millisecond
	slowFinish.Store(true)
	t.Cleanup(func() { pollEvery = saved; slowFinish.Store(false) })
	var failPart, parts atomic.Int32
	hs := uploadStore(t, &failPart, &parts)
	c := NewClient(hs.URL, filestore.KindUploads, nil, nil)
	ctx := context.Background()
	data := make([]byte, 3*testPart+1)
	_, _ = rand.Read(data)
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if err := c.PutLarge(ctx, sha, int64(len(data)), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.HasBlob(ctx, sha); err != nil || !ok {
		t.Fatalf("the blob after a slow finish: %v %v", ok, err)
	}
}

func TestPutLargeToAStoreWithoutUploadsIsTooBig(t *testing.T) {
	hs := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(hs.Close)
	c := NewClient(hs.URL, filestore.KindUploads, nil, nil)
	lim, err := c.UploadLimits(context.Background())
	if err != nil || lim.MaxSize != 0 {
		t.Fatalf("an older store's limits: %+v %v", lim, err)
	}
	err = c.PutLarge(context.Background(), strings.Repeat("a", 64), 10, bytes.NewReader(make([]byte, 10)))
	if !errors.Is(err, ErrTooBig) {
		t.Fatalf("an older store: %v", err)
	}
}
