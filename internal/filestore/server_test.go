package filestore

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The wire API is what agents speak; it must not depend on the drivers underneath.

func wireServer(t *testing.T, r *rig) *httptest.Server {
	t.Helper()
	// The caller is the bearer's machine; it may open design and uploads, and every collection is its own room.
	api := NewServer(r.meta, r.blobs, func(req *http.Request, coll string) (Access, error) {
		if req.Header.Get("Authorization") != "Bearer "+machineTok {
			return Access{}, ErrUnauthorized
		}
		if coll != KindDesign && coll != KindUploads {
			return Access{}, ErrForbidden
		}
		return Access{Member: "vm-1", Room: coll}, nil
	})
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	// The rig closes the drivers; the server must not close them twice.
	return srv
}

const machineTok = "0123456789abcdef0123456789abcdef"

const designAPI, uploadsAPI = "/v1/collections/design", "/v1/collections/uploads"

func do(t *testing.T, method, url string, body io.Reader) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, body)
	req.Header.Set("Authorization", "Bearer "+machineTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestWireAPIOverEveryDriverCombination(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv := wireServer(t, r)
		const body = "<x>wire</x>"
		sha := sum([]byte(body))

		if c := do(t, "HEAD", srv.URL+designAPI+"/blobs/"+sha, nil).StatusCode; c != 404 {
			t.Fatalf("absent blob: %d", c)
		}
		commit := func(base int64) (*http.Response, CommitResponse) {
			b, _ := json.Marshal(CommitRequest{Path: "proj/a.html", Base: base, Sha: sha, Tag: sha, Size: int64(len(body))})
			resp := do(t, "POST", srv.URL+designAPI+"/commit", bytes.NewReader(b))
			var out CommitResponse
			_ = json.NewDecoder(resp.Body).Decode(&out)
			return resp, out
		}
		if resp, _ := commit(0); resp.StatusCode != http.StatusPreconditionRequired {
			t.Fatalf("a commit before the upload must be 428, got %d", resp.StatusCode)
		}
		if c := do(t, "PUT", srv.URL+designAPI+"/blobs/"+sha, strings.NewReader("tampered")).StatusCode; c != 400 {
			t.Fatalf("wrong contents: %d", c)
		}
		if c := do(t, "PUT", srv.URL+designAPI+"/blobs/"+sha, strings.NewReader(body)).StatusCode; c != 204 {
			t.Fatalf("put: %d", c)
		}
		if c := do(t, "HEAD", srv.URL+designAPI+"/blobs/"+sha, nil).StatusCode; c != 200 {
			t.Fatalf("head: %d", c)
		}
		resp := do(t, "GET", srv.URL+designAPI+"/blobs/"+sha, nil)
		if got, _ := io.ReadAll(resp.Body); resp.StatusCode != 200 || string(got) != body {
			t.Fatalf("get: %d %q", resp.StatusCode, got)
		}
		if c := do(t, "GET", srv.URL+designAPI+"/blobs/"+sum([]byte("nope")), nil).StatusCode; c != 404 {
			t.Fatalf("get absent: %d", c)
		}
		if resp, out := commit(0); resp.StatusCode != 200 || out.Entry.Rev != 1 || out.Conflict {
			t.Fatalf("commit: %d %+v", resp.StatusCode, out)
		}
		if resp, out := commit(0); resp.StatusCode != http.StatusConflict || !out.Conflict || out.Entry.Rev != 1 {
			t.Fatalf("stale commit must be 409 with the current revision: %d %+v", resp.StatusCode, out)
		}

		// A long poll is held until a write lands, then answers with it.
		done := make(chan Changes, 1)
		go func() {
			resp := do(t, "GET", srv.URL+designAPI+"/changes?since=1&wait=10", nil)
			var ch Changes
			_ = json.NewDecoder(resp.Body).Decode(&ch)
			done <- ch
		}()
		time.Sleep(300 * time.Millisecond)
		select {
		case <-done:
			t.Fatal("the long poll answered with nothing new")
		default:
		}
		const body2 = "<x>second</x>"
		sha2 := sum([]byte(body2))
		do(t, "PUT", srv.URL+designAPI+"/blobs/"+sha2, strings.NewReader(body2))
		b, _ := json.Marshal(CommitRequest{Path: "proj/b.html", Sha: sha2, Tag: sha2, Size: int64(len(body2))})
		do(t, "POST", srv.URL+designAPI+"/commit", bytes.NewReader(b))
		select {
		case ch := <-done:
			if len(ch.Entries) != 1 || ch.Entries[0].Path != "proj/b.html" || ch.Rev != 2 {
				t.Fatalf("long poll: %+v", ch)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a write must end the long poll")
		}

		resp = do(t, "GET", srv.URL+designAPI+"/history?path=proj/a.html", nil)
		var hist []Entry
		_ = json.NewDecoder(resp.Body).Decode(&hist)
		if len(hist) != 1 || hist[0].Rev != 1 {
			t.Fatalf("history: %+v", hist)
		}

	})
}

func TestWireRefusesUnknownCallersAndCollections(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv := wireServer(t, r)
		req, _ := http.NewRequest("GET", srv.URL+designAPI+"/changes", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("no token: %d", resp.StatusCode)
		}
		if c := do(t, "GET", srv.URL+"/v1/collections/nope/changes", nil).StatusCode; c != 404 {
			t.Fatalf("unknown collection: %d", c)
		}
		if c := do(t, "GET", srv.URL+"/v1/collections/files/changes", nil).StatusCode; c != 403 {
			t.Fatalf("forbidden collection: %d", c)
		}
	})
}

// A collection of files takes any clean path and serves the contents by hash; its record is in the database, so the
// collector keeps the blob.
func TestUploadsCollectionKeepsABlobByHash(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv := wireServer(t, r)
		const body = "\x89PNG picture"
		sha := sum([]byte(body))
		if c := do(t, "PUT", srv.URL+uploadsAPI+"/blobs/"+sha, strings.NewReader(body)).StatusCode; c != 204 {
			t.Fatalf("put: %d", c)
		}
		b, _ := json.Marshal(CommitRequest{Path: "chat-1/.hidden/shot.png", Sha: sha, Size: int64(len(body))})
		resp := do(t, "POST", srv.URL+uploadsAPI+"/commit", bytes.NewReader(b))
		if resp.StatusCode != 200 {
			t.Fatalf("commit into uploads: %d", resp.StatusCode)
		}
		resp = do(t, "GET", srv.URL+uploadsAPI+"/blobs/"+sha, nil)
		if got, _ := io.ReadAll(resp.Body); resp.StatusCode != 200 || string(got) != body || resp.Header.Get("ETag") != `"`+sha+`"` {
			t.Fatalf("get by hash: %d %q", resp.StatusCode, got)
		}
		var cur Entry
		resp = do(t, "GET", srv.URL+uploadsAPI+"/current?path=chat-1/.hidden/shot.png", nil)
		if err := json.NewDecoder(resp.Body).Decode(&cur); err != nil || cur.Sha != sha {
			t.Fatalf("current: %+v %v", cur, err)
		}
		// The design library does not take hidden paths, nor see the uploads' contents.
		if c := do(t, "POST", srv.URL+designAPI+"/commit", bytes.NewReader(b)).StatusCode; c != 400 {
			t.Fatalf("a hidden path in design: %d", c)
		}
		if c := do(t, "HEAD", srv.URL+designAPI+"/blobs/"+sha, nil).StatusCode; c != 404 {
			t.Fatalf("design must not see an upload: %d", c)
		}
		if ok, _ := r.meta.Room(bg, KindUploads); ok == nil {
			t.Fatal("no uploads room")
		}
		rm, _ := r.meta.Room(bg, KindUploads)
		if ref, err := rm.Referenced(bg, sha); err != nil || !ref {
			t.Fatalf("the upload's blob must be referenced by its record: %v %v", ref, err)
		}
	})
}

func TestLeasesOverTheWire(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv := wireServer(t, r)
		post := func(path, body string) (*http.Response, LeaseResponse) {
			resp := do(t, "POST", srv.URL+path, strings.NewReader(body))
			var out LeaseResponse
			_ = json.NewDecoder(resp.Body).Decode(&out)
			return resp, out
		}
		resp, out := post(designAPI+"/leases", `{"key":"p/Оплата","chat":"c1","user":"Анна","chat_title":"Оплата","edit":true,"machine":"spoofed"}`)
		if resp.StatusCode != 200 || out.Lease.Machine != "vm-1" {
			t.Fatalf("take: %d %+v (the machine is the token's, not the body's)", resp.StatusCode, out)
		}
		resp, out = post(designAPI+"/leases", `{"key":"p/Оплата","chat":"c2"}`)
		if resp.StatusCode != 409 || !out.Held || out.Lease.Chat != "c1" || out.Lease.ChatTitle != "Оплата" {
			t.Fatalf("second chat: %d %+v", resp.StatusCode, out)
		}
		var ls Leases
		_ = json.NewDecoder(do(t, "GET", srv.URL+designAPI+"/leases", nil).Body).Decode(&ls)
		if len(ls.Leases) != 1 {
			t.Fatalf("list = %+v", ls)
		}
		if resp, _ = post(designAPI+"/leases/release", `{"key":"p/Оплата","force":true}`); resp.StatusCode != 200 {
			t.Fatalf("force: %d", resp.StatusCode)
		}
		if resp, _ = post(designAPI+"/leases", `{"key":"p/Оплата","chat":"c2"}`); resp.StatusCode != 200 {
			t.Fatalf("after force c2 takes it: %d", resp.StatusCode)
		}
		if resp, _ = post(designAPI+"/leases", `{"key":"","chat":"c2"}`); resp.StatusCode != 400 {
			t.Fatalf("empty key: %d", resp.StatusCode)
		}
	})
}

func TestMissingBlobsAnswersABatchLikeALookAtEach(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv := wireServer(t, r)
		const body = "\x89PNG picture"
		have, lack := sum([]byte(body)), sum([]byte("never uploaded"))
		if c := do(t, "PUT", srv.URL+uploadsAPI+"/blobs/"+have, strings.NewReader(body)).StatusCode; c != 204 {
			t.Fatalf("put: %d", c)
		}
		ask := func(api string, shas ...string) (*http.Response, []string) {
			b, _ := json.Marshal(MissingBlobsRequest{Shas: shas})
			resp := do(t, "POST", srv.URL+api+"/blobs/missing", bytes.NewReader(b))
			var out MissingBlobsResponse
			_ = json.NewDecoder(resp.Body).Decode(&out)
			return resp, out.Missing
		}
		resp, missing := ask(uploadsAPI, have, lack, lack)
		if resp.StatusCode != 200 || len(missing) != 1 || missing[0] != lack {
			t.Fatalf("missing: %d %v, want only %s", resp.StatusCode, missing, lack)
		}
		if resp, missing := ask(uploadsAPI); resp.StatusCode != 200 || len(missing) != 0 {
			t.Fatalf("nothing asked: %d %v", resp.StatusCode, missing)
		}
		// Another collection is another room: it does not hold the upload, as a look at the one blob says too.
		if resp, missing := ask(designAPI, have); resp.StatusCode != 200 || len(missing) != 1 {
			t.Fatalf("design must not see an upload: %d %v", resp.StatusCode, missing)
		}
		if resp, _ := ask(uploadsAPI, "not-a-hash"); resp.StatusCode != 400 {
			t.Fatalf("a name that is no hash: %d", resp.StatusCode)
		}
		many := make([]string, MaxBlobCheck+1)
		for i := range many {
			many[i] = have
		}
		if resp, _ := ask(uploadsAPI, many...); resp.StatusCode != 400 {
			t.Fatalf("more than the limit: %d", resp.StatusCode)
		}
		// The caller is judged as for one blob: a collection it may not open, and a caller nobody knows.
		if resp, _ := ask("/v1/collections/"+KindFiles, have); resp.StatusCode != 403 {
			t.Fatalf("a collection the caller may not open: %d", resp.StatusCode)
		}
		b, _ := json.Marshal(MissingBlobsRequest{Shas: []string{have}})
		anon, err := http.Post(srv.URL+uploadsAPI+"/blobs/missing", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		anon.Body.Close()
		if anon.StatusCode != 401 {
			t.Fatalf("an unknown caller: %d", anon.StatusCode)
		}
	})
}
