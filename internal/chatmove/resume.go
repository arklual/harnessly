package chatmove

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// Resume snapshots: after each turn a machine leaves the chat's bundle in its person's resume room of the
// hub's store (filestore.KindResume), so the chat goes on on another machine of the person while this one is off. The
// bundle is the one a move carries (history, title, where in the repository), gzipped and sealed on the machine with
// the person's resume key, which only the person's machines read from the hub's safe; the store keeps ciphertext.

const (
	// ResumeVersion is the version of a snapshot file's shape.
	ResumeVersion = 1
	// ResumeKeySecret is the name the person's resume key is kept under in their folder of the hub's safe.
	ResumeKeySecret = "chat-resume-key"
	// maxSnapshot bounds a snapshot file read back from the store.
	maxSnapshot = filestore.MaxFileSize
	// maxPlain bounds a gunzipped bundle: the history, escaped in JSON, and the rest.
	maxPlain = 2*MaxHistory + 1<<16
)

// ResumeSnapshot is the file a machine keeps per chat in the resume room, at ResumePath. Only the bundle says anything
// about the chat, and it is sealed for Import under OpID with the person's key.
type ResumeSnapshot struct {
	V         int      `json:"v"`
	OpID      string   `json:"opId"`
	At        int64    `json:"at"`
	BundleEnc Envelope `json:"bundleEnc"`
}

// ResumePath is where a machine keeps a chat's snapshot in the room: under its own id, the only place the hub lets it
// write.
func ResumePath(machine, conv string) string { return machine + "/" + conv + ".json" }

// validResumePath: both parts are ids, so the path is one clean file under the machine's folder.
func validResumePath(machine, conv string) bool {
	return idPattern.MatchString(machine) && idPattern.MatchString(conv) && filestore.SafeRel(ResumePath(machine, conv))
}

// resumeOp is the operation a snapshot is sealed under: the same chat at the same fence is the same operation, so
// continuing from one snapshot twice opens the chat once (Import keeps the operation's result).
func resumeOp(machine, conv, fence string) string {
	h := sha256.Sum256([]byte("harnessly/chat-resume/v1\x00" + machine + "\x00" + conv + "\x00" + fence))
	return "r" + hex.EncodeToString(h[:16])
}

// ResumeStore is the person's resume room (filesync.Client of filestore.KindResume).
type ResumeStore interface {
	Current(ctx context.Context, path string) (*filestore.Entry, error)
	GetBlob(ctx context.Context, sha string, w io.Writer) error
	Put(ctx context.Context, path string, data []byte, author string) (filestore.Entry, error)
}

// Resume is what a machine needs to keep and read snapshots: the person's room and key.
type Resume struct {
	Store ResumeStore
	Key   func(ctx context.Context) ([]byte, error)
}

// SealResume builds the snapshot of a chat of this machine at rest and the fence it was read at. A chat that is working, has no
// history, moved away or holds more than MaxHistory is refused, as export refuses it. The project is named whether the
// tree is clean or not: the chat goes on at the last commit, the uncommitted changes stay on the machine.
func (s *Service) SealResume(ctx context.Context, machine, conv string, key []byte, at int64) (ResumeSnapshot, string, *Refusal) {
	if !validResumePath(machine, conv) {
		return ResumeSnapshot{}, "", refuse(http.StatusBadRequest, CodeBadBundle, "Неверный чат")
	}
	info, ok := s.Conv.Info(conv)
	switch {
	case !ok:
		return ResumeSnapshot{}, "", refuse(http.StatusNotFound, CodeNotFound, "Чат не найден")
	case s.Store.Blocked(conv):
		return ResumeSnapshot{}, "", refuse(http.StatusConflict, CodeAlreadyMoved, "Чат уже продолжен на другой ВМ")
	case info.Busy:
		return ResumeSnapshot{}, "", refuse(http.StatusConflict, CodeBusy, "Агент ещё работает")
	case !info.HasHistory:
		return ResumeSnapshot{}, "", refuse(http.StatusUnprocessableEntity, CodeNoHistory, "В чате пока нет истории")
	}
	var project *Project
	if r := Inspect(ctx, info.Cwd); r != nil {
		p := r.Project
		project = &p
	}
	history, fence, err := s.Conv.Snapshot(conv, MaxHistory)
	switch {
	case errors.Is(err, ErrNoHistory):
		return ResumeSnapshot{}, "", refuse(http.StatusUnprocessableEntity, CodeNoHistory, "В чате пока нет истории")
	case errors.Is(err, ErrTooBig):
		return ResumeSnapshot{}, "", refuse(http.StatusRequestEntityTooLarge, CodeTooBig, "История слишком большая для снимка")
	case err != nil:
		return ResumeSnapshot{}, "", refuse(http.StatusInternalServerError, CodeInternal, "Не удалось прочитать историю")
	}
	raw, err := json.Marshal(Bundle{V: BundleVersion, Title: info.Title, History: string(history), Project: project})
	if err != nil {
		return ResumeSnapshot{}, "", refuse(http.StatusInternalServerError, CodeInternal, "Не удалось собрать снимок")
	}
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	if _, err := zw.Write(raw); err != nil || zw.Close() != nil {
		return ResumeSnapshot{}, "", refuse(http.StatusInternalServerError, CodeInternal, "Не удалось сжать снимок")
	}
	op := resumeOp(machine, conv, fence)
	env, err := Encrypt(key, op, Import, zipped.Bytes())
	if err != nil {
		return ResumeSnapshot{}, "", refuse(http.StatusInternalServerError, CodeInternal, "Не удалось зашифровать снимок")
	}
	return ResumeSnapshot{V: ResumeVersion, OpID: op, At: at, BundleEnc: env}, fence, nil
}

// ResumeRequest continues on this machine a chat of another machine of the same person from its snapshot, whether
// that machine is on or not.
type ResumeRequest struct {
	SourceMachine  string   `json:"sourceMachine"`
	ConversationID string   `json:"conversationId"`
	Provider       string   `json:"provider"`
	Profile        string   `json:"profile"`
	Model          ModelRef `json:"model"`
	WithoutFiles   bool     `json:"withoutFiles"`
}

// ResumeFrom reads the chat's snapshot from the person's room, opens it with the person's key and imports it like a
// move. The hub refuses the room to a machine of another person (403), so a snapshot that is not the person's never
// reaches here.
func (s *Service) ResumeFrom(ctx context.Context, req ResumeRequest) (ImportResponse, *Refusal) {
	if s.Resume == nil {
		return ImportResponse{}, refuse(http.StatusNotFound, CodeNoSnapshot, "На этой машине продолжение из снимка не включено")
	}
	if !validResumePath(req.SourceMachine, req.ConversationID) {
		return ImportResponse{}, refuse(http.StatusBadRequest, CodeBadBundle, "Неверный запрос")
	}
	e, err := s.Resume.Store.Current(ctx, ResumePath(req.SourceMachine, req.ConversationID))
	if err != nil {
		return ImportResponse{}, refuse(http.StatusBadGateway, CodeNoSnapshot, "Хаб не отдал снимок чата")
	}
	if e == nil || e.Deleted || e.Sha == "" {
		return ImportResponse{}, refuse(http.StatusNotFound, CodeNoSnapshot, "Снимка этого чата в хабе нет")
	}
	var buf bytes.Buffer
	if err := s.Resume.Store.GetBlob(ctx, e.Sha, &limitWriter{w: &buf, left: maxSnapshot}); err != nil {
		return ImportResponse{}, refuse(http.StatusBadGateway, CodeNoSnapshot, "Хаб не отдал снимок чата")
	}
	var snap ResumeSnapshot
	if json.Unmarshal(buf.Bytes(), &snap) != nil || snap.V != ResumeVersion {
		return ImportResponse{}, refuse(http.StatusBadRequest, CodeBadBundle, "Снимок не прочитать")
	}
	key, err := s.Resume.Key(ctx)
	if err != nil {
		return ImportResponse{}, refuse(http.StatusBadGateway, CodeNoSnapshot, "Нет ключа снимков в сейфе хаба")
	}
	return s.Import(ctx, ImportRequest{OpID: snap.OpID, BundleEnc: snap.BundleEnc, Provider: req.Provider,
		Profile: req.Profile, Model: req.Model, WithoutFiles: req.WithoutFiles, Key: base64.StdEncoding.EncodeToString(key)})
}

// gunzipBundle opens a gzipped bundle (a resume snapshot's) and passes any other through.
func gunzipBundle(raw []byte) ([]byte, error) {
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return raw, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrBundle
	}
	plain, err := io.ReadAll(io.LimitReader(zr, maxPlain+1))
	if err != nil || len(plain) > maxPlain {
		return nil, ErrBundle
	}
	return plain, nil
}

type limitWriter struct {
	w    io.Writer
	left int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		return 0, errors.New("snapshot bigger than the store takes")
	}
	l.left -= int64(len(p))
	return l.w.Write(p)
}

// Safe is the person's folder of the hub's safe (agent.LocalSafe).
type Safe interface {
	Read(ctx context.Context, path string) (map[string]string, error)
	Write(ctx context.Context, path string, data map[string]string) error
}

// ResumeKeys finds the person's resume key in their folder of the hub's safe, making one the first time; the key is
// kept in memory once read. Two machines making it at once settle on what the safe kept: each writes, then reads back.
type ResumeKeys struct {
	Safe   Safe
	Folder string

	mu  sync.Mutex
	key []byte
}

// Key is the person's resume key.
func (k *ResumeKeys) Key(ctx context.Context) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.key != nil {
		return k.key, nil
	}
	path := k.Folder + "/" + ResumeKeySecret
	read := func() ([]byte, error) {
		data, err := k.Safe.Read(ctx, path)
		if err != nil {
			return nil, err
		}
		b, err := base64.StdEncoding.DecodeString(data[secrets.ValueKey])
		if err != nil || len(b) < MinOpKey {
			return nil, fmt.Errorf("the hub's safe keeps %s, and it is no resume key", ResumeKeySecret)
		}
		return b, nil
	}
	key, err := read()
	if err != nil && secrets.ReasonOf(err) == secrets.ReasonNotFound {
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		if err := k.Safe.Write(ctx, path, map[string]string{secrets.ValueKey: base64.StdEncoding.EncodeToString(fresh)}); err != nil {
			return nil, err
		}
		key, err = read()
	}
	if err != nil {
		return nil, err
	}
	k.key = key
	return key, nil
}

// errorText is a refusal or an error in a log line.
func errorText(r *Refusal) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Code + " " + r.Message)
}
