package design

import (
	"context"
	"errors"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
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
	beat       time.Time // the last refresh of the store's lease while a build runs
	started    time.Time
	job        string
	jobRev     int64
	failedRev  int64
	failed     *string
}

type deckPdfKeep struct {
	mu    sync.Mutex
	decks map[string]*deckPdfDeck
	// revs is the store's word on each deck (deckStoreRev), under revMu with the rest below.
	revMu sync.Mutex
	revs  map[string]deckStoreRevAt
	// touched is when a file a deck's PDF is made of last changed in this machine's folder, by the deck; touchedAll is
	// the same for a change that names no deck. The store is asked again until it has answered deckPdfSettle after it.
	touched    map[string]time.Time
	touchedAll time.Time
	// recheck is from when a deck's word is asked for once more: the store itself showed a newer revision of it.
	recheck    map[string]time.Time
	recheckAll time.Time
	// failed is when the store last did not answer about a deck.
	failed map[string]time.Time
	// following: the changes of the folder are heard (follow). top is the newest revision of the whole library the
	// look around saw, topNext when it looks again.
	following bool
	top       int64
	topKnown  bool
	topNext   time.Time
}

type deckStoreRevAt struct {
	rev     int64
	at      time.Time
	checked time.Time
}

// A library synced with a store that keeps leases builds the PDF of a marked deck on any machine of the space
// whose person may edit it, one machine at a time: the one holding the store's lease "pdf/<deck>". Whether the PDF is
// behind is told by the store's revisions (the newest of what the PDF is made of, against the one written beside the
// published PDF, deckPdfRevFile), not by the files' times: a file another machine wrote has the time it arrived.

// deckPdfRevFile holds the store revision the published PDF was built from.
const deckPdfRevFile = DeckPublishedDir + "/deck.pdf.rev"

// deckPdfLeaseKey leads the lease of a deck's PDF: ":" is in no project id, so no board's lease ("<project>/<board>")
// is ever this one, and the board leases' list skips it.
const deckPdfLeaseKey = "task:pdf/"

// deckPdfLeaseRefresh is how often a machine building a PDF says it still does (the lease lapses after two minutes).
const deckPdfLeaseRefresh = 30 * time.Second

var (
	// deckPdfStoreFor says how long the store's word on a deck is taken as it is, whatever happened since.
	deckPdfStoreFor = 5 * time.Second
	// deckPdfSettle is how long after a change in the folder the store is still asked: this machine's own edit reaches
	// the store a moment after it reaches the disk.
	deckPdfSettle = 20 * time.Second
	// deckPdfLookEvery is how often the store is asked whether anything changed that the folder's watcher did not
	// tell: one request for the whole library, spread by up to half of it so the machines of a space do not ask
	// together. deckPdfBlindEvery is the same while the folder is not watched at all.
	deckPdfLookEvery  = 3 * time.Minute
	deckPdfBlindEvery = 15 * time.Second
	// deckPdfLookMax is how many of the library's newest revisions one look around reads.
	deckPdfLookMax = 100
)

// deckPdfStore is the store whose leases decide who builds; nil without one (the runtime named in the mark builds).
func (l *Library) deckPdfStore() (Revisions, RevisionLeases) {
	r := l.revisions()
	if r == nil {
		return nil, nil
	}
	ls, ok := r.(RevisionLeases)
	if !ok {
		return nil, nil
	}
	return r, ls
}

// deckPdfPart says which deck a path of the library (or of the store) belongs to when it is one of the files the PDF
// is made of: anything in the project folder, the files of meta/ and of meta/pages/.
func deckPdfPart(rel string) (string, bool) {
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 3)
	if len(parts) < 2 || parts[0] == "" {
		return "", false
	}
	switch parts[1] {
	case ProjectDirname:
		return parts[0], true
	case MetaDirname:
		if len(parts) == 2 {
			return parts[0], true
		}
		page, inPages := strings.CutPrefix(parts[2], MetaPagesDirname+"/")
		if !strings.Contains(parts[2], "/") || inPages && !strings.Contains(page, "/") {
			return parts[0], true
		}
	}
	return "", false
}

// follow hears the folder's changes from then on: a change made here or brought by the sync is what tells that the
// store may have a new revision of a deck, so an unchanged deck asks the store nothing.
func (k *deckPdfKeep) follow(l *Library) {
	k.revMu.Lock()
	was := k.following
	k.following = true
	k.revMu.Unlock()
	if was {
		return
	}
	l.Subscribe(func(rel string) { k.changed(rel, time.Now()) })
}

// changed notes a change in the folder.
func (k *deckPdfKeep) changed(rel string, now time.Time) {
	k.revMu.Lock()
	defer k.revMu.Unlock()
	if rel == "." {
		k.touchedAll = now
		return
	}
	if id, ok := deckPdfPart(rel); ok {
		if k.touched == nil {
			k.touched = map[string]time.Time{}
		}
		k.touched[id] = now
	}
}

// fresh is whether the word the store gave on a deck at checked still stands (under revMu).
func (k *deckPdfKeep) fresh(id string, checked time.Time) bool {
	touched := k.touched[id]
	if k.touchedAll.After(touched) {
		touched = k.touchedAll
	}
	if !touched.IsZero() && checked.Before(touched.Add(deckPdfSettle)) {
		return false
	}
	return !checked.Before(k.recheck[id]) && !checked.Before(k.recheckAll)
}

// lookAround asks the store, once in a long while, for the newest revisions of the whole library and has the decks
// they name asked about again: what covers a change the watcher never told (it was not running, or the kernel dropped
// the event). With nothing new in the store that is one request and nothing more.
func (l *Library) lookAround(r Revisions, now time.Time) {
	keep := l.deckPdfs()
	keep.revMu.Lock()
	due := !now.Before(keep.topNext)
	keep.revMu.Unlock()
	if !due {
		return
	}
	every := deckPdfLookEvery
	if !l.watching() {
		every = deckPdfBlindEvery
	}
	keep.revMu.Lock()
	if now.Before(keep.topNext) {
		keep.revMu.Unlock()
		return
	}
	keep.topNext = now.Add(every + rand.N(every/2+1))
	top, known := keep.top, keep.topKnown
	keep.revMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	es, err := r.HistoryUnder(ctx, "", deckPdfLookMax)
	if err != nil {
		return
	}
	keep.revMu.Lock()
	defer keep.revMu.Unlock()
	newest, newer := top, 0
	for _, e := range es {
		newest = max(newest, e.Rev)
		if !known || e.Rev <= top {
			continue
		}
		newer++
		if id, ok := deckPdfPart(e.Path); ok {
			if keep.recheck == nil {
				keep.recheck = map[string]time.Time{}
			}
			keep.recheck[id] = now
		}
	}
	if known && newer == len(es) && newer >= deckPdfLookMax {
		// More changed than one look reads: every deck is asked about.
		keep.recheckAll = now
	}
	keep.top, keep.topKnown = newest, true
}

// deckStoreRev is the newest store revision of what the PDF is made of — the project folder, the files of meta/ and
// of meta/pages/, as deckContentRev has it — and when it was made. The store is asked when the deck is first looked
// at, after a change of its files in this machine's folder and when a look around showed a newer revision; otherwise
// the word it gave last stands.
func (l *Library) deckStoreRev(r Revisions, id string) (int64, time.Time, error) {
	keep := l.deckPdfs()
	keep.follow(l)
	started := time.Now()
	l.lookAround(r, started)
	keep.revMu.Lock()
	c, ok := keep.revs[id]
	if ok && (started.Sub(c.checked) < deckPdfStoreFor || keep.fresh(id, c.checked)) {
		keep.revMu.Unlock()
		return c.rev, c.at, nil
	}
	if at, bad := keep.failed[id]; bad && started.Sub(at) < deckPdfStoreFor {
		keep.revMu.Unlock()
		return 0, time.Time{}, errDeckPdfStoreAway
	}
	keep.revMu.Unlock()
	rev, at, err := deckStoreRevNow(r, id)
	keep.revMu.Lock()
	defer keep.revMu.Unlock()
	if err != nil {
		if keep.failed == nil {
			keep.failed = map[string]time.Time{}
		}
		keep.failed[id] = started
		return 0, time.Time{}, err
	}
	delete(keep.failed, id)
	if keep.revs == nil {
		keep.revs = map[string]deckStoreRevAt{}
	}
	keep.revs[id] = deckStoreRevAt{rev: rev, at: time.UnixMilli(at), checked: started}
	return rev, time.UnixMilli(at), nil
}

var errDeckPdfStoreAway = errors.New("the store did not answer a moment ago")

// deckStoreRevNow asks the store.
func deckStoreRevNow(r Revisions, id string) (rev, at int64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	es, err := r.HistoryUnder(ctx, id+"/"+ProjectDirname+"/", 1)
	if err != nil {
		return 0, 0, err
	}
	if len(es) > 0 {
		rev, at = es[0].Rev, es[0].At
	}
	meta := id + "/" + MetaDirname + "/"
	es, err = r.HistoryUnder(ctx, meta, 200)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range es {
		if _, part := deckPdfPart(e.Path); part {
			if e.Rev > rev {
				rev, at = e.Rev, e.At
			}
			break
		}
	}
	return rev, at, nil
}

// deckPdfFromRev is the store revision the published PDF was built from, 0 when there is none.
func (l *Library) deckPdfFromRev(id string) int64 {
	if l.deckPdfFrom(id) == 0 {
		return 0
	}
	raw, err := readFileLimited(l.deckFile(id, deckPdfRevFile), 64)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// deckPdfShared: the PDFs are kept through the store, by whichever machine holds the lease.
func (l *Library) deckPdfShared() bool {
	r, _ := l.deckPdfStore()
	return r != nil
}

// deckPdfRevs is what the PDF is made of now (rev, and when it was made) and what the published one was built from:
// the store's revisions when the library keeps its PDFs through the store, the files' times otherwise. A store that
// does not answer leaves nothing behind.
func (l *Library) deckPdfRevs(id string) (rev int64, at time.Time, from int64) {
	if r, _ := l.deckPdfStore(); r != nil {
		rev, at, err := l.deckStoreRev(r, id)
		if err != nil {
			return 0, time.Time{}, 0
		}
		return rev, at, l.deckPdfFromRev(id)
	}
	rev = l.deckContentRev(id)
	return rev, time.UnixMilli(rev), l.deckPdfFrom(id)
}

// deckPdfLease takes or refreshes this machine's lease on building the deck's PDF; true without a store's leases.
func (l *Library) deckPdfLease(id string) bool {
	_, ls := l.deckPdfStore()
	if ls == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	me := l.deckPdfMe()
	if me == "" {
		return false
	}
	got, err := ls.TakeLease(ctx, deckPdfLeaseKey+id, me+"/pdf")
	return err == nil && got
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
	if l.cfg.Passive {
		return
	}
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
			if now.Sub(d.beat) >= deckPdfLeaseRefresh {
				d.beat = now
				l.deckPdfLease(id)
			}
			return
		}
		if ok && j.State == DeckJobDone {
			if r, _ := l.deckPdfStore(); r != nil {
				// What the PDF was built from, beside it: the other machines see it is not behind.
				_ = writeFileAtomic(l.deckFile(id, deckPdfRevFile), []byte(strconv.FormatInt(d.jobRev, 10)+"\n"))
			}
		}
		if ok && j.State == DeckJobFailed {
			d.failedRev, d.failed = d.jobRev, j.Error
		} else {
			d.failedRev, d.failed = 0, nil
		}
		d.job = ""
	}
	rev, at, from := l.deckPdfRevs(id)
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
	quiet := now.Sub(at) >= deckPdfQuiet
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
	// One machine of the space builds it: the one that holds the store's lease.
	if !l.deckPdfLease(id) {
		return
	}
	d.beat = now
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
	rev, _, from := l.deckPdfRevs(id)
	p.PDFBuilding = rev > from && rev != failedRev && len(l.DeckSlides(id)) > 0
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
		// Through a store with leases any machine whose person may edit the deck keeps it; else the one named.
		if owner, on := l.deckPdfOwner(id); on && (owner == me || l.deckPdfShared() && l.Can(id, RoleEdit)) {
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
