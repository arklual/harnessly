package design

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// deckLibrary is a library with one deck of three pages and an API that lets everyone in.
func deckLibrary(t *testing.T) (*Library, *API, string, []string) {
	t.Helper()
	root := t.TempDir()
	l := testLibrary(t, root)
	// A finished take builds its transcript in the background; let it end before the temp dir goes.
	t.Cleanup(func() { waitRecIdle(t) })
	id := "talk"
	writeFile(t, filepath.Join(root, id, ArtifactFile), `{"title":"Talk"}`)
	writeFile(t, filepath.Join(root, id, MetaDirname, MetaProjectFile), `{"v":1,"title":"Talk"}`)
	var pages []string
	for i, n := range []string{"One", "Two", "Three"} {
		pid := ULID(1_700_000_000_000+int64(i), [10]byte{byte(i + 1)})
		pages = append(pages, pid)
		writeFile(t, filepath.Join(root, id, MetaDirname, MetaPagesDirname, pid+".json"),
			`{"id":"`+pid+`","title":"`+n+`","file":"`+n+`.dc.html","w":1280,"h":720,"order":`+strconv.Itoa(i+1)+`}`)
		writeFile(t, filepath.Join(root, id, ProjectDirname, n+".dc.html"), "<html><body><h1>"+n+"</h1></body></html>")
	}
	return l, pagesAPI(l), id, pages
}

// waitRecIdle waits until no take is building its transcript.
func waitRecIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		recBuilding.Range(func(_, _ any) bool { busy = true; return false })
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("a transcript build did not end")
}

func postJSON(a func(*http.Request) Answer, method, body string) (int, map[string]any) {
	r := httptest.NewRequest(method, "/x", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	ans := a(r)
	raw, _ := json.Marshal(ans.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return ans.Status, out
}

const ebml = "\x1a\x45\xdf\xa3"

func TestRecChunksAreInOrderAndIdempotent(t *testing.T) {
	l, _, id, pages := deckLibrary(t)
	m, err := l.CreateRec(id, true, "video/webm")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Slides) != 3 || m.Slides[0] != pages[0] || m.State != DeckRecRecording {
		t.Fatalf("meta %+v", m)
	}
	put := func(seq int, s string) error { return l.AppendRecChunk(id, m.ID, seq, strings.NewReader(s)) }
	if err := put(0, ebml+"aaa"); err != nil {
		t.Fatal(err)
	}
	if err := put(1, "bb"); err != nil {
		t.Fatal(err)
	}
	// The same seq again changes nothing, whatever the bytes.
	if err := put(1, "XXXX"); err != nil {
		t.Fatalf("a repeat is a no-op: %v", err)
	}
	// A gap names the chunk wanted.
	if err := put(5, "c"); err == nil {
		t.Fatal("a gap was taken")
	} else if e, ok := err.(errRecNext); !ok || e.Next != 2 {
		t.Fatalf("gap error %v", err)
	}
	// A partial write the state does not know about is cut off before the next chunk.
	f, _ := os.OpenFile(filepath.Join(l.recDir(id, m.ID), recMediaFile), os.O_WRONLY|os.O_APPEND, 0o644)
	_, _ = f.WriteString("JUNK")
	_ = f.Close()
	if err := put(2, "c"); err != nil {
		t.Fatal(err)
	}
	if got := readFileT(t, filepath.Join(l.recDir(id, m.ID), recMediaFile)); got != ebml+"aaabbc" {
		t.Fatalf("av.webm = %q", got)
	}
	if _, err := l.FinishRec(id, m.ID, 1500); err != nil {
		t.Fatal(err)
	}
	if err := put(3, "d"); err != errRecFinished {
		t.Fatalf("a chunk after the finish: %v", err)
	}
	if err := put(0, ebml+"zzz"); err != nil {
		t.Fatalf("a repeat after the finish is still a no-op: %v", err)
	}
}

func TestRecRoutes(t *testing.T) {
	l, a, id, _ := deckLibrary(t)
	status, out := postJSON(a.DeckRec, http.MethodPost, `{"artifact":"`+id+`","camera":true,"mime":"video/webm;codecs=vp9,opus"}`)
	if status != 200 {
		t.Fatalf("create %d %v", status, out)
	}
	rec, _ := out["rec"].(string)
	if !IsPageID(rec) {
		t.Fatalf("rec = %v", out)
	}
	if s, _ := postJSON(a.DeckRec, http.MethodPost, `{"artifact":"`+id+`","camera":true,"mime":"text/html"}`); s != 400 {
		t.Errorf("a mime that is not media: %d", s)
	}
	chunk := func(seq int, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/x?artifact="+id+"&rec="+rec+"&seq="+strconv.Itoa(seq), strings.NewReader(body))
		r.Header.Set("Content-Type", "video/webm;codecs=vp9,opus")
		r.Header.Set("Content-Type", "video/webm;codecs=vp9,opus")
		a.DeckRecChunk(w, r)
		return w
	}
	if w := chunk(0, ebml+"0123456789"); w.Code != 204 {
		t.Fatalf("chunk 0: %d %s", w.Code, w.Body)
	}
	if w := chunk(0, ebml+"0123456789"); w.Code != 204 {
		t.Fatalf("chunk 0 again: %d", w.Code)
	}
	w := chunk(3, "x")
	var gap struct {
		Next int `json:"next"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &gap)
	if w.Code != 409 || gap.Next != 1 {
		t.Fatalf("gap: %d %s", w.Code, w.Body)
	}
	if w := chunk(-1, "x"); w.Code != 400 {
		t.Errorf("negative seq: %d", w.Code)
	}

	// Events: unknown kinds and times out of the recording are dropped.
	lines := `[{"t":0,"k":"slide","page":"` + "P" + `"},{"t":10,"k":"nonsense"},{"t":-5,"k":"step","n":1},{"t":20,"k":"step","n":2},{"t":30,"k":"asr","t0":25,"text":"привет"}]`
	if s, _ := postJSON(a.DeckRecEvents, http.MethodPost, `{"artifact":"`+id+`","rec":"`+rec+`","lines":`+lines+`}`); s != 204 {
		t.Fatalf("events: %d", s)
	}
	evs := readRecEvents(filepath.Join(l.recDir(id, rec), recEventsFile))
	if len(evs) != 3 || evs[1].K != DeckEvStep || *evs[1].N != 2 || evs[2].Text != "привет" || *evs[2].T0 != 25 {
		t.Fatalf("events kept: %+v", evs)
	}

	// Finish: the answer has the summary and, until the transcript is there, no segments.
	status, out = postJSON(a.DeckRecFinish, http.MethodPost, `{"artifact":"`+id+`","rec":"`+rec+`","durationMs":4000}`)
	sum, _ := out["summary"].(map[string]any)
	if status != 200 || sum["state"] != "done" || sum["durationMs"] != float64(4000) {
		t.Fatalf("finish %d %v", status, out)
	}
	if segs, ok := out["segments"].([]any); !ok || segs == nil {
		t.Errorf("segments must be a list: %v", out["segments"])
	}
	// The transcript is built behind the finish.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && fileSize(filepath.Join(l.recDir(id, rec), recTranscriptFile)) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	r := httptest.NewRequest(http.MethodGet, "/x?artifact="+id+"&rec="+rec, nil)
	ans := a.DeckRec(r)
	raw, _ := json.Marshal(ans.Body)
	var got DeckRecording
	_ = json.Unmarshal(raw, &got)
	if ans.Status != 200 || len(got.Segments) != 1 || got.Segments[0].Parts[0].Text != "привет" || !got.Edit.Cut || got.Edit.Cam != "br" {
		t.Fatalf("read %d %s", ans.Status, raw)
	}
	if len(l.DeckRecordings(id)) != 1 || l.DeckRecordings(id)[0].State != DeckRecDone {
		t.Errorf("recordings %+v", l.DeckRecordings(id))
	}

	// The montage.
	status, out = postJSON(a.DeckRecEdit, http.MethodPut, `{"artifact":"`+id+`","rec":"`+rec+`","edit":{"cut":false,"subs":true,"kept":["0.0","0.0"],"cursor":"off","cam":"tl"}}`)
	edit, _ := out["edit"].(map[string]any)
	if status != 200 || edit["cut"] != false || edit["cam"] != "tl" || len(edit["kept"].([]any)) != 1 {
		t.Fatalf("edit %d %v", status, out)
	}
	for _, bad := range []string{`"cam":"middle","cursor":"off","kept":[]`, `"cam":"br","cursor":"wiggle","kept":[]`, `"cam":"br","cursor":"off","kept":["x"]`} {
		if s, _ := postJSON(a.DeckRecEdit, http.MethodPut, `{"artifact":"`+id+`","rec":"`+rec+`","edit":{"cut":true,"subs":true,`+bad+`}}`); s != 400 {
			t.Errorf("%s: %d", bad, s)
		}
	}
	if s, _ := postJSON(a.DeckRecFinish, http.MethodPost, `{"artifact":"`+id+`","rec":"`+missingRecID+`","durationMs":1}`); s != 400 && s != 404 {
		t.Errorf("finish of nothing: %d", s)
	}
}

// missingRecID is a well-formed recording id that names nothing.
const missingRecID = "00000000000000000000000000"

func TestRecMediaServesRanges(t *testing.T) {
	l, a, id, _ := deckLibrary(t)
	m, _ := l.CreateRec(id, false, "audio/webm")
	body := append([]byte(ebml), bytes.Repeat([]byte("0123456789"), 10)...)
	if err := l.AppendRecChunk(id, m.ID, 0, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	get := func(rng string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/x?artifact="+id+"&rec="+m.ID, nil)
		if rng != "" {
			r.Header.Set("Range", rng)
		}
		a.DeckRecMedia(w, r)
		return w
	}
	w := get("bytes=14-23")
	if w.Code != 206 || w.Body.String() != "0123456789" || w.Header().Get("Content-Range") != "bytes 14-23/104" || w.Header().Get("Content-Type") != "audio/webm" || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		w.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" || w.Header().Get("Content-Disposition") != `inline; filename="recording.webm"` {
		t.Fatalf("range: %d %q %v", w.Code, w.Body, w.Header())
	}
	if w := get(""); w.Code != 200 || w.Body.Len() != 104 || w.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("whole: %d %d", w.Code, w.Body.Len())
	}
	w = httptest.NewRecorder()
	a.DeckRecMedia(w, httptest.NewRequest(http.MethodGet, "/x?artifact="+id+"&rec="+missingRecID, nil))
	if w.Code != 404 {
		t.Errorf("missing recording: %d", w.Code)
	}
	w = httptest.NewRecorder()
	a.DeckRecMedia(w, httptest.NewRequest(http.MethodPost, "/x?artifact="+id+"&rec="+m.ID, nil))
	if w.Code != 405 {
		t.Errorf("post: %d", w.Code)
	}
}

func TestAbandonedRecordingIsFailed(t *testing.T) {
	l, _, id, _ := deckLibrary(t)
	m, _ := l.CreateRec(id, false, "audio/webm")
	old := time.Now().Add(-recStale - time.Minute)
	for _, n := range []string{recMetaFile} {
		_ = os.Chtimes(filepath.Join(l.recDir(id, m.ID), n), old, old)
	}
	if got := l.DeckRecordings(id); len(got) != 1 || got[0].State != DeckRecFailed {
		t.Fatalf("recordings %+v", got)
	}
	got, err := l.ReadRecording(id, m.ID)
	if err != nil || got.Summary.State != DeckRecFailed {
		t.Fatalf("read %+v %v", got.Summary, err)
	}
}

func TestRecRetakeIsInTheSummary(t *testing.T) {
	l, _, id, pages := deckLibrary(t)
	m, _ := l.CreateRec(id, false, "audio/webm")
	_, err := l.AppendRecEvents(id, m.ID, []DeckEvent{
		{T: 0, K: DeckEvSlide, Page: pages[0]}, {T: 1000, K: DeckEvSlide, Page: pages[1]}, {T: 2000, K: DeckEvRetake, Page: pages[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.FinishRec(id, m.ID, 3000)
	if err != nil {
		t.Fatal(err)
	}
	// The transcript is built behind the finish; the folder must not go while it is.
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end) && fileSize(filepath.Join(l.recDir(id, m.ID), recTranscriptFile)) == 0; {
		time.Sleep(20 * time.Millisecond)
	}
	if len(got.Retaken) != 1 || got.Retaken[0] != 2 || len(got.Summary.Takes) != 3 {
		t.Fatalf("retaken %v takes %+v", got.Retaken, got.Summary.Takes)
	}
}

func TestRecMimeIsAllowlisted(t *testing.T) {
	for in, want := range map[string]string{
		"video/webm":                    "video/webm",
		"VIDEO/WEBM;codecs=vp9,opus":    "video/webm;codecs=vp9,opus",
		"video/mp4; codecs=avc1.42E01E": "video/mp4;codecs=avc1.42E01E",
		"audio/ogg;codecs=opus":         "audio/ogg;codecs=opus",
		"audio/webm":                    "audio/webm",
	} {
		if _, got, ok := cleanRecMime(in); !ok || got != want {
			t.Errorf("%q -> %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"text/html", "video/x-flv", "video/", "image/svg+xml", "video/webm;charset=utf-8", "video/webm;codecs=a b",
		"video/webm;codecs=\"x;y\"", "video/webm;codecs=<script>", strings.Repeat("a", 200), ""} {
		if _, got, ok := cleanRecMime(bad); ok {
			t.Errorf("%q was taken as %q", bad, got)
		}
	}
	// A stored mime that is not on the list is served as bytes.
	l, a, id, _ := deckLibrary(t)
	m, _ := l.CreateRec(id, false, "audio/webm")
	m.Mime = "text/html"
	if err := l.writeRecMeta(id, m); err != nil {
		t.Fatal(err)
	}
	if err := l.AppendRecChunk(id, m.ID, 0, strings.NewReader(ebml)); err != errRecFormat {
		t.Fatalf("a mime off the list takes no bytes: %v", err)
	}
	m.Mime = "audio/webm"
	_ = l.writeRecMeta(id, m)
	_ = l.AppendRecChunk(id, m.ID, 0, strings.NewReader(ebml+"x"))
	m.Mime = "text/html"
	_ = l.writeRecMeta(id, m)
	w := httptest.NewRecorder()
	a.DeckRecMedia(w, httptest.NewRequest(http.MethodGet, "/x?artifact="+id+"&rec="+m.ID, nil))
	if w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("Content-Disposition") != `inline; filename="recording.bin"` {
		t.Errorf("served as %v", w.Header())
	}
}

func TestRecChunkChecksTheContainer(t *testing.T) {
	l, _, id, _ := deckLibrary(t)
	for mime, good := range map[string]string{"video/mp4": "\x00\x00\x00\x18ftypisom", "audio/ogg": "OggS\x00", "video/webm": ebml} {
		m, err := l.CreateRec(id, false, mime)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.AppendRecChunk(id, m.ID, 0, strings.NewReader("<html><script>alert(1)</script></html>")); err != errRecFormat {
			t.Errorf("%s: html was taken: %v", mime, err)
		}
		if fileSize(filepath.Join(l.recDir(id, m.ID), recMediaFile)) != 0 {
			t.Errorf("%s: bytes were kept", mime)
		}
		if err := l.AppendRecChunk(id, m.ID, 0, strings.NewReader(good)); err != nil {
			t.Errorf("%s: %v", mime, err)
		}
		// The check is on the first chunk only; a later one is the rest of the stream.
		if err := l.AppendRecChunk(id, m.ID, 1, strings.NewReader("tail")); err != nil {
			t.Errorf("%s: %v", mime, err)
		}
	}
	// A short first chunk is not a container either.
	m, _ := l.CreateRec(id, false, "video/webm")
	if err := l.AppendRecChunk(id, m.ID, 0, strings.NewReader("\x1a\x45")); err != errRecFormat {
		t.Errorf("short chunk: %v", err)
	}
}

func TestRecChunkRefusesWhatAFormCanSend(t *testing.T) {
	l, a, id, _ := deckLibrary(t)
	m, _ := l.CreateRec(id, false, "video/webm")
	send := func(ct, site string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/x?artifact="+id+"&rec="+m.ID+"&seq=0", strings.NewReader(ebml+"x"))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		a.DeckRecChunk(w, r)
		return w.Code
	}
	for _, ct := range []string{"", "text/plain", "multipart/form-data; boundary=x", "application/x-www-form-urlencoded", "text/html"} {
		if code := send(ct, ""); code != 415 {
			t.Errorf("content type %q: %d", ct, code)
		}
	}
	if code := send("video/webm", "cross-site"); code != 403 {
		t.Errorf("cross-site: %d", code)
	}
	if fileSize(filepath.Join(l.recDir(id, m.ID), recMediaFile)) != 0 {
		t.Error("a refused chunk was kept")
	}
	for _, ct := range []string{"application/octet-stream", "video/webm;codecs=vp9", "audio/webm"} {
		if code := send(ct, "same-origin"); code != 204 {
			t.Errorf("content type %q: %d", ct, code)
		}
	}
}
