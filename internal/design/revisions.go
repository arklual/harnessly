package design

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/storerev"
)

// The history of a library that syncs with a store (HRN-880) is the store's revisions, kept in the center: every
// change of every file, whichever machine made it, with no git repository beside the library. A board's versions, the
// return to one of them and the version a guest link is pinned to are read from there; the machine whose library it
// is may be off. A library without a store keeps its git history as before, and a version pinned in git still opens.

// Revision is one revision of a file in the store.
type Revision struct {
	Rev             int64
	Path, Sha       string
	Author, Machine string
	At              int64 // ms
	Deleted         bool
}

// Revisions is the store's history of the library as the runtime reaches it.
type Revisions interface {
	// History is the revisions of one path, newest first.
	History(ctx context.Context, path string) ([]Revision, error)
	// HistoryUnder is the newest revisions under a folder (prefix ends in "/"; "" for the whole library).
	HistoryUnder(ctx context.Context, prefix string, limit int) ([]Revision, error)
	// Snapshot is a folder as it was at a revision, without what was deleted by then.
	Snapshot(ctx context.Context, prefix string, at int64) ([]Revision, error)
	// Read is the contents of a revision by its sha256.
	Read(ctx context.Context, sha string) ([]byte, error)
}

// RevisionLeases is a store that keeps short claims too (HRN-881): a task one machine of the space does for all, as
// the PDF of a deck, is done by the one that holds its lease. The lease lapses by itself when its holder stops
// refreshing it.
type RevisionLeases interface {
	// TakeLease takes or refreshes the lease on key for chat: true when this machine holds it now.
	TakeLease(ctx context.Context, key, chat string) (bool, error)
}

type revisionsBox struct{ r Revisions }

// UseRevisions makes the store's revisions the library's history; nil goes back to the git history.
func (l *Library) UseRevisions(r Revisions) {
	if r == nil {
		l.revs.Store(nil)
		return
	}
	l.revs.Store(&revisionsBox{r: r})
}

func (l *Library) revisions() Revisions {
	if b := l.revs.Load(); b != nil {
		return b.r
	}
	return nil
}

// StoreRevID is the version id of a store revision (storerev.ID).
func StoreRevID(rev int64) string { return storerev.ID(rev) }

// StoreRevOf reads a version id made by StoreRevID: the store's revision, false for a version of git.
func StoreRevOf(id string) (int64, bool) { return storerev.Of(id) }

func storeRevOf(id string) (int64, bool) { return storerev.Of(id) }

const revisionsTimeout = 20 * time.Second

// storeHistory is readHistory over the store: a board's revisions, or a project's newest ones.
func (l *Library) storeHistory(r Revisions, project string, file *string, limit float64) (scopeHistory, error) {
	target, err := ScopePath(project, file)
	if err != nil {
		return scopeHistory{}, err
	}
	n := min(max(int(limit), 1), 200)
	ctx, cancel := context.WithTimeout(context.Background(), revisionsTimeout)
	defer cancel()
	var revs []Revision
	if file != nil {
		revs, err = r.History(ctx, target)
	} else {
		revs, err = r.HistoryUnder(ctx, target+"/", n)
	}
	if err != nil {
		return scopeHistory{}, historyErr(locale.Pick("The history is not reachable now: "+err.Error(), "История сейчас недоступна: "+err.Error()))
	}
	out := scopeHistory{}
	for _, e := range revs {
		if len(out.entries) == n {
			break
		}
		if e.Deleted {
			continue
		}
		who := strings.TrimSpace(strings.Trim(e.Author+" · "+e.Machine, " ·"))
		var editors []string
		if who != "" {
			editors = []string{who}
		}
		machine := e.Machine
		out.entries = append(out.entries, HistoryEntry{ID: StoreRevID(e.Rev), At: e.At, Editors: editors, Runtime: &machine})
	}
	// A board edited here and not in the store yet (the sync sends it within a second) is unsaved.
	if file != nil && len(revs) > 0 {
		if data, err := os.ReadFile(filepath.Join(l.Root(), filepath.FromSlash(target))); err == nil && sha256Hex(data) != revs[0].Sha {
			out.unsaved = true
		}
	}
	return out, nil
}

// storeRestore puts a board back to a store revision: its contents are written over the file, and the sync sends them
// as a new revision; the history only grows, and nothing is saved first since every revision is kept already.
func (l *Library) storeRestore(r Revisions, project, file string, wanted RestoreTarget, now time.Time) (restoreResult, error) {
	target, err := ScopePath(project, &file)
	if err != nil {
		return restoreResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), revisionsTimeout)
	defer cancel()
	revs, err := r.History(ctx, target)
	if err != nil {
		return restoreResult{}, historyErr(locale.Pick("The history is not reachable now: "+err.Error(), "История сейчас недоступна: "+err.Error()))
	}
	var pick *Revision
	if n, ok := storeRevOf(wanted.Revision); ok {
		for i := range revs {
			if revs[i].Rev == n {
				pick = &revs[i]
				break
			}
		}
		if pick == nil {
			return restoreResult{}, historyErr(locale.Pick("No such version in the history", "Такой версии нет в истории"))
		}
	} else {
		before := now.Add(-wanted.Ago).UnixMilli()
		for i := range revs {
			if revs[i].At <= before {
				pick = &revs[i]
				break
			}
		}
		if pick == nil {
			return restoreResult{}, historyErr(locale.Pick("The board had no saved versions before that time", "Раньше этого времени у доски не было сохранённых версий"))
		}
	}
	if pick.Deleted {
		return restoreResult{}, historyErr(locale.Pick("This board did not exist in that version yet", "В этой версии такой доски ещё не было"))
	}
	data, err := r.Read(ctx, pick.Sha)
	if err != nil {
		return restoreResult{}, historyErr(locale.Pick("That version is not reachable now: "+err.Error(), "Эта версия сейчас недоступна: "+err.Error()))
	}
	if page, has := l.pageOfFile(project, file); has {
		if t, rec := page.Template(); rec && t.Locked() && l.checkBoardLock(page, string(data)) != nil {
			board := BoardTitleOf(file)
			return restoreResult{}, historyErr(locale.Pick(
				"The board \""+board+"\" is locked by the app \""+t.App+"\", and that version does not keep what is closed: unbind the board from the template first",
				"Доска «"+board+"» закрыта приложением «"+t.App+"», а в этой версии закрытая часть другая: сначала отвяжите доску от шаблона"))
		}
	}
	dir := filepath.Join(l.Root(), project)
	if !isPlainFolder(l.Root(), project) || !isPlainFolder(dir, ProjectDirname) {
		return restoreResult{}, historyErr(locale.Pick("The project folder is not a plain folder: a version cannot be restored", "Папка проекта не обычная папка: вернуть версию нельзя"))
	}
	if _, err := writeIfChanged(filepath.Join(l.Root(), filepath.FromSlash(target)), data); err != nil {
		return restoreResult{}, err
	}
	return restoreResult{revision: StoreRevID(pick.Rev), snapshot: snapshotOutcome{kind: SnapshotCommitted}}, nil
}

// storePin is the version a guest link pins to: the store's newest revision of the library.
func (l *Library) storePin(r Revisions) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), revisionsTimeout)
	defer cancel()
	revs, err := r.HistoryUnder(ctx, "", 1)
	if err != nil {
		return "", err
	}
	if len(revs) == 0 {
		return "", historyErr(locale.Pick("Nothing is in the store yet", "В хранилище пока ничего нет"))
	}
	return StoreRevID(revs[0].Rev), nil
}

// extractStorePinned writes the project as it was at a store revision into base/<project>: plain files only, beside the
// target first and moved into place, as extractPinned does for git.
func (l *Library) extractStorePinned(r Revisions, at int64, project, base string) error {
	target := filepath.Join(base, project)
	if isPlainFolder(base, project) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	revs, err := r.Snapshot(ctx, project+"/", at)
	if err != nil {
		return err
	}
	if len(revs) == 0 {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(base, ".pin-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var total int64
	for _, e := range revs {
		rel, ok := strings.CutPrefix(e.Path, project+"/")
		if !ok || rel == "" || path.Clean(rel) != rel || strings.HasPrefix(rel, "../") {
			continue
		}
		data, err := r.Read(ctx, e.Sha)
		if err != nil {
			return err
		}
		if total += int64(len(data)); total > pinnedBytes {
			return errPinnedTooBig
		}
		file := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(file, data, 0o600); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, target); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
