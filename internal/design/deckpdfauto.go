package design

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The PDF guests download, kept up to date. Sharing a deck with the PDF on never waits for the file: the
// link is made at once and the deck is marked (DeckPdfAutoFile, naming the runtime that keeps it). That runtime's
// keeper builds the PDF in the background through the ordinary export job, which publishes it into DeckPublishedDir,
// the folder the guests' reads go through, and builds it again once the deck has been quiet for a few seconds. The
// published file carries the time its build began, so a change made while it ran leaves it stale and it is built once
// more. A build that failed is not tried again until the deck changes.

// DeckPdfAutoFile marks a deck whose PDF is kept for guests; it holds the runtime that keeps it.
const DeckPdfAutoFile = ".media/pdf-auto"

var (
	// deckPdfEvery is how often the keeper looks at the marked decks.
	deckPdfEvery = 3 * time.Second
	// deckPdfQuiet is how long a deck stays unchanged before its PDF is built again.
	deckPdfQuiet = 5 * time.Second
	// deckPdfMaxWait is how long a deck that keeps changing (an agent writing it) waits for a new PDF at most.
	deckPdfMaxWait = 45 * time.Second
	// deckPdfGap is the least time between two builds of one deck.
	deckPdfGap = 15 * time.Second
	// deckPdfWalkMax caps the files the keeper looks at in one project.
	deckPdfWalkMax = 5000
)

// deckPdfDeck is the keeper's memory of one deck.
type deckPdfDeck struct {
	staleSince time.Time
	started    time.Time
	job        string
	jobRev     int64
	failedRev  int64
	failed     *string
}

type deckPdfKeep struct {
	mu    sync.Mutex
	decks map[string]*deckPdfDeck
}

var deckPdfStates sync.Map

func (l *Library) deckPdfs() *deckPdfKeep {
	if s, ok := deckPdfStates.Load(l); ok {
		return s.(*deckPdfKeep)
	}
	s, _ := deckPdfStates.LoadOrStore(l, &deckPdfKeep{decks: map[string]*deckPdfDeck{}})
	return s.(*deckPdfKeep)
}

// deckPdfMe is the name this runtime writes into the marker.
func (l *Library) deckPdfMe() string {
	id, err := l.Identity()
	if err != nil {
		return ""
	}
	return id.Runtime
}

// deckPdfOwner is the runtime that keeps the deck's PDF; false when nobody does.
func (l *Library) deckPdfOwner(id string) (string, bool) {
	raw, err := readFileLimited(l.deckFile(id, DeckPdfAutoFile), 256)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

// SetDeckPdfAuto starts or stops keeping the deck's PDF for guests. Starting answers at once: the first build runs in
// the background. Stopping leaves the published file where it is (a link that still offers it keeps working).
func (l *Library) SetDeckPdfAuto(id string, on bool) (DeckPublished, error) {
	if err := l.requireDeck(id); err != nil {
		return DeckPublished{}, err
	}
	file := l.deckFile(id, DeckPdfAutoFile)
	keep := l.deckPdfs()
	if !on {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return DeckPublished{}, err
		}
		keep.mu.Lock()
		delete(keep.decks, id)
		keep.mu.Unlock()
		return l.DeckPublishedState(id), nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return DeckPublished{}, err
	}
	if err := writeFileAtomic(file, []byte(l.deckPdfMe()+"\n")); err != nil {
		return DeckPublished{}, err
	}
	l.keepDeckPdf(id, time.Now())
	return l.DeckPublishedState(id), nil
}

// deckContentRev is the newest mtime (ms) of what the PDF is made of: the project's files (boards, pictures, the
// theme), the pages and the settings. Notes, comments, recordings and exports are not in it.
func (l *Library) deckContentRev(id string) int64 {
	var newest int64
	note := func(info fs.FileInfo) {
		if ms := info.ModTime().UnixMilli(); ms > newest {
			newest = ms
		}
	}
	seen := 0
	_ = filepath.WalkDir(l.ProjectDir(id), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		seen++
		if seen > deckPdfWalkMax {
			return filepath.SkipAll
		}
		if strings.HasPrefix(d.Name(), ".") && path != l.ProjectDir(id) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			note(info)
		}
		return nil
	})
	// The pages' folder moves when a page goes; meta/ itself moves when the notes' or comments' folder is made.
	if st, err := os.Stat(l.pagesDir(id)); err == nil {
		note(st)
	}
	for _, dir := range []string{l.MetaDir(id), l.pagesDir(id)} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if info, err := e.Info(); err == nil {
				note(info)
			}
		}
	}
	return newest
}

// deckPdfFrom is the time (ms) the published PDF's build began, 0 when there is none.
func (l *Library) deckPdfFrom(id string) int64 {
	st, err := os.Stat(filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckPublishedDir), "deck.pdf"))
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		return 0
	}
	return st.ModTime().UnixMilli()
}

// keepDeckPdf looks at one marked deck and starts its build when the PDF is behind the deck: at once when there is no
// PDF yet, else once the deck has been quiet for deckPdfQuiet (or has kept changing for deckPdfMaxWait).
func (l *Library) keepDeckPdf(id string, now time.Time) {
	keep := l.deckPdfs()
	keep.mu.Lock()
	defer keep.mu.Unlock()
	d := keep.decks[id]
	if d == nil {
		d = &deckPdfDeck{}
		keep.decks[id] = d
	}
	if d.job != "" {
		j, _, ok := l.DeckExportJobOf(id, d.job)
		if ok && (j.State == DeckJobQueued || j.State == DeckJobRunning) {
			return
		}
		if ok && j.State == DeckJobFailed {
			d.failedRev, d.failed = d.jobRev, j.Error
		} else {
			d.failedRev, d.failed = 0, nil
		}
		d.job = ""
	}
	rev, from := l.deckContentRev(id), l.deckPdfFrom(id)
	if rev <= from {
		d.staleSince = time.Time{}
		return
	}
	if rev == d.failedRev {
		return
	}
	if d.staleSince.IsZero() {
		d.staleSince = now
	}
	quiet := now.Sub(time.UnixMilli(rev)) >= deckPdfQuiet
	waited := now.Sub(d.staleSince) >= deckPdfMaxWait
	if from != 0 && !quiet && !waited {
		return
	}
	if !d.started.IsZero() && now.Sub(d.started) < deckPdfGap {
		return
	}
	if len(l.DeckSlides(id)) == 0 {
		return
	}
	j, err := l.StartDeckExport(id, DeckExportPDF, "", true)
	if err != nil {
		msg := line(err.Error())
		d.failedRev, d.failed = rev, &msg
		return
	}
	d.job, d.jobRev, d.started, d.staleSince = j.ID, rev, now, time.Time{}
}

// deckPdfStatus adds what the keeper knows to the published state: whether the PDF is kept, built now or behind the
// deck, when the published one was built from and why the last build failed.
func (l *Library) deckPdfStatus(id string, p *DeckPublished) {
	if p.PDF {
		at := time.UnixMilli(l.deckPdfFrom(id)).UTC().Format(time.RFC3339)
		p.PDFAt = &at
	}
	if _, on := l.deckPdfOwner(id); !on {
		return
	}
	p.PDFAuto = true
	keep := l.deckPdfs()
	keep.mu.Lock()
	d := keep.decks[id]
	var job string
	var failedRev int64
	if d != nil {
		job, failedRev = d.job, d.failedRev
		if d.failed != nil {
			msg := *d.failed
			p.PDFError = &msg
		}
	}
	keep.mu.Unlock()
	if job != "" {
		if j, _, ok := l.DeckExportJobOf(id, job); ok && (j.State == DeckJobQueued || j.State == DeckJobRunning) {
			p.PDFBuilding = true
			return
		}
	}
	rev := l.deckContentRev(id)
	p.PDFBuilding = rev > l.deckPdfFrom(id) && rev != failedRev && len(l.DeckSlides(id)) > 0
	if p.PDFBuilding {
		p.PDFError = nil
	}
}

// keepDeckPdfs is the keeper: every deckPdfEvery it looks at the decks this runtime keeps the PDF of.
func (l *Library) keepDeckPdfs(stop chan struct{}) {
	t := time.NewTicker(deckPdfEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			l.keepDeckPdfsOnce(time.Now())
		}
	}
}

func (l *Library) keepDeckPdfsOnce(now time.Time) {
	root := l.Root()
	if root == "" {
		return
	}
	names, _ := readNames(root)
	me := l.deckPdfMe()
	kept := map[string]bool{}
	for _, id := range names {
		if !IsLibraryID(id) {
			continue
		}
		if owner, on := l.deckPdfOwner(id); on && owner == me {
			kept[id] = true
			l.keepDeckPdf(id, now)
		}
	}
	keep := l.deckPdfs()
	keep.mu.Lock()
	for id := range keep.decks {
		if !kept[id] {
			delete(keep.decks, id)
		}
	}
	keep.mu.Unlock()
}
