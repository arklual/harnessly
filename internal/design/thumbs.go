package design

import (
	"crypto/sha1"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The picture of a board for the library gallery: the board opened once in a headless browser at its own size, kept as
// a JPEG next to the design (.thumbs/) under a name that carries the version of the board's file, so an edited board is
// drawn again and an untouched one never is. The browser stays up a short while after the last drawing and goes. The
// gallery asks for the pictures on screen first; the rest of a project is drawn behind them, and an edited board shows
// its earlier picture until the new one is there.

const (
	ThumbDirname    = ".thumbs"
	thumbTabs       = 3
	thumbIdle       = 45 * time.Second
	thumbRenderWait = 45 * time.Second
	thumbMinSide    = 320
	thumbMaxSide    = 2400
)

var (
	defaultThumbView = shotView{1440, 900, false}
	phoneThumbView   = shotView{390, 844, true}
	thumbPhoneRe     = regexp.MustCompile(`[?&]m=1\b`)
)

type thumbTurn struct {
	key   string
	start chan struct{}
}

type thumbState struct {
	mu       sync.Mutex
	browser  *browser
	starting chan struct{}
	idle     *time.Timer
	active   int
	queue    []thumbTurn
	inflight map[string]*thumbDrawing
	warming  map[string]bool
}

// thumbDrawing is a picture being drawn: done closes with file set ("" when it could not be drawn).
type thumbDrawing struct {
	done chan struct{}
	file string
}

func (d *thumbDrawing) Wait(timeout time.Duration) (string, bool) {
	select {
	case <-d.done:
		return d.file, true
	case <-time.After(timeout):
		return "", false
	}
}

var thumbStates sync.Map

func (l *Library) thumbs() *thumbState {
	if s, ok := thumbStates.Load(l); ok {
		return s.(*thumbState)
	}
	s, _ := thumbStates.LoadOrStore(l, &thumbState{inflight: map[string]*thumbDrawing{}, warming: map[string]bool{}})
	return s.(*thumbState)
}

// thumbView is the size a board is drawn at: its own when the project says it, a phone for a page built for one, a
// desktop otherwise.
func thumbView(b Board) shotView {
	if b.W != nil && b.H != nil && *b.W != 0 && *b.H != 0 {
		clamp := func(side int) int { return int(math.Min(thumbMaxSide, math.Max(thumbMinSide, float64(side)))) }
		return shotView{clamp(*b.W), clamp(*b.H), false}
	}
	if thumbPhoneRe.MatchString(b.Path) {
		return phoneThumbView
	}
	return defaultThumbView
}

func thumbHash(boardPath string) string {
	sum := sha1.Sum([]byte(boardPath))
	return hex.EncodeToString(sum[:])[:16]
}

// thumbFileName: the board's path, then the version of its page.
func thumbFileName(boardPath, version string) string {
	return thumbHash(boardPath) + "-" + version + ".jpg"
}

// thumbVersion changes when the board's page changes; for a page that shows a bundle also when a file beside it does.
func (l *Library) thumbVersion(id, boardPath string) string {
	pathname := pageFileOf(boardPath)
	real := l.ResolveProjectFile(id, strings.Split(pathname, "/"))
	if real == "" {
		return ""
	}
	st, err := os.Stat(real)
	if err != nil {
		return ""
	}
	newest := mtimeMs(st)
	if !IsBoardFile(pathname) {
		names, _ := readNames(filepath.Dir(real))
		for _, n := range names {
			if b, err := os.Stat(filepath.Join(filepath.Dir(real), n)); err == nil && b.Mode().IsRegular() {
				newest = math.Max(newest, mtimeMs(b))
			}
		}
	}
	return strconv.FormatInt(round(newest), 36) + "-" + strconv.FormatInt(st.Size(), 36)
}

func (s *thumbState) takeSlot(key string, urgent bool) {
	s.mu.Lock()
	if s.active < thumbTabs {
		s.active++
		s.mu.Unlock()
		return
	}
	t := thumbTurn{key: key, start: make(chan struct{})}
	if urgent {
		s.queue = append([]thumbTurn{t}, s.queue...)
	} else {
		s.queue = append(s.queue, t)
	}
	s.mu.Unlock()
	<-t.start
}

// hurry puts a picture someone is looking at before the ones drawn ahead of time.
func (s *thumbState) hurry(key string) {
	for i, t := range s.queue {
		if t.key == key && i > 0 {
			s.queue = append([]thumbTurn{t}, append(s.queue[:i:i], s.queue[i+1:]...)...)
			return
		}
	}
}

func (s *thumbState) freeSlot() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) > 0 {
		next := s.queue[0]
		s.queue = s.queue[1:]
		close(next.start)
		return
	}
	s.active--
	if s.active == 0 {
		if s.idle != nil {
			s.idle.Stop()
		}
		s.idle = time.AfterFunc(thumbIdle, s.closeBrowser)
	}
}

func (s *thumbState) closeBrowser() {
	s.mu.Lock()
	b := s.browser
	s.browser = nil
	s.mu.Unlock()
	if b != nil {
		b.close()
	}
}

// browserFor is the drawing browser, started when there is none (or the last one exited).
func (l *Library) thumbBrowser(s *thumbState, bin string) (*browser, error) {
	for {
		s.mu.Lock()
		if s.idle != nil {
			s.idle.Stop()
			s.idle = nil
		}
		if b := s.browser; b != nil {
			select {
			case <-b.exited:
				s.browser = nil
			default:
				s.mu.Unlock()
				return b, nil
			}
		}
		if wait := s.starting; wait != nil {
			s.mu.Unlock()
			<-wait
			continue
		}
		s.starting = make(chan struct{})
		s.mu.Unlock()
		b, err := l.launchBrowser(bin)
		s.mu.Lock()
		if err == nil {
			s.browser = b
		}
		close(s.starting)
		s.starting = nil
		s.mu.Unlock()
		return b, err
	}
}

func (l *Library) drawThumb(id string, b Board, file string) bool {
	bin := l.findChrome()
	if bin == "" {
		return false
	}
	base, err := libraryBase(l)
	if err != nil {
		return false
	}
	s := l.thumbs()
	br, err := l.thumbBrowser(s, bin)
	if err != nil {
		return false
	}
	t, err := br.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		return false
	}
	targetID := t["targetId"]
	temp := file + "." + randomHex(4) + ".tmp"
	defer func() {
		_, _ = br.cdp.send("Target.closeTarget", map[string]any{"targetId": targetID}, "")
		_ = os.Remove(temp)
	}()
	a, err := br.cdp.send("Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true}, "")
	if err != nil {
		return false
	}
	session, _ := a["sessionId"].(string)
	for _, d := range []string{"Page", "Runtime", "Log", "Network", "DOM"} {
		if _, err := br.cdp.send(d+".enable", nil, session); err != nil {
			return false
		}
	}
	done := make(chan shotResult, 1)
	go func() { done <- shoot(br.cdp, session, base+BoardHref(id, b.Path), thumbView(b), temp, false) }()
	select {
	case res := <-done:
		// A page with errors still has a picture worth showing; only a drawing that produced no file is a failure.
		return res.Saved && os.Rename(temp, file) == nil
	case <-time.After(thumbRenderWait):
		go func() {
			<-done
			_ = os.Remove(temp)
		}()
		return false
	}
}

func (l *Library) renderThumb(id string, b Board, file, key string, urgent bool) string {
	s := l.thumbs()
	s.takeSlot(key, urgent)
	defer s.freeSlot()
	if l.drawThumb(id, b, file) {
		return file
	}
	return ""
}

func dropOldThumbs(dir, hash, keep string) {
	names, _ := readNames(dir)
	for _, n := range names {
		if strings.HasPrefix(n, hash+"-") && n != keep {
			_ = os.RemoveAll(filepath.Join(dir, n))
		}
	}
}

// earlierThumb is the newest picture of an earlier version of a board.
func earlierThumb(dir, hash string) string {
	best, at := "", 0.0
	names, _ := readNames(dir)
	for _, n := range names {
		if !strings.HasPrefix(n, hash+"-") || !strings.HasSuffix(n, ".jpg") {
			continue
		}
		file := filepath.Join(dir, n)
		if st, err := os.Stat(file); err == nil && st.Mode().IsRegular() && (best == "" || mtimeMs(st) > at) {
			best, at = file, mtimeMs(st)
		}
	}
	return best
}

// ThumbAnswer is what the gallery can be given for a board right now: the picture of the board as it is (Current),
// else that of an earlier version, else none, and the drawing of the current one when it is not there yet.
type ThumbAnswer struct {
	File    string
	Current bool
	Drawing *thumbDrawing
}

// BoardThumbNow answers without waiting; urgent is a picture a person is looking at. ok false: no such board.
func (l *Library) BoardThumbNow(id, boardPath string, urgent bool) (ThumbAnswer, bool) {
	if !IsLibraryID(id) {
		return ThumbAnswer{}, false
	}
	a := l.ReadArtifact(id)
	if a == nil {
		return ThumbAnswer{}, false
	}
	var board *Board
	for i := range a.Boards {
		if a.Boards[i].Path == boardPath {
			board = &a.Boards[i]
			break
		}
	}
	if board == nil {
		return ThumbAnswer{}, false
	}
	version := l.thumbVersion(id, board.Path)
	if version == "" {
		return ThumbAnswer{}, false
	}
	dir := filepath.Join(l.ArtifactDir(id), ThumbDirname)
	name := thumbFileName(board.Path, version)
	file := filepath.Join(dir, name)
	if _, err := os.Stat(file); err == nil {
		return ThumbAnswer{File: file, Current: true}, true
	}
	key := id + "\n" + board.Path + "\n" + version
	s := l.thumbs()
	s.mu.Lock()
	d := s.inflight[key]
	if d != nil {
		if urgent {
			s.hurry(key)
		}
		s.mu.Unlock()
	} else {
		s.mu.Unlock()
		_ = os.MkdirAll(dir, 0o777)
		s.mu.Lock()
		if d = s.inflight[key]; d == nil {
			d = &thumbDrawing{done: make(chan struct{})}
			s.inflight[key] = d
			b := *board
			go func() {
				f := l.renderThumb(id, b, file, key, urgent)
				if f != "" {
					dropOldThumbs(dir, thumbHash(b.Path), name)
				}
				s.mu.Lock()
				delete(s.inflight, key)
				s.mu.Unlock()
				d.file = f
				close(d.done)
			}()
		}
		s.mu.Unlock()
	}
	return ThumbAnswer{File: earlierThumb(dir, thumbHash(board.Path)), Drawing: d}, true
}

// WarmThumbs draws ahead the pictures a project still lacks, behind the ones being looked at.
func (l *Library) WarmThumbs(id string) {
	if !IsLibraryID(id) {
		return
	}
	s := l.thumbs()
	s.mu.Lock()
	if s.warming[id] {
		s.mu.Unlock()
		return
	}
	s.warming[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.warming, id)
		s.mu.Unlock()
	}()
	a := l.ReadArtifact(id)
	if a == nil {
		return
	}
	var drawings []*thumbDrawing
	for _, b := range a.Boards {
		if ans, ok := l.BoardThumbNow(id, b.Path, false); ok && ans.Drawing != nil {
			drawings = append(drawings, ans.Drawing)
		}
	}
	for _, d := range drawings {
		<-d.done
	}
}
