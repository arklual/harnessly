package designview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// A hand edit made in Sinopia's editor of a deck or a video (its settings, a slide's notes, an element moved
// on a slide, a comment) is saved through the hub: the hub keeps a working copy of the project as the store has it,
// runs the runtime's own handler of the edit on it, and commits every file the handler changed on top of the revision
// it was read at. Nobody's machine takes part; the machines of the space take the new revision as they take any other.
// What needs a live process (a recording, an export, the model tidying notes, a slide from a kit) stays the machine's.

// EditRoutes are the edits the hub saves itself, by their path under <platform api>/design/.
var EditRoutes = map[string]func(*design.API, *http.Request) design.Answer{
	"deck":           (*design.API).Deck,
	"deck/settings":  (*design.API).DeckSettings,
	"deck/notes":     (*design.API).DeckNotes,
	"deck/patch":     (*design.API).DeckPatch,
	"deck/comments":  (*design.API).DeckComments,
	"deck/pdf":       (*design.API).DeckPdf,
	"video":          (*design.API).Video,
	"video/settings": (*design.API).VideoSettings,
	"video/scene":    (*design.API).VideoScene,
	"video/overlay":  (*design.API).VideoOverlay,
	"video/comments": (*design.API).VideoComments,
}

// workspace is the hub's working copy of one project: the files as of head, with what each was when written.
type workspace struct {
	mu    sync.Mutex
	base  string // the library folder; the project is base/<project>
	head  int64  // the store's revision of the project the copy is; 0 when it has to be read again
	files map[string]wsFile
}

type wsFile struct {
	rev   int64
	sha   string
	size  int64
	mtime time.Time
}

// Edit runs an edit route through EditAnswer and writes the handler's answer to w; an error comes before anything is
// written (designaccess.Viewer).
func (v *View) Edit(ctx context.Context, w http.ResponseWriter, r *http.Request, spaceID, project, author, route string) error {
	ans, err := v.EditAnswer(ctx, spaceID, project, author, route, r)
	if err != nil {
		return err
	}
	gate.WriteJSON(w, ans.Status, ans.Body, ans.Header)
	return nil
}

// EditAnswer runs an edit route on the project's working copy and commits what it changed. A file changed in the
// store since the copy was read makes the whole edit ErrConflict (the files committed before it stay; the editor reads
// the project again). The answer is the handler's.
func (v *View) EditAnswer(ctx context.Context, spaceID, project, author, route string, r *http.Request) (design.Answer, error) {
	handler := EditRoutes[route]
	if handler == nil || !design.IsLibraryID(project) || strings.ContainsAny(spaceID, "/\\.") {
		return design.Answer{}, ErrBadPath
	}
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return design.Answer{}, err
	}
	wr, ok := room.(Writer)
	if !ok {
		return design.Answer{}, ErrReadOnly
	}
	ws := v.workspaceOf(spaceID, project)
	ws.mu.Lock()
	defer ws.mu.Unlock()
	head, err := v.Head(ctx, spaceID, project)
	if err != nil {
		return design.Answer{}, err
	}
	if head == 0 {
		return design.Answer{}, ErrMissing
	}
	if ws.head != head {
		if err := v.readWorkspace(ctx, room, ws, spaceID, project, head); err != nil {
			ws.head = 0
			return design.Answer{}, err
		}
	}
	lib := design.New(design.Config{
		LibraryDir: ws.base,
		DataDir:    filepath.Join(v.o.Dir, "edit-data"),
		Passive:    true,
		ShareProbe: func(string) design.ShareFSReport { return design.ShareFSReport{OK: true, At: time.Now()} },
	})
	api := &design.API{
		L:           lib,
		PlatformAPI: "/api/platform",
		// The hub let the person in with their role (designaccess): the copy's own rules have nothing to add.
		Session:    func(*http.Request) bool { return true },
		Person:     func(*http.Request) string { return author },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false },
	}
	ans := handler(api, r)
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return ans, nil
	}
	if err := v.commitWorkspace(ctx, wr, ws, spaceID, project, author); err != nil {
		ws.head = 0
		return design.Answer{}, err
	}
	return ans, nil
}

func (v *View) workspaceOf(spaceID, project string) *workspace {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.edits == nil {
		v.edits = map[string]*workspace{}
	}
	key := spaceID + "/" + project
	ws := v.edits[key]
	if ws == nil {
		ws = &workspace{base: filepath.Join(v.o.Dir, "edit", spaceID, project)}
		v.edits[key] = ws
	}
	return ws
}

// readWorkspace writes the project as of head into the copy's folder: writable files of its own, not the kept ones.
func (v *View) readWorkspace(ctx context.Context, room Room, ws *workspace, spaceID, project string, head int64) error {
	snapshot, err := room.Snapshot(ctx, project+"/", head)
	if err != nil {
		return err
	}
	if len(snapshot.Entries) > maxFiles {
		return fmt.Errorf("designview: %d files in one project", len(snapshot.Entries))
	}
	if err := os.RemoveAll(ws.base); err != nil {
		return err
	}
	dir := filepath.Join(ws.base, project)
	files := map[string]wsFile{}
	var total int64
	for _, e := range snapshot.Entries {
		rel, ok := strings.CutPrefix(e.Path, project+"/")
		if !ok || !filestore.SafeRel(e.Path) || !filestore.ValidSha(e.Sha) {
			continue
		}
		if total += e.Size; total > v.o.MaxBytes {
			return fmt.Errorf("designview: the project is over %d bytes", v.o.MaxBytes)
		}
		blob, err := v.blob(ctx, room, spaceID, e.Sha)
		if err != nil {
			return err
		}
		file := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return err
		}
		if err := copyWritable(blob, file); err != nil {
			return err
		}
		st, err := os.Stat(file)
		if err != nil {
			return err
		}
		files[rel] = wsFile{rev: e.Rev, sha: e.Sha, size: st.Size(), mtime: st.ModTime()}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ws.files, ws.head = files, head
	return nil
}

func copyWritable(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// commitWorkspace commits what changed in the copy since it was read or last committed: new and changed files on
// their revision, deleted ones as deletions. Files the store does not keep stay in the copy only.
func (v *View) commitWorkspace(ctx context.Context, w Writer, ws *workspace, spaceID, project, author string) error {
	dir := filepath.Join(ws.base, project)
	seen := map[string]bool{}
	var last int64
	commit := func(rel string, req filestore.CommitRequest) error {
		e, conflict, err := w.Commit(ctx, req, HubMachine)
		if err != nil {
			if errors.Is(err, filestore.ErrBadRequest) {
				v.o.Log.Info("design edit: a file the store does not keep stays in the hub's copy", "project", project, "file", rel)
				return nil
			}
			return err
		}
		if conflict {
			return ErrConflict
		}
		if e != nil {
			last = max(last, e.Rev)
			if req.Deleted {
				delete(ws.files, rel)
			} else {
				st, _ := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
				f := wsFile{rev: e.Rev, sha: req.Sha, size: req.Size}
				if st != nil {
					f.mtime = st.ModTime()
				}
				ws.files[rel] = f
			}
		}
		return nil
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		st, err := d.Info()
		if err != nil {
			return err
		}
		old, had := ws.files[rel]
		if had && old.size == st.Size() && old.mtime.Equal(st.ModTime()) {
			return nil
		}
		if st.Size() > filestore.MaxFileSize {
			return fmt.Errorf("designview: %s is over the store's size", rel)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		if had && old.sha == sha {
			old.mtime, old.size = st.ModTime(), st.Size()
			ws.files[rel] = old
			return nil
		}
		if err := w.PutBlob(ctx, sha, strings.NewReader(string(data))); err != nil {
			return err
		}
		return commit(rel, filestore.CommitRequest{Path: project + "/" + rel, Base: old.rev, Sha: sha, Size: int64(len(data)), Author: author})
	})
	if err != nil {
		return err
	}
	for rel, f := range ws.files {
		if !seen[rel] {
			if err := commit(rel, filestore.CommitRequest{Path: project + "/" + rel, Base: f.rev, Deleted: true, Author: author}); err != nil {
				return err
			}
		}
	}
	// The copy is the store's head only when nobody else wrote the project in between.
	if last > 0 {
		head, err := v.Head(ctx, spaceID, project)
		if err != nil || head != last {
			ws.head = 0
		} else {
			ws.head = head
		}
	}
	return nil
}
