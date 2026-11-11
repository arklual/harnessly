package chatmove

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// memRoom is the person's resume room in memory.
type memRoom struct {
	mu    sync.Mutex
	files map[string][]byte
	puts  int
}

func (m *memRoom) Current(_ context.Context, path string) (*filestore.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[path]
	if !ok {
		return nil, nil
	}
	h := sha256.Sum256(b)
	return &filestore.Entry{Path: path, Sha: hex.EncodeToString(h[:])}, nil
}

func (m *memRoom) GetBlob(_ context.Context, sha string, w io.Writer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.files {
		if h := sha256.Sum256(b); hex.EncodeToString(h[:]) == sha {
			_, err := w.Write(b)
			return err
		}
	}
	return errors.New("no blob")
}

func (m *memRoom) Put(_ context.Context, path string, data []byte, _ string) (filestore.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[path] = append([]byte(nil), data...)
	m.puts++
	return filestore.Entry{Path: path}, nil
}

// memSafe is the person's folder of the hub's safe.
type memSafe struct {
	mu     sync.Mutex
	data   map[string]map[string]string
	writes int
}

func (s *memSafe) Read(_ context.Context, path string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.data[path]
	if !ok {
		return nil, &secrets.Refusal{Reason: secrets.ReasonNotFound}
	}
	return d, nil
}

func (s *memSafe) Write(_ context.Context, path string, d map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]map[string]string{}
	}
	s.data[path] = d
	s.writes++
	return nil
}

// a machine leaves a sealed snapshot of a chat at rest in the person's room; another machine of the person
// continues the chat from it while the first is off, once per snapshot; the room keeps no history in plain text.
func TestResumeFromASnapshotWhileTheSourceIsOff(t *testing.T) {
	ctx := context.Background()
	room, safe := &memRoom{}, &memSafe{}
	keysA := &ResumeKeys{Safe: safe, Folder: "me"}
	keysB := &ResumeKeys{Safe: safe, Folder: "me"}

	srcConv := newFake("")
	src := service(t, srcConv, secretA)
	src.Resume = &Resume{Store: room, Key: keysA.Key}
	dstConv := newFake("")
	dst := service(t, dstConv, secretB, t.TempDir())
	dst.Resume = &Resume{Store: room, Key: keysB.Key}

	up := &ResumeUploader{Service: src, Machine: "vm-a", List: func() []string { return []string{"tab-src"} },
		State: filepath.Join(t.TempDir(), "resume.json")}
	up.Tick(ctx) // the first look only takes note of where the chats are
	if room.puts != 0 {
		t.Fatalf("a chat that did not change since the switch was sent: %d", room.puts)
	}
	srcConv.fence = "101" // a turn
	up.Tick(ctx)
	if room.puts != 0 {
		t.Fatal("sent before the chat came to rest")
	}
	up.Tick(ctx)
	if room.puts != 1 {
		t.Fatalf("puts after the turn = %d", room.puts)
	}
	up.Tick(ctx)
	if room.puts != 1 {
		t.Fatalf("an unchanged chat was sent again: %d", room.puts)
	}
	// A restart remembers what the store has.
	again := &ResumeUploader{Service: src, Machine: "vm-a", List: up.List, State: up.State}
	again.Tick(ctx)
	again.Tick(ctx)
	if room.puts != 1 {
		t.Fatalf("a restart sent it again: %d", room.puts)
	}
	if safe.writes != 1 {
		t.Fatalf("the key was made %d times", safe.writes)
	}

	raw := room.files[ResumePath("vm-a", "tab-src")]
	if len(raw) == 0 || bytes.Contains(raw, []byte("plain-history-marker")) || bytes.Contains(raw, []byte("Work")) {
		t.Fatalf("the room keeps the chat readable: %s", raw)
	}

	// The source is off: the other machine reads the snapshot and opens the chat, a second time it is the same one.
	req := ResumeRequest{SourceMachine: "vm-a", ConversationID: "tab-src", Provider: "claude", Model: ModelRef{ID: "opus"}, WithoutFiles: true}
	first, ref := dst.ResumeFrom(ctx, req)
	if ref != nil || first.ConversationID == "" || first.Existing {
		t.Fatalf("resume: %+v %+v", first, ref)
	}
	second, ref := dst.ResumeFrom(ctx, req)
	if ref != nil || !second.Existing || second.ConversationID != first.ConversationID {
		t.Fatalf("resume twice: %+v %+v", second, ref)
	}
	if len(dstConv.opens) != 1 || !strings.Contains(string(dstConv.opens[0].History), "plain-history-marker") ||
		dstConv.opens[0].Title != "Work" {
		t.Fatalf("opened: %+v", dstConv.opens)
	}

	// Without the person's key the snapshot does not open; nothing of the chat is there for another key.
	other := service(t, newFake(""), secretB, t.TempDir())
	other.Resume = &Resume{Store: room, Key: (&ResumeKeys{Safe: &memSafe{}, Folder: "me"}).Key}
	if _, ref := other.ResumeFrom(ctx, req); ref == nil || ref.Code != CodeBadBundle {
		t.Fatalf("another key opened it: %+v", ref)
	}
	// A chat with no snapshot, a machine with the switch off.
	if _, ref := dst.ResumeFrom(ctx, ResumeRequest{SourceMachine: "vm-a", ConversationID: "tab-none", Provider: "claude"}); ref == nil || ref.Code != CodeNoSnapshot {
		t.Fatalf("no snapshot: %+v", ref)
	}
	dst.Resume = nil
	if _, ref := dst.ResumeFrom(ctx, req); ref == nil || ref.Code != CodeNoSnapshot {
		t.Fatalf("switch off: %+v", ref)
	}
	if _, ref := src.ResumeFrom(ctx, ResumeRequest{SourceMachine: "../x", ConversationID: "tab-src"}); ref == nil || ref.Code != CodeBadBundle {
		t.Fatalf("a path out of the room: %+v", ref)
	}
}

func TestResumeSnapshotRefusesAChatAtWork(t *testing.T) {
	ctx := context.Background()
	conv := newFake("")
	s := service(t, conv, secretA)
	key := bytes.Repeat([]byte{7}, 32)
	conv.info["tab-src"] = Info{Title: "Work", HasHistory: true, Busy: true}
	if _, _, ref := s.SealResume(ctx, "vm-a", "tab-src", key, 0); ref == nil || ref.Code != CodeBusy {
		t.Fatalf("busy: %+v", ref)
	}
	conv.info["tab-src"] = Info{Title: "Work"}
	if _, _, ref := s.SealResume(ctx, "vm-a", "tab-src", key, 0); ref == nil || ref.Code != CodeNoHistory {
		t.Fatalf("no history: %+v", ref)
	}
	conv.info["tab-src"] = Info{Title: "Work", HasHistory: true}
	snap, fence, ref := s.SealResume(ctx, "vm-a", "tab-src", key, 5)
	if ref != nil || fence != "100" || snap.V != ResumeVersion || snap.OpID != resumeOp("vm-a", "tab-src", "100") {
		t.Fatalf("snapshot: %+v %q %+v", snap, fence, ref)
	}
	b, _ := json.Marshal(snap)
	if bytes.Contains(b, []byte("plain-history-marker")) {
		t.Fatal("the snapshot is not sealed")
	}
}
