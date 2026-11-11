package filestore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// testPartSize keeps the tests' files small: a few parts of 64 KiB, or of 5 MiB in a bucket (the smallest part it
// takes but the last).
func testPartSize(r *rig) int64 {
	if r.s3 != nil {
		return 5 << 20
	}
	return 64 << 10
}

// uploadServer is the API over blobs (the rig's when nil) with the caller named by X-Member (vm-1 when unset), all in
// the room uploads, with the tests' part size.
func uploadServer(t *testing.T, r *rig, blobs BlobStore) (*httptest.Server, *Server) {
	t.Helper()
	if blobs == nil {
		blobs = r.blobs
	}
	api := NewServer(r.meta, blobs, func(req *http.Request, coll string) (Access, error) {
		if req.Header.Get("Authorization") != "Bearer "+machineTok {
			return Access{}, ErrUnauthorized
		}
		member := req.Header.Get("X-Member")
		if member == "" {
			member = "vm-1"
		}
		return Access{Member: member, Room: KindUploads}, nil
	})
	api.SetPartSize(testPartSize(r))
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, api
}

func callAs(t *testing.T, member, method, url string, body []byte) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("Authorization", "Bearer "+machineTok)
	if member != "" {
		req.Header.Set("X-Member", member)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func openUploadAs(t *testing.T, member, base string, data []byte) (int, UploadState, []byte) {
	t.Helper()
	body, _ := json.Marshal(UploadRequest{Sha: sum(data), Size: int64(len(data))})
	code, out := callAs(t, member, "POST", base+"/uploads", body)
	var st UploadState
	if code == 200 && json.Unmarshal(out, &st) != nil {
		t.Fatalf("open: %s", out)
	}
	return code, st, out
}

func openUpload(t *testing.T, base string, data []byte) UploadState {
	t.Helper()
	code, st, out := openUploadAs(t, "", base, data)
	if code != 200 {
		t.Fatalf("open: %d %s", code, out)
	}
	return st
}

func partOf(data []byte, ps int64, n int) []byte {
	end := min(int64(len(data)), int64(n+1)*ps)
	return data[int64(n)*ps : end]
}

func sendPart(t *testing.T, member, base, id string, n int, part []byte) (int, []byte) {
	t.Helper()
	return callAs(t, member, "PUT", fmt.Sprintf("%s/uploads/%s/parts/%d?sha=%s", base, id, n, sum(part)), part)
}

func uploadStateOf(t *testing.T, base, id string) UploadState {
	t.Helper()
	code, out := callAs(t, "", "GET", base+"/uploads/"+id, nil)
	var st UploadState
	if code != 200 || json.Unmarshal(out, &st) != nil {
		t.Fatalf("state: %d %s", code, out)
	}
	return st
}

func randBytes(n int64) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// nothingWritten checks a folder store keeps nothing of an upload once it is finished or gone.
func nothingWritten(t *testing.T, r *rig, id string) {
	t.Helper()
	if f, ok := r.blobs.(*fileBlobs); ok {
		if _, err := os.Stat(f.uploadPath(KindUploads, id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the upload's file stayed: %v", err)
		}
	}
}

func TestAnUploadInPartsGoesOnAfterABreakAndBecomesABlob(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv, _ := uploadServer(t, r, nil)
		base := srv.URL + uploadsAPI
		ps := testPartSize(r)
		data := randBytes(2*ps + 12345)

		code, out := callAs(t, "", "GET", base+"/uploads", nil)
		var lim UploadLimits
		if code != 200 || json.Unmarshal(out, &lim) != nil || lim.PartSize != ps || lim.MaxSize != 0 {
			t.Fatalf("limits (no limit by default): %d %s", code, out)
		}
		st := openUpload(t, base, data)
		if !ValidUploadID(st.ID) || st.PartSize != ps || len(st.Parts) != 0 || st.State != UploadReceiving {
			t.Fatalf("opened %+v", st)
		}
		if c, out := sendPart(t, "", base, st.ID, 0, partOf(data, ps, 0)); c != 204 {
			t.Fatalf("part 0: %d %s", c, out)
		}
		// Parts go in order; one sent again is taken as it is.
		if c, out := sendPart(t, "", base, st.ID, 2, partOf(data, ps, 2)); c != 409 || !strings.Contains(string(out), "part 1 is next") {
			t.Fatalf("a part out of order: %d %s", c, out)
		}
		if c, _ := sendPart(t, "", base, st.ID, 0, partOf(data, ps, 0)); c != 204 {
			t.Fatalf("part 0 again: %d", c)
		}
		// Finishing with parts missing names them.
		if c, out := callAs(t, "", "POST", base+"/uploads/"+st.ID+"/complete", nil); c != 409 || !strings.Contains(string(out), "[1 2]") {
			t.Fatalf("complete with parts missing: %d %s", c, out)
		}
		// The connection broke: opening again gives the same upload and the part it has.
		again := openUpload(t, base, data)
		if again.ID != st.ID || len(again.Parts) != 1 || again.Parts[0].N != 0 || again.Parts[0].Sha != sum(partOf(data, ps, 0)) {
			t.Fatalf("reopened %+v, want %s with part 0", again, st.ID)
		}
		for n := 1; n < 3; n++ {
			if c, out := sendPart(t, "", base, st.ID, n, partOf(data, ps, n)); c != 204 {
				t.Fatalf("part %d: %d %s", n, c, out)
			}
		}
		if c, out := callAs(t, "", "POST", base+"/uploads/"+st.ID+"/complete", nil); c != 204 {
			t.Fatalf("complete: %d %s", c, out)
		}
		code, got := callAs(t, "", "GET", base+"/blobs/"+sum(data), nil)
		if code != 200 || !bytes.Equal(got, data) {
			t.Fatalf("the blob: %d, %d bytes", code, len(got))
		}
		// The upload says it is done and takes nothing more; the same contents again are done at once.
		if st := uploadStateOf(t, base, st.ID); st.State != UploadDone {
			t.Fatalf("a finished upload: %+v", st)
		}
		if c, _ := callAs(t, "", "POST", base+"/uploads/"+st.ID+"/complete", nil); c != 204 {
			t.Fatalf("a finished upload completed again: %d", c)
		}
		if c, _ := sendPart(t, "", base, st.ID, 0, partOf(data, ps, 0)); c != 409 {
			t.Fatalf("a part of a finished upload: %d", c)
		}
		nothingWritten(t, r, st.ID)
		if st := openUpload(t, base, data); !st.Done {
			t.Fatalf("contents the room holds: %+v", st)
		}
	})
}

func TestAnUploadRefusesWhatItWasNotAnnounced(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv, api := uploadServer(t, r, nil)
		base := srv.URL + uploadsAPI
		ps := testPartSize(r)
		data := randBytes(ps + 10)

		api.SetMaxUpload(ps)
		if c, _, _ := openUploadAs(t, "", base, data); c != http.StatusRequestEntityTooLarge {
			t.Fatalf("above the limit an admin set: %d", c)
		}
		api.SetMaxUpload(0)
		st := openUpload(t, base, data)

		// A part that does not hash to its sha; then parts of other contents go in, one by one.
		if c, _ := callAs(t, "", "PUT", fmt.Sprintf("%s/uploads/%s/parts/0?sha=%s", base, st.ID, sum(data[:10])), partOf(data, ps, 0)); c != 400 {
			t.Fatalf("a part with another hash: %d", c)
		}
		other := randBytes(int64(len(data)))
		if c, out := sendPart(t, "", base, st.ID, 0, partOf(other, ps, 0)); c != 204 {
			t.Fatalf("part 0: %d %s", c, out)
		}
		if c, _ := sendPart(t, "", base, st.ID, 0, partOf(data, ps, 0)); c != 409 {
			t.Fatalf("a part in with other contents, sent again: %d", c)
		}
		// One of the wrong size, one past the end.
		if c, _ := sendPart(t, "", base, st.ID, 1, data[:11]); c != 400 {
			t.Fatalf("a last part of the wrong size: %d", c)
		}
		if c, _ := sendPart(t, "", base, st.ID, 2, data[:10]); c != 400 {
			t.Fatalf("a part past the end: %d", c)
		}
		// Another member of the room neither adds to it, nor sees it, nor finishes it.
		if c, _ := sendPart(t, "vm-2", base, st.ID, 1, partOf(data, ps, 1)); c != 404 {
			t.Fatalf("another member's part: %d", c)
		}
		if c, _ := callAs(t, "vm-2", "GET", base+"/uploads/"+st.ID, nil); c != 404 {
			t.Fatalf("another member's state: %d", c)
		}
		if c, _ := callAs(t, "vm-2", "POST", base+"/uploads/"+st.ID+"/complete", nil); c != 404 {
			t.Fatalf("another member's complete: %d", c)
		}
		// Parts that do not make the announced contents drop the upload.
		if c, out := sendPart(t, "", base, st.ID, 1, partOf(other, ps, 1)); c != 204 {
			t.Fatalf("part 1: %d %s", c, out)
		}
		if c, _ := callAs(t, "", "POST", base+"/uploads/"+st.ID+"/complete", nil); c != 400 {
			t.Fatalf("parts of other contents: %d", c)
		}
		if c, _ := callAs(t, "", "HEAD", base+"/blobs/"+sum(data), nil); c != 404 {
			t.Fatalf("the announced contents were kept: %d", c)
		}
		if c, _ := callAs(t, "", "HEAD", base+"/blobs/"+sum(other), nil); c != 404 {
			t.Fatalf("the other contents were kept: %d", c)
		}
		nothingWritten(t, r, st.ID)
		if again := openUpload(t, base, data); again.ID == st.ID || len(again.Parts) != 0 {
			t.Fatalf("the bad upload stayed: %+v", again)
		}
	})
}

func TestAnUploadNobodyFinishesIsRemovedAfterItsTerm(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv, api := uploadServer(t, r, nil)
		base := srv.URL + uploadsAPI
		ps := testPartSize(r)
		now := time.Now()
		api.now = func() time.Time { return now }
		data := randBytes(ps + 10)
		st := openUpload(t, base, data)
		if c, _ := sendPart(t, "", base, st.ID, 0, partOf(data, ps, 0)); c != 204 {
			t.Fatalf("part: %d", c)
		}
		if n := api.SweepUploads(bg); n != 0 {
			t.Fatalf("swept %d uploads in their term", n)
		}
		now = now.Add(UploadTTL + 2*time.Hour)
		if n := api.SweepUploads(bg); n != 1 {
			t.Fatalf("swept %d expired uploads, want 1", n)
		}
		nothingWritten(t, r, st.ID)
		if c, _ := sendPart(t, "", base, st.ID, 1, partOf(data, ps, 1)); c != 404 {
			t.Fatalf("a part of an expired upload: %d", c)
		}
	})
}

// slowBlobs finishes an upload only when let.
type slowBlobs struct {
	BlobStore
	let chan struct{}
}

func (s slowBlobs) UploadFinish(ctx context.Context, room string, u *Upload) error {
	<-s.let
	return s.BlobStore.UploadFinish(ctx, room, u)
}

// A finish that takes longer than a moment answers 202; the upload says it is finishing, then done.
func TestAFinishThatTakesAWhileIsAskedAbout(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		slow := slowBlobs{BlobStore: r.blobs, let: make(chan struct{})}
		srv, api := uploadServer(t, r, slow)
		api.finishWait = 10 * time.Millisecond
		base := srv.URL + uploadsAPI
		ps := testPartSize(r)
		data := randBytes(ps + 99)
		st := openUpload(t, base, data)
		for n := 0; n < 2; n++ {
			if c, out := sendPart(t, "", base, st.ID, n, partOf(data, ps, n)); c != 204 {
				t.Fatalf("part %d: %d %s", n, c, out)
			}
		}
		code, out := callAs(t, "", "POST", base+"/uploads/"+st.ID+"/complete", nil)
		var got UploadState
		if code != 202 || json.Unmarshal(out, &got) != nil || got.State != UploadFinishing {
			t.Fatalf("a slow complete: %d %s", code, out)
		}
		if st := uploadStateOf(t, base, st.ID); st.State != UploadFinishing {
			t.Fatalf("while finishing: %+v", st)
		}
		if c, _ := callAs(t, "", "POST", base+"/uploads/"+st.ID+"/complete", nil); c != 202 {
			t.Fatalf("complete again while finishing: %d", c)
		}
		if c, _ := callAs(t, "", "DELETE", base+"/uploads/"+st.ID, nil); c != 409 {
			t.Fatalf("cancel while finishing: %d", c)
		}
		if again := openUpload(t, base, data); again.ID != st.ID || again.State != UploadFinishing {
			t.Fatalf("opened again while finishing: %+v", again)
		}
		close(slow.let)
		deadline := time.Now().Add(10 * time.Second)
		for uploadStateOf(t, base, st.ID).State != UploadDone {
			if time.Now().After(deadline) {
				t.Fatal("the upload was not done")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if c, got := callAs(t, "", "GET", base+"/blobs/"+sum(data), nil); c != 200 || !bytes.Equal(got, data) {
			t.Fatalf("the blob: %d, %d bytes", c, len(got))
		}
	})
}

// lowDisk is a folder store whose disk has free bytes left.
type lowDisk struct {
	BlobStore
	free int64
}

func (l *lowDisk) FreeSpace() (int64, bool) { return l.free, true }

// Without a size limit, a folder store still refuses what its disk cannot take, saying so.
func TestAnUploadIsRefusedWhenTheDiskIsShort(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		disk := &lowDisk{BlobStore: r.blobs, free: SpareSpace + 100}
		srv, _ := uploadServer(t, r, disk)
		base := srv.URL + uploadsAPI
		ps := testPartSize(r)
		data := randBytes(ps + 200)
		code, _, out := openUploadAs(t, "", base, data)
		if code != http.StatusInsufficientStorage || !strings.Contains(string(out), "not enough free space") {
			t.Fatalf("an upload bigger than the disk: %d %s", code, out)
		}
		disk.free = 1 << 40
		st := openUpload(t, base, data)
		disk.free = SpareSpace + 100
		if c, out := sendPart(t, "", base, st.ID, 0, partOf(data, ps, 0)); c != http.StatusInsufficientStorage {
			t.Fatalf("a part the disk cannot take: %d %s", c, out)
		}
		disk.free = 1 << 40
		if c, out := sendPart(t, "", base, st.ID, 0, partOf(data, ps, 0)); c != 204 {
			t.Fatalf("the part once there is room: %d %s", c, out)
		}
	})
}

// One member has a few uploads under way at once; another member still has room for theirs.
func TestOneMemberHasAFewUploadsUnderWay(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		srv, _ := uploadServer(t, r, nil)
		base := srv.URL + uploadsAPI
		for i := 0; i < MaxOpenUploadsPerMember; i++ {
			openUpload(t, base, randBytes(100))
		}
		code, _, out := openUploadAs(t, "", base, randBytes(100))
		if code != http.StatusTooManyRequests || !strings.Contains(string(out), "of yours") {
			t.Fatalf("one upload too many: %d %s", code, out)
		}
		if code, _, out := openUploadAs(t, "vm-2", base, randBytes(100)); code != 200 {
			t.Fatalf("another member: %d %s", code, out)
		}
	})
}

func TestPartSizeKeepsAnyFileWithinMaxParts(t *testing.T) {
	s := &Server{}
	if got := s.partSizeFor(1 << 30); got != PartSize {
		t.Fatalf("1 GiB in parts of %d", got)
	}
	for _, size := range []int64{PartSize*MaxParts + 1, 200 << 30, 5 << 40} {
		ps := s.partSizeFor(size)
		if ps%(1<<20) != 0 || (size+ps-1)/ps > MaxParts || ps < PartSize {
			t.Fatalf("%d bytes in parts of %d", size, ps)
		}
	}
}
