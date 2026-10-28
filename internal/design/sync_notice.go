package design

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Writes by revision. When the design-sync agent of this machine finds that a board was written by someone
// else while the agent's chat was editing it, the other version takes the board's place and the chat's contents are
// kept in <library>/.conflicts/<project>/project/<board>.<machine>.<time> with a notice <same>.json beside them
// (design-sync, internal/agent/revision.go). A chat that comes back to the board is told, once, what happened to its
// edit: re-apply it on the fresh version (a small edit), or go on in the variant project it was saved as.

const syncConflictsDirname = ".conflicts"

// SyncNotice is a notice of the design-sync agent about a write that lost.
type SyncNotice struct {
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	Kept       string `json:"kept"`
	Project    string `json:"project"`
	Board      string `json:"board"`
	Variant    string `json:"variant"`
	CurrentRev int64  `json:"current_rev"`
	By         string `json:"by"`
	ByMachine  string `json:"by_machine"`
	Machine    string `json:"machine"`
	At         string `json:"at"`

	file string
}

// TakeSyncNotice is the oldest notice about a lost write on a board of the project, taken: it is renamed to
// "….json.seen", so it is told once. nil when there is none.
func (l *Library) TakeSyncNotice(project, boardFile string) *SyncNotice {
	if !IsLibraryID(project) || boardFile == "" || strings.ContainsAny(boardFile, `\/`) {
		return nil
	}
	dir := filepath.Join(l.Root(), syncConflictsDirname, project, ProjectDirname)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); e.Type().IsRegular() && strings.HasPrefix(n, boardFile+".") && strings.HasSuffix(n, ".json") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		p := filepath.Join(dir, n)
		f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(f, 16<<10))
		_ = f.Close()
		var notice SyncNotice
		if json.Unmarshal(raw, &notice) != nil || notice.Kind == "" {
			continue
		}
		if os.Rename(p, p+".seen") != nil {
			continue
		}
		notice.file = p
		return &notice
	}
	return nil
}

// Message is the words for the chat whose edit lost.
func (n SyncNotice) Message() string {
	by, _ := CleanText(n.By, 64)
	if by == "" {
		by = locale.Pick("somebody", "кто-то")
	}
	machine, _ := CleanText(n.ByMachine, 64)
	who := by
	if machine != "" {
		who += " (" + machine + ")"
	}
	board, _ := CleanText(strings.TrimSuffix(n.Board, dcSuffix), 120)
	kept, _ := CleanText(n.Kept, 300)
	if n.Kind == "variant" {
		variant, _ := CleanText(n.Variant, 64)
		return locale.Pick(
			"While you were editing the board \""+board+"\", "+who+" saved a new version of it: yours was not written. Your edit was big, so it was saved as the variant project \""+variant+"\" (your version is also in "+kept+"). Go on in the variant, or read the board again and move what you need into it.",
			"Пока ты правил доску «"+board+"», "+who+" сохранил(а) её новую версию: твоя правка не записана. Правка крупная, поэтому она сохранена вариантом: проект «"+variant+"» (твоя версия ещё и в "+kept+"). Продолжай в варианте или перечитай доску и перенеси нужное в неё.")
	}
	return locale.Pick(
		"While you were editing the board \""+board+"\", "+who+" saved a new version of it: yours was not written. Your version is kept in "+kept+". Read the board again and apply your edit to the fresh version.",
		"Пока ты правил доску «"+board+"», "+who+" сохранил(а) её новую версию: твоя правка не записана. Твоя версия сохранена в "+kept+". Перечитай доску и примени правку заново на свежей версии.")
}
