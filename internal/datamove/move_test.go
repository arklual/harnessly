package datamove

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/delivery"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fakeStore struct {
	mu    sync.Mutex
	blobs map[string][]byte
	puts  int
	fail  error // the next put fails with it
	lose  bool  // puts are answered and not kept
}

func (s *fakeStore) HasBlob(_ context.Context, kind, sha string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.blobs[kind+"/"+sha]
	return ok, nil
}

func (s *fakeStore) PutBlob(_ context.Context, kind, sha string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail; err != nil {
		s.fail = nil
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != sha {
		return errors.New("the bytes do not hash to the name")
	}
	s.puts++
	if !s.lose {
		s.blobs[kind+"/"+sha] = append([]byte(nil), data...)
	}
	return nil
}

type fakeJournal struct {
	mu      sync.Mutex
	reports []hubproto.MigrationReport
	answer  hubproto.MigrationAnswer
	err     error
}

func (j *fakeJournal) Report(_ context.Context, rep hubproto.MigrationReport) (hubproto.MigrationAnswer, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err != nil {
		return hubproto.MigrationAnswer{}, j.err
	}
	j.reports = append(j.reports, rep)
	ans := j.answer
	j.answer = hubproto.MigrationAnswer{}
	return ans, nil
}

func (j *fakeJournal) last(kind, object string) *hubproto.MigrationObject {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.reports) - 1; i >= 0; i-- {
		for _, o := range j.reports[i].Objects {
			if o.Kind == kind && o.Object == object {
				return &o
			}
		}
	}
	return nil
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shaOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// oldInstall is the data of a runtime of an older version: two pictures of chats, a handoff note, and a transcript
// that records a file the agent handed over.
func oldInstall(t *testing.T) (data string, transcripts func(context.Context) []string) {
	t.Helper()
	data = t.TempDir()
	up := filepath.Join(data, "v2", "uploads")
	for id, body := range map[string]string{"up_1": "png-1", "up_2": "png-2"} {
		write(t, filepath.Join(up, id), body)
		raw, _ := json.Marshal(map[string]string{"id": id, "name": id + ".png", "kind": "image", "mime": "image/png"})
		write(t, filepath.Join(up, id+".json"), string(raw))
	}
	write(t, filepath.Join(data, "v2", "handoffs", "drafts", "o1", "tab-1", "h1.jsonl"), `{"note":"hand over"}`+"\n")
	report := filepath.Join(data, "work", "report.pdf")
	write(t, report, "pdf-bytes")
	link := delivery.Link(delivery.Info{Path: report, Name: "report.pdf", Mime: "application/pdf", Size: 9, SHA256: shaOf("pdf-bytes")})
	// Claude's transcript writes the record inside a JSON string; a Go writer would escape its ampersands.
	line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "Delivered: " + link}}}})
	escaped := strings.ReplaceAll(string(line), "&", `\u0026`)
	// A record a person typed is no handed-over file: it is not read.
	secret := filepath.Join(data, "secret.key")
	write(t, secret, "do not move")
	typed, _ := json.Marshal(map[string]any{"type": "user", "content": delivery.Link(delivery.Info{Path: secret, Name: "k",
		Size: 11, SHA256: shaOf("do not move")})})
	tr := filepath.Join(data, "claude", "chat.jsonl")
	write(t, tr, `{"type":"assistant","content":"hello"}`+"\n"+string(typed)+"\n"+escaped+"\n")
	return data, func(context.Context) []string { return []string{tr} }
}

func newMover(data string, transcripts func(context.Context) []string, st *fakeStore, j Journal) *Mover {
	return New(Options{Version: "0.17.0", StateFile: filepath.Join(data, "migrations", ID+".json"), Target: st, Journal: j,
		Sources: []Source{Uploads{Dir: filepath.Join(data, "v2", "uploads")}, Handoffs{Dir: filepath.Join(data, "v2", "handoffs")},
			&Delivered{Transcripts: transcripts, Pause: 1}},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func TestAnOldInstallMovesEverythingAndGoesOnWhereItWas(t *testing.T) {
	data, transcripts := oldInstall(t)
	st := &fakeStore{blobs: map[string][]byte{}}
	j := &fakeJournal{}
	m := newMover(data, transcripts, st, j)
	m.pass(context.Background())
	s := m.Status()
	if s.Total != 4 || s.Verified != 4 || !s.MigrationDone() || s.FinishedAt == "" || s.Hub != "on" {
		t.Fatalf("status = %+v, want all four objects in the hub", s)
	}
	for _, k := range []string{"uploads/" + shaOf("png-1"), "uploads/" + shaOf("png-2"), "files/" + shaOf("pdf-bytes"), "handoff/" + shaOf(`{"note":"hand over"}`+"\n")} {
		if _, ok := st.blobs[k]; !ok {
			t.Errorf("the store has no %s", k)
		}
	}
	if o := j.last("uploads", "up_1"); o == nil || o.State != hubproto.MigrationVerified || o.Mime != "image/png" || o.Sha256 != shaOf("png-1") {
		t.Fatalf("the hub was told %+v of up_1", o)
	}
	if got := j.reports[len(j.reports)-1].Total; got != 4 {
		t.Fatalf("the report's total = %d", got)
	}

	// A new runtime (the next version) reads the journal from the disk and moves nothing again.
	puts := st.puts
	again := newMover(data, transcripts, st, j)
	if s := again.Status(); s.Verified != 4 {
		t.Fatalf("the journal on the disk = %+v", s)
	}
	again.pass(context.Background())
	if st.puts != puts {
		t.Fatalf("%d objects put again", st.puts-puts)
	}
	// Nothing on the disk was removed.
	if _, err := os.Stat(filepath.Join(data, "v2", "uploads", "up_1")); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnreadableObjectWaitsForAPersonThenMoves(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	data, transcripts := oldInstall(t)
	broken := filepath.Join(data, "v2", "uploads", "up_2")
	if err := os.Chmod(broken, 0); err != nil {
		t.Fatal(err)
	}
	st := &fakeStore{blobs: map[string][]byte{}}
	m := newMover(data, transcripts, st, &fakeJournal{})
	wait := m.pass(context.Background())
	s := m.Status()
	if s.Failed != 1 || s.Verified != 3 || s.Failures[0].Code != CodeRead || s.Failures[0].Object != "up_2" {
		t.Fatalf("status = %+v, want up_2 failed with MIG-READ", s)
	}
	if wait < m.o.Every {
		t.Fatalf("a failure that waits for a person is tried again in %v", wait)
	}
	m.pass(context.Background())
	if s := m.Status(); s.Failures[0].Attempts != 1 {
		t.Fatalf("attempts = %d: MIG-READ is not tried again by itself", s.Failures[0].Attempts)
	}
	if err := os.Chmod(broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if n := m.Retry(nil); n != 1 {
		t.Fatalf("retry marked %d", n)
	}
	m.pass(context.Background())
	if s := m.Status(); !s.MigrationDone() {
		t.Fatalf("status = %+v after the retry", s)
	}
}

func TestAFailedUploadIsTriedAgainByItself(t *testing.T) {
	data, transcripts := oldInstall(t)
	st := &fakeStore{blobs: map[string][]byte{}, fail: errors.New("503 from the hub")}
	m := newMover(data, transcripts, st, &fakeJournal{})
	wait := m.pass(context.Background())
	if s := m.Status(); s.Failed != 1 || s.Failures[0].Code != CodePut {
		t.Fatalf("status = %+v, want one MIG-PUT", s)
	}
	if wait > 2*60e9 {
		t.Fatalf("a transient failure waits %v", wait)
	}
	m.pass(context.Background())
	if s := m.Status(); !s.MigrationDone() {
		t.Fatalf("status = %+v", s)
	}
}

func TestAStoreThatLosesWritesIsAVerifyFailure(t *testing.T) {
	data, transcripts := oldInstall(t)
	st := &fakeStore{blobs: map[string][]byte{}, lose: true}
	m := newMover(data, transcripts, st, nil)
	m.pass(context.Background())
	s := m.Status()
	if s.Failed != 4 || s.Failures[0].Code != CodeVerify || s.Hub != "no hub" {
		t.Fatalf("status = %+v, want every object MIG-VERIFY", s)
	}
}

func TestAHandedOverFileThatChangedIsAHashFailure(t *testing.T) {
	data, transcripts := oldInstall(t)
	write(t, filepath.Join(data, "work", "report.pdf"), "pdf-bytes, edited since")
	m := newMover(data, transcripts, &fakeStore{blobs: map[string][]byte{}}, nil)
	m.pass(context.Background())
	s := m.Status()
	if s.Failed != 1 || s.Failures[0].Code != CodeHash || s.Failures[0].Kind != KindFiles {
		t.Fatalf("status = %+v, want the file MIG-HASH", s)
	}
	if n := m.Skip([]hubproto.MigrationKey{{Kind: KindFiles, Object: shaOf("pdf-bytes")}}); n != 1 {
		t.Fatalf("skip marked %d", n)
	}
	m.pass(context.Background())
	if s := m.Status(); !s.MigrationDone() || s.Skipped != 1 {
		t.Fatalf("status = %+v, want the move over with one left on the machine", s)
	}
}

func TestTheHubsAsksAreTakenAndAnOlderHubIsSaid(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	data, transcripts := oldInstall(t)
	broken := filepath.Join(data, "v2", "uploads", "up_1")
	if err := os.Chmod(broken, 0); err != nil {
		t.Fatal(err)
	}
	j := &fakeJournal{}
	m := newMover(data, transcripts, &fakeStore{blobs: map[string][]byte{}}, j)
	m.pass(context.Background())
	j.answer = hubproto.MigrationAnswer{Skip: []hubproto.MigrationKey{{Kind: KindUploads, Object: "up_1"}}}
	m.pass(context.Background())
	if s := m.Status(); s.Skipped != 1 || !s.MigrationDone() {
		t.Fatalf("status = %+v, want up_1 skipped by the hub's ask", s)
	}
	j.err = ErrNoJournal
	_, _ = m.Verify(context.Background()) // the journal is gone: Verify fails, the status says why
	m.pass(context.Background())
	if s := m.Status(); !strings.Contains(s.Hub, "older version") {
		t.Fatalf("hub = %q", s.Hub)
	}
}

func TestVerifyMovesAgainWhatTheStoreLost(t *testing.T) {
	data, transcripts := oldInstall(t)
	st := &fakeStore{blobs: map[string][]byte{}}
	m := newMover(data, transcripts, st, nil)
	m.pass(context.Background())
	delete(st.blobs, "uploads/"+shaOf("png-1"))
	v, err := m.Verify(context.Background())
	if err != nil || v.Checked != 4 || len(v.Missing) != 1 || v.Missing[0].Object != "up_1" {
		t.Fatalf("verify = %+v, %v", v, err)
	}
	m.pass(context.Background())
	if _, ok := st.blobs["uploads/"+shaOf("png-1")]; !ok || !m.Status().MigrationDone() {
		t.Fatalf("up_1 was not moved again: %+v", m.Status())
	}
}

func TestALinkIsNotFollowed(t *testing.T) {
	data, transcripts := oldInstall(t)
	up := filepath.Join(data, "v2", "uploads", "up_1")
	if err := os.Remove(up); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(data, "secret.key"), up); err != nil {
		t.Fatal(err)
	}
	st := &fakeStore{blobs: map[string][]byte{}}
	m := newMover(data, transcripts, st, nil)
	m.pass(context.Background())
	s := m.Status()
	if s.Failed != 1 || s.Failures[0].Object != "up_1" || s.Failures[0].Code != CodeRead {
		t.Fatalf("status = %+v, want up_1 refused", s)
	}
	if _, ok := st.blobs["uploads/"+shaOf("do not move")]; ok {
		t.Fatal("the link's target was moved")
	}
}

// the machine's sidebar goes to the hub once, and again only when it changes; a hub that keeps none leaves it
// skipped and asked again each pass; Verify does not ask the store for it.
func TestTheSidebarIsDeliveredOnceAndAgainWhenItChanges(t *testing.T) {
	data := t.TempDir()
	side := &hubproto.MigrationSidebar{Projects: []hubproto.MigrationProject{{ID: "p1", Name: "Работа", CreatedAt: 1}}}
	sent, kept := 0, false
	src := Sidebar{
		Read: func(context.Context) (*hubproto.MigrationSidebar, error) { return side, nil },
		Send: func(_ context.Context, got hubproto.MigrationSidebar) (bool, error) {
			sent++
			if !kept {
				return false, ErrNotKept
			}
			return len(got.Projects) > 0, nil
		},
	}
	st, j := &fakeStore{blobs: map[string][]byte{}}, &fakeJournal{}
	m := New(Options{Version: "0.17.0", StateFile: filepath.Join(data, "m.json"), Target: st, Journal: j, Sources: []Source{src},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	m.pass(context.Background())
	if o := j.last(KindSidebar, hubproto.MigrationSidebarObject); sent != 1 || o == nil || o.State != hubproto.MigrationSkipped ||
		o.Code != CodeNotKept || !m.Status().MigrationDone() {
		t.Fatalf("a hub without a sidebar: sent %d, told %+v", sent, o)
	}
	kept = true
	m.pass(context.Background())
	if o := j.last(KindSidebar, hubproto.MigrationSidebarObject); sent != 2 || o.State != hubproto.MigrationVerified || o.Sha256 == "" {
		t.Fatalf("delivered: sent %d, told %+v", sent, o)
	}
	m.pass(context.Background())
	if sent != 2 {
		t.Fatalf("an unchanged sidebar sent again: %d", sent)
	}
	side.Projects = append(side.Projects, hubproto.MigrationProject{ID: "p2", Name: "Новый", CreatedAt: 2})
	m.pass(context.Background())
	if sent != 3 {
		t.Fatalf("a new project not sent: %d", sent)
	}
	if v, err := m.Verify(context.Background()); err != nil || v.Checked != 0 {
		t.Fatalf("verify asked the store of the sidebar: %+v %v", v, err)
	}
	// A machine with nothing in its sidebar has nothing to merge.
	empty := New(Options{Version: "0.17.0", StateFile: filepath.Join(data, "e.json"), Target: st, Sources: []Source{Sidebar{
		Read: func(context.Context) (*hubproto.MigrationSidebar, error) { return &hubproto.MigrationSidebar{}, nil },
		Send: src.Send}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	empty.pass(context.Background())
	if s := empty.Status(); s.Total != 0 || sent != 3 {
		t.Fatalf("an empty sidebar: %+v, sent %d", s, sent)
	}
}

// An upload of today's runtime is <id><ext> (wire.uploadExt) beside <id>.json: it moves, not "gone".
func TestAnUploadWithAnExtensionMoves(t *testing.T) {
	data, transcripts := oldInstall(t)
	up := filepath.Join(data, "v2", "uploads")
	write(t, filepath.Join(up, "up_3.png"), "png-3")
	raw, _ := json.Marshal(map[string]string{"id": "up_3", "name": "shot.png", "kind": "image", "mime": "image/png"})
	write(t, filepath.Join(up, "up_3.json"), string(raw))
	write(t, filepath.Join(up, "up_4.md"), "notes")
	raw, _ = json.Marshal(map[string]string{"id": "up_4", "name": "Транскрипт.md", "kind": "file", "mime": "text/markdown"})
	write(t, filepath.Join(up, "up_4.json"), string(raw))
	st := &fakeStore{blobs: map[string][]byte{}}
	m := newMover(data, transcripts, st, &fakeJournal{})
	m.pass(context.Background())
	if s := m.Status(); s.Total != 6 || s.Verified != 6 || s.Failed != 0 {
		t.Fatalf("status = %+v, want all six verified", s)
	}
	for _, body := range []string{"png-3", "notes"} {
		found := false
		for _, b := range st.blobs {
			found = found || string(b) == body
		}
		if !found {
			t.Fatalf("the store has no %q", body)
		}
	}
}

// batchStore is a store that also answers for many blobs at once, and counts both kinds of look.
type batchStore struct {
	fakeStore
	looks, batches int
}

func (s *batchStore) HasBlob(ctx context.Context, kind, sha string) (bool, error) {
	s.mu.Lock()
	s.looks++
	s.mu.Unlock()
	return s.fakeStore.HasBlob(ctx, kind, sha)
}

func (s *batchStore) MissingBlobs(_ context.Context, kind string, shas []string) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches++
	missing := map[string]bool{}
	for _, sha := range shas {
		if _, ok := s.blobs[kind+"/"+sha]; !ok {
			missing[sha] = true
		}
	}
	return missing, nil
}

func TestVerifyAsksTheStoreOncePerKind(t *testing.T) {
	data, transcripts := oldInstall(t)
	st := &batchStore{fakeStore: fakeStore{blobs: map[string][]byte{}}}
	m := newMover(data, transcripts, &st.fakeStore, nil)
	m.o.Target = st
	m.pass(context.Background())
	if !m.Status().MigrationDone() {
		t.Fatalf("the move: %+v", m.Status())
	}
	delete(st.blobs, "uploads/"+shaOf("png-1"))
	st.looks, st.batches = 0, 0
	v, err := m.Verify(context.Background())
	if err != nil || v.Checked != 4 || len(v.Missing) != 1 || v.Missing[0].Object != "up_1" {
		t.Fatalf("verify = %+v, %v", v, err)
	}
	// The two uploads are one request; a kind with one object is asked about as before.
	if st.batches != 1 || st.looks != 2 {
		t.Fatalf("%d batch looks and %d single ones; want 1 and 2", st.batches, st.looks)
	}
	m.pass(context.Background())
	if _, ok := st.blobs["uploads/"+shaOf("png-1")]; !ok || !m.Status().MigrationDone() {
		t.Fatalf("up_1 was not moved again: %+v", m.Status())
	}
}

// An object no person can move does not stop the move: a file that left the disk and one bigger than the hub takes
// are skipped with their reason, and the big one is not sent again until its size changes.
func TestAGoneOrTooBigObjectIsSkippedWithItsReason(t *testing.T) {
	data, transcripts := oldInstall(t)
	if err := os.Remove(filepath.Join(data, "work", "report.pdf")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(data, "v2", "uploads", "up_2"), "png-2, grown bigger than the hub takes")
	st := &fakeStore{blobs: map[string][]byte{}}
	j := &fakeJournal{}
	m := newMover(data, transcripts, st, j)
	m.o.MaxBytes = 25
	m.pass(context.Background())
	s := m.Status()
	if s.Failed != 0 || s.Skipped != 2 || s.Verified != 2 || !s.MigrationDone() {
		t.Fatalf("status = %+v, want two skipped and the move done", s)
	}
	if o := j.last("files", shaOf("pdf-bytes")); o == nil || o.State != hubproto.MigrationSkipped || o.Code != CodeGone {
		t.Fatalf("the hub was told %+v of the gone file", o)
	}
	if o := j.last("uploads", "up_2"); o == nil || o.State != hubproto.MigrationSkipped || o.Code != CodeTooBig {
		t.Fatalf("the hub was told %+v of the big upload", o)
	}

	// The cause gone, both move.
	write(t, filepath.Join(data, "work", "report.pdf"), "pdf-bytes")
	write(t, filepath.Join(data, "v2", "uploads", "up_2"), "png-2")
	m.pass(context.Background())
	if s := m.Status(); s.Verified != 4 || s.Skipped != 0 {
		t.Fatalf("status = %+v after the causes went", s)
	}
}

// A store that refuses an object for its size is not sent it again pass after pass.
func TestAnObjectTheStoreRefusesForItsSizeIsNotSentAgain(t *testing.T) {
	data, transcripts := oldInstall(t)
	st := &fakeStore{blobs: map[string][]byte{}, fail: fmt.Errorf("%w: 400 file bigger than 52428800 bytes", ErrTooBig)}
	m := newMover(data, transcripts, st, &fakeJournal{})
	m.pass(context.Background())
	s := m.Status()
	if s.Failed != 0 || s.Skipped != 1 || !s.MigrationDone() {
		t.Fatalf("status = %+v, want one skipped", s)
	}
	puts := st.puts
	m.pass(context.Background())
	if st.puts != puts || m.Status().Skipped != 1 {
		t.Fatalf("the refused object was sent again: %d puts", st.puts-puts)
	}
}

// A journal from a runtime that failed such objects skips them on its next pass.
func TestAnOlderJournalsGoneFailureIsSkipped(t *testing.T) {
	data, transcripts := oldInstall(t)
	st := &fakeStore{blobs: map[string][]byte{}}
	m := newMover(data, transcripts, st, &fakeJournal{})
	m.pass(context.Background())
	if err := os.Remove(filepath.Join(data, "v2", "uploads", "up_1")); err != nil {
		t.Fatal(err)
	}
	k := key(KindUploads, "up_1")
	m.mu.Lock()
	r := m.st.Records[k]
	r.State, r.Code, r.Error, r.Attempts = hubproto.MigrationFailed, CodeGone, "gone", 12
	m.mu.Unlock()
	m.pass(context.Background())
	if s := m.Status(); s.Failed != 0 || s.Skipped != 1 || !s.MigrationDone() {
		t.Fatalf("status = %+v, want the old failure skipped", s)
	}
}
