package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// a runtime without a hub writes its entities into the embedded store and the check says they are all there.
func TestRecordsReachTheEmbeddedStore(t *testing.T) {
	l, err := storelink.Open(storelink.Options{DataDir: t.TempDir(), Name: "box"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	recs := []filestore.Record{{ID: "shop", Title: "Shop", Kind: "site", At: 1}, {ID: "kit-a", Title: "Kit", Kind: "ds", At: 2}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); startRecords(ctx, l, func() []filestore.Record { return recs }, nil, nil) }()
	deadline := time.Now().Add(20 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		if recordsLive.Load() != nil {
			rr := httptest.NewRecorder()
			h := designRecordsRoutes()[0]
			req := httptest.NewRequest(http.MethodGet, platformAPI+"/design/records-check", nil)
			if h.Match(req) {
				h.Handler.ServeHTTP(rr, req)
				body = rr.Body.String()
				if strings.Contains(body, `"OK":true`) || strings.Contains(body, `"ok":true`) {
					break
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(strings.ToLower(body), `"ok":true`) {
		t.Fatalf("the store lacks the entities: %s", body)
	}
}
