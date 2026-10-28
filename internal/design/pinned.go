package design

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// A guest link may be pinned to a version of the design: the gateway puts the history revision into the link's token,
// and the runtime answers from that snapshot instead of from the folder the owner keeps editing. The snapshot is taken
// out of the history repository once, into a library folder of its own, and read by a second Library over that folder,
// so a pinned page goes through the very same listing and serving code as a live one.

const (
	pinnedDirname = "design-pinned"
	// pinnedKeep is how many snapshots stay extracted; the oldest one is removed when a new one comes.
	pinnedKeep = 12
	// pinnedBytes caps what one snapshot may take on disk.
	pinnedBytes = 1 << 30
	// pinRecent is how long a taken revision answers a repeated ask.
	pinRecent = 2 * time.Second
)

var errPinnedTooBig = errors.New("pinned snapshot exceeds its size limit")

type pinnedState struct {
	mu    sync.Mutex
	libs  map[string]*Library
	order []string
}

// PinRevision saves what the owner has now as a snapshot (the history waits for the files to be quiet for a while, a
// link must not) and returns the revision to pin a link to.
func (l *Library) PinRevision() (string, error) {
	// Asked again at once, the answer is the one just given: nothing is walked or saved twice in a row.
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	if l.lastPin != "" && time.Since(l.lastPinAt) < pinRecent {
		return l.lastPin, nil
	}
	rev, err := l.pinRevision()
	if err == nil {
		l.lastPin, l.lastPinAt = rev, time.Now()
	}
	return rev, err
}

func (l *Library) pinRevision() (string, error) {
	h := &l.hist
	h.mu.Lock()
	runtime := h.runtime
	h.mu.Unlock()
	h.chain.Lock()
	out, err := l.snapshotNow(runtime, nil, "", time.Now())
	h.chain.Unlock()
	if err != nil {
		return "", err
	}
	if out.kind == SnapshotBusy {
		return "", historyErr(locale.Pick("The history is busy, try again", "История занята, попробуйте ещё раз"))
	}
	repo, err := l.ensureRepo()
	if err != nil {
		return "", err
	}
	sha, err := l.gitOK(repo, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// pinnedLibrary is the library holding the project as it was at the revision; false when the history has no such
// version of it.
func (l *Library) pinnedLibrary(project, rev string) (*Library, bool) {
	if !fullSHA.MatchString(rev) || !IsLibraryID(project) {
		return nil, false
	}
	key := rev + "/" + project
	p := &l.pinned
	p.mu.Lock()
	defer p.mu.Unlock()
	if lib := p.libs[key]; lib != nil {
		return lib, true
	}
	repo, err := l.historyRepo()
	if err != nil {
		return nil, false
	}
	base := filepath.Join(l.pinnedBase(), rev)
	if err := l.extractPinned(repo, rev, project, base); err != nil {
		return nil, false
	}
	cfg := l.cfg
	cfg.LibraryDir, cfg.WorkspaceDir, cfg.AltWorkspaceDir, cfg.WorkspaceConfig = base, "", "", ""
	lib := New(cfg)
	if p.libs == nil {
		p.libs = map[string]*Library{}
	}
	p.libs[key] = lib
	p.order = append(p.order, key)
	for len(p.order) > pinnedKeep {
		old := p.order[0]
		p.order = p.order[1:]
		delete(p.libs, old)
		rev, project, _ := strings.Cut(old, "/")
		_ = os.RemoveAll(filepath.Join(l.pinnedBase(), rev, project))
	}
	return lib, true
}

func (l *Library) pinnedBase() string {
	dir := l.cfg.HistoryDir
	if dir == "" {
		dir = l.cfg.DataDir
	}
	return filepath.Join(dir, pinnedDirname)
}

// extractPinned unpacks the project's folder of the revision into base/<project>. Only plain files and folders are
// taken: a link in a snapshot must not point a guest's read anywhere. The work is done beside the target and moved
// into place, so a failed run leaves nothing half-written.
func (l *Library) extractPinned(repo historyRepo, rev, project, base string) error {
	target := filepath.Join(base, project)
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		return nil
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(base, ".extracting-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	ctx, cancel := context.WithTimeout(context.Background(), historyGitTime)
	defer cancel()
	args := append(l.safeConfig(), "--git-dir="+repo.gitDir, "archive", "--format=tar", rev, "--", project)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env, cmd.Dir = gitEnv(), repo.root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	readErr := untarPlain(out, stage)
	// The pipe is drained so git can finish, whatever the reading ended with.
	_, _ = io.Copy(io.Discard, out)
	if err := cmd.Wait(); err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if st, err := os.Stat(filepath.Join(stage, project)); err != nil || !st.IsDir() {
		return errors.New("the revision has no such project")
	}
	return os.Rename(filepath.Join(stage, project), target)
}

func untarPlain(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(hdr.Name)
		if name == "." || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			total += hdr.Size
			if total > pinnedBytes {
				return errPinnedTooBig
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			if err := f.Close(); copyErr == nil {
				copyErr = err
			}
			if copyErr != nil {
				return copyErr
			}
		}
	}
}
