package proxy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// HRN-879: what a person attaches is put into the hub's store by its sha256 and named by the machine's upload id; the
// chat reads it from the hub, the machine on or off, and an upload the hub has not got is read from the machine.
func TestWorkspaceKeepsTheChatsUploadsInTheHub(t *testing.T) {
	w := setupWorkspace(t, fleetOn)
	png := []byte("\x89PNG\r\n\x1a\n-a picture-")
	sum := sha256.Sum256(png)
	sha := hex.EncodeToString(sum[:])
	q := "?machine=" + w.home
	base := "/agents/api/acme/uploads/"

	if r := w.owner.Do("PUT", base+"blobs/"+strings.Repeat("0", 64)+q, png); r.Code != 400 {
		t.Errorf("bytes under another hash: %s", r)
	}
	if r := w.owner.Do("PUT", base+"refs/up_AAAAAAAA"+q, map[string]any{"sha256": sha, "mime": "image/png", "name": "a.png", "size": len(png)}); r.Code != 400 {
		t.Errorf("a ref to a blob the hub has not got: %s", r)
	}
	if r := w.owner.Do("PUT", base+"blobs/"+sha+q, png); r.Code != 204 {
		t.Fatalf("put: %s", r)
	}
	if r := w.owner.Do("PUT", base+"refs/up_AAAAAAAA"+q, map[string]any{"sha256": sha, "mime": "image/png", "name": "a.png", "size": len(png)}); r.Code != 204 {
		t.Fatalf("ref: %s", r)
	}
	r := w.owner.Do("GET", base+"files/up_AAAAAAAA"+q, nil)
	if r.Code != 200 || string(r.Body) != string(png) || r.Header.Get("Content-Type") != "image/png" || !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("the upload from the hub: %s %v", r, r.Header)
	}
	if !strings.HasPrefix(r.Header.Get("Content-Disposition"), "inline") || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers %v", r.Header)
	}
	// A page is never served as one: anything outside the known kinds is a download.
	if r := w.owner.Do("GET", base+"blobs/"+sha+q+"&type=text/html", nil); r.Code != 200 || r.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("as html: %s %v", r, r.Header)
	}
	// Not in the hub: the machine's own copy.
	if r := w.owner.Do("GET", base+"files/up_BBBBBBBB"+q, nil); r.Code != 307 || r.Header.Get("Location") != "/agents/api/v2/uploads/up_BBBBBBBB?machine="+w.home {
		t.Errorf("an upload the hub has not got: %s %v", r, r.Header)
	}
	// Somebody who may not open the machine reads nothing.
	stranger := w.s.SignIn("stranger@example.com")
	if r := stranger.Do("GET", base+"files/up_AAAAAAAA"+q, nil); r.Code == 200 {
		t.Errorf("a stranger reads the upload: %s", r)
	}
	if r := stranger.Do("PUT", base+"blobs/"+sha+q, png); r.Code == 204 {
		t.Errorf("a stranger writes into the machine's space: %s", r)
	}
	// Somebody else of the space reads none of it by its hash, and names no upload with it.
	home, _ := w.s.DB.S().MachineByID(context.Background(), w.home)
	member := w.s.SignIn("member@example.com")
	w.owner.Join(home.SpaceID, member)
	// Naming the owner's hash with other bytes makes nobody an owner of it.
	if r := member.Do("PUT", base+"blobs/"+sha+q, []byte("other bytes")); r.Code == 204 {
		t.Errorf("a member claims the owner's bytes with other ones: %s", r)
	}
	if r := member.Do("GET", base+"blobs/"+sha+q+"&type=image/png", nil); r.Code == 200 {
		t.Errorf("a member reads the owner's upload by its hash: %s", r)
	}
	if r := member.Do("PUT", base+"refs/up_CCCCCCCC"+q, map[string]any{"sha256": sha, "mime": "image/png", "name": "a.png", "size": len(png)}); r.Code == 204 {
		t.Errorf("a member names an upload with the owner's bytes: %s", r)
	}
	// The page is told to put uploads into the hub.
	if r := w.owner.Do("GET", "/agents/", nil, navigate...); !strings.Contains(string(r.Body), "&#34;centralUploads&#34;:true") {
		t.Errorf("the page is not told: %s", r.Body)
	}
}

// HRN-879: a file the agent handed over is put by the machine into its space's `files` collection; the chat's card reads
// it from the hub, and one the hub has not got from the machine's own preview of the path.
func TestWorkspaceReadsTheHandedOverFilesFromTheHub(t *testing.T) {
	w := setupWorkspace(t, fleetOn)
	pdf := []byte("%PDF-1.7 a report")
	sum := sha256.Sum256(pdf)
	sha := hex.EncodeToString(sum[:])
	m, err := w.s.DB.S().MachineByID(context.Background(), w.home)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.s.Hub.Store.PutBlob(context.Background(), m.SpaceID, filestore.KindFiles, sha, bytes.NewReader(pdf)); err != nil {
		t.Fatal(err)
	}
	base := "/agents/api/acme/uploads/delivered/"
	q := "?machine=" + w.home + "&type=application/pdf&path=/home/u/report.pdf"
	// In the store, but in none of the person's chats: the machine answers, as for a file the hub has not got.
	if r := w.owner.Do("GET", base+sha+q, nil); r.Code != 307 {
		t.Fatalf("a file no chat of the person shows: %s", r)
	}
	turn := `{"type":"feed_reset","turns":[{"id":"c:t1#0","startedAt":1,"endedAt":2,"status":"done","outputs":[` +
		`{"kind":"delivery","op":"created","title":"отчёт.pdf","ref":"/home/u/report.pdf","mime":"application/pdf","sha256":"` + sha + `"}]}],` +
		`"entries":[],"beforeTurnId":null}`
	if err := history.ApplyBackfill(context.Background(), w.s.DB, history.Owner{MachineID: w.home, UserID: w.owner.ID},
		hubproto.HistoryBackfill{Ch: "conv:tab-a", Events: []json.RawMessage{json.RawMessage(turn)}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	q = "?machine=" + w.home + "&type=application/pdf&name=" + url.QueryEscape("отчёт.pdf") + "&path=/home/u/report.pdf"
	r := w.owner.Do("GET", base+sha+q, nil)
	if r.Code != 200 || string(r.Body) != string(pdf) || r.Header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("the file from the hub: %s %v", r, r.Header)
	}
	// The pane, by the path alone.
	if r := w.owner.Do("GET", base+"by-path?machine="+w.home+"&path=/home/u/report.pdf", nil); r.Code != 200 || string(r.Body) != string(pdf) || r.Header.Get("Content-Type") != "application/pdf" {
		t.Errorf("by path: %s %v", r, r.Header)
	}
	if r := w.owner.Do("GET", base+"by-path?machine="+w.home+"&path=/home/u/other.pdf", nil); r.Code != 307 {
		t.Errorf("a path no turn names: %s", r)
	}
	if r := w.owner.Do("GET", base+sha+q+"&download=1", nil); r.Code != 200 || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("a download: %s %v", r, r.Header)
	}
	// Not in the hub: the machine's preview of the path.
	other := strings.Repeat("a", 64)
	want := "/agents/api/acme/file-preview?machine=" + w.home + "&path=%2Fhome%2Fu%2Freport.pdf"
	if r := w.owner.Do("GET", base+other+q, nil); r.Code != 307 || r.Header.Get("Location") != want {
		t.Errorf("a file the hub has not got: %s %v", r, r.Header)
	}
	if r := w.owner.Do("GET", base+other+"?machine="+w.home, nil); r.Code != 404 {
		t.Errorf("without a path: %s", r)
	}
	// Another space's machine does not read this space's files.
	if r := w.owner.Do("GET", base+sha+"?machine="+w.other+"&type=application/pdf", nil); r.Code == 200 {
		t.Errorf("read through another space: %s", r)
	}
}
