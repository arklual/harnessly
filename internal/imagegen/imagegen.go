// Package imagegen makes pictures with Codex: a job runs `codex exec`, whose built-in image tool draws them, and
// watches the folder Codex saves each picture to (~/.codex/generated_images/<thread>/), so every picture is known the
// moment it is drawn, not when the run ends. Codex's --json stream says nothing of pictures; the folder does.
//
// A job belongs to whoever asked: a chat (an agent's generate_images call), a Sinopia design. Its pictures are
// copied into the job's own folder (and into the asker's folder when one is named), where the page shows them from.
package imagegen

import (
	"bufio"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// MaxCount is the most pictures one job draws.
	MaxCount = 8
	// RunTimeout bounds one job.
	RunTimeout = 12 * time.Minute
	// watchEvery is how often the Codex folder is looked at.
	watchEvery = 400 * time.Millisecond
	// MaxRunning is how many jobs draw at once on a machine.
	MaxRunning = 4
	// keepJobs is how many finished jobs are remembered.
	keepJobs = 300
	// maxPrompt bounds a brief.
	maxPrompt = 8000
)

// State is where a job is.
type State string

const (
	Running State = "running"
	Done    State = "done"
	Failed  State = "failed"
)

// Shape is the frame asked for.
type Shape string

const (
	Square    Shape = "square"
	Landscape Shape = "landscape"
	Portrait  Shape = "portrait"
)

// Request is what to draw.
type Request struct {
	Prompt string `json:"prompt"`
	Count  int    `json:"count"`
	Shape  Shape  `json:"shape,omitempty"`
	// Transparent asks for a transparent background.
	Transparent bool `json:"transparent,omitempty"`
	// Conv is the chat that asked ("" for none); Owner says who else did (e.g. "design:<id>").
	Conv  string `json:"conv,omitempty"`
	Owner string `json:"owner,omitempty"`
	// CopyTo is a folder the pictures are also copied into ("" for none), as <Name>-<n>.png.
	CopyTo string `json:"copyTo,omitempty"`
	// SaveOK says, right before each copy, whether CopyTo (its links followed) may still receive the pictures; a
	// request with CopyTo and no SaveOK copies nothing.
	SaveOK func(real string) bool `json:"-"`
	Name   string                 `json:"name,omitempty"`
	// Source is a picture to change instead of drawing a new one ("" for a new one): see edit.go. Mode says how, Format
	// is the frame of ModeSize, Overlay a PNG with what the person drew on it. SourceOK says, when the job starts,
	// whether this source may be read; a request with a Source and no SourceOK is refused.
	Source   string            `json:"source,omitempty"`
	Mode     Mode              `json:"mode,omitempty"`
	Format   string            `json:"format,omitempty"`
	Overlay  []byte            `json:"overlay,omitempty"`
	SourceOK func(string) bool `json:"-"`
}

// Image is one picture drawn.
type Image struct {
	N    int    `json:"n"`
	Path string `json:"path"`
	// Copy is the picture in the asker's folder ("" when none was asked or the copy failed).
	Copy string `json:"copy,omitempty"`
}

// Snapshot is a job as it is now.
type Snapshot struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Count  int    `json:"count"`
	Shape  Shape  `json:"shape,omitempty"`
	Conv   string `json:"conv,omitempty"`
	Owner  string `json:"owner,omitempty"`
	// Source and Mode: the picture this job changes, and how; absent for a new picture.
	Source    string  `json:"source,omitempty"`
	Mode      Mode    `json:"mode,omitempty"`
	State     State   `json:"state"`
	Images    []Image `json:"images"`
	Error     string  `json:"error,omitempty"`
	StartedAt int64   `json:"startedAt"`
	EndedAt   int64   `json:"endedAt,omitempty"`
}

// Service runs and remembers the jobs.
type Service struct {
	// Dir is where the jobs' pictures are kept: <Dir>/<job id>/<n>.png.
	Dir string
	// CodexHome is Codex's folder (~/.codex).
	CodexHome string
	// Env is the environment `codex exec` runs with (the agents' sign-in included); nil for this process's.
	Env func() []string
	// Binary is the codex binary; "" for "codex".
	Binary string
	// Changed is told a job's new state; nil for nothing.
	Changed func(Snapshot)

	mu    sync.Mutex
	jobs  map[string]*job
	order []string
	// loaded: the jobs of earlier runs were read back (edit.go).
	loaded bool
}

type job struct {
	snap Snapshot
	done chan struct{}
}

// ErrBadRequest is a request that cannot be drawn; ErrBusy, one that waits for the jobs already drawing.
var (
	ErrBadRequest = errors.New("bad request")
	ErrBusy       = errors.New("busy")
)

// Normalize checks and fills a request.
func Normalize(r Request) (Request, error) {
	r.Prompt = strings.TrimSpace(r.Prompt)
	r, err := normalizeEdit(r)
	if err != nil {
		return r, err
	}
	if len(r.Prompt) > maxPrompt {
		return r, fmt.Errorf("%w: the prompt is longer than %d characters", ErrBadRequest, maxPrompt)
	}
	if r.Count <= 0 {
		r.Count = 1
	}
	if r.Count > MaxCount {
		return r, fmt.Errorf("%w: at most %d pictures at once", ErrBadRequest, MaxCount)
	}
	switch r.Shape {
	case "", Square, Landscape, Portrait:
	default:
		return r, fmt.Errorf("%w: shape is square, landscape or portrait", ErrBadRequest)
	}
	r.Name = safeName(r.Name)
	return r, nil
}

// Start begins a job and gives its first snapshot.
func (s *Service) Start(r Request) (Snapshot, error) {
	r, err := Normalize(r)
	if err != nil {
		return Snapshot{}, err
	}
	id := newID()
	j := &job{done: make(chan struct{}), snap: Snapshot{
		ID: id, Prompt: r.Prompt, Count: r.Count, Shape: r.Shape, Conv: r.Conv, Owner: r.Owner, Source: r.Source, Mode: r.Mode, State: Running,
		Images: []Image{}, StartedAt: time.Now().UnixMilli(),
	}}
	s.mu.Lock()
	s.loadJobsLocked()
	running := 0
	for _, other := range s.jobs {
		if other.snap.State == Running {
			running++
		}
	}
	if running >= MaxRunning {
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("%w: %d jobs are drawing already, wait for one to end", ErrBusy, running)
	}
	s.jobs[id] = j
	s.order = append(s.order, id)
	s.forgetLocked()
	snap := cloneSnap(j.snap)
	s.mu.Unlock()
	s.tell(snap)
	go s.run(j, r)
	return snap, nil
}

// Get is a job's snapshot.
func (s *Service) Get(id string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadJobsLocked()
	j, ok := s.jobs[id]
	if !ok {
		return Snapshot{}, false
	}
	return cloneSnap(j.snap), true
}

// List gives the jobs of a chat or an owner (either may be ""), oldest first.
func (s *Service) List(conv, owner string) []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadJobsLocked()
	out := []Snapshot{}
	for _, id := range s.order {
		j := s.jobs[id]
		if (conv != "" && j.snap.Conv != conv) || (owner != "" && j.snap.Owner != owner) {
			continue
		}
		out = append(out, cloneSnap(j.snap))
	}
	return out
}

// Wait waits for a job to end, or for ctx; it gives the job as it is then.
func (s *Service) Wait(ctx context.Context, id string) (Snapshot, bool) {
	s.mu.Lock()
	s.loadJobsLocked()
	j, ok := s.jobs[id]
	s.mu.Unlock()
	if !ok {
		return Snapshot{}, false
	}
	select {
	case <-j.done:
	case <-ctx.Done():
	}
	return s.Get(id)
}

// ImagePath is the file of a job's n-th picture, when there is one.
func (s *Service) ImagePath(id string, n int) (string, bool) {
	snap, ok := s.Get(id)
	if !ok {
		return "", false
	}
	for _, im := range snap.Images {
		if im.N == n {
			return im.Path, true
		}
	}
	return "", false
}

func (s *Service) forgetLocked() {
	for len(s.order) > keepJobs {
		id := s.order[0]
		if j := s.jobs[id]; j != nil && j.snap.State == Running {
			break
		}
		s.order = s.order[1:]
		delete(s.jobs, id)
	}
}

func (s *Service) update(j *job, f func(*Snapshot)) {
	s.mu.Lock()
	f(&j.snap)
	snap := cloneSnap(j.snap)
	s.mu.Unlock()
	s.tell(snap)
}

func (s *Service) tell(snap Snapshot) {
	if s.Changed != nil {
		s.Changed(snap)
	}
}

func sortByStart(jobs []Snapshot) {
	slices.SortStableFunc(jobs, func(a, b Snapshot) int { return cmp.Compare(a.StartedAt, b.StartedAt) })
}

func cloneSnap(s Snapshot) Snapshot {
	s.Images = slices.Clone(s.Images)
	if s.Images == nil {
		s.Images = []Image{}
	}
	return s
}

// Brief is what Codex is told for a request.
func Brief(r Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Use your built-in image generation tool to draw exactly %d picture", r.Count)
	if r.Count > 1 {
		b.WriteString("s, each a distinct variant of the brief (different composition or take), started in parallel when you can")
	}
	b.WriteString(". Only call the image tool: do not run commands, read or write files, or copy the results anywhere — the " +
		"pictures are collected from where the tool saves them. Do not ask questions; if the brief is vague, choose sensibly.")
	switch r.Shape {
	case Landscape:
		b.WriteString(" Frame: landscape (wide).")
	case Portrait:
		b.WriteString(" Frame: portrait (tall).")
	case Square:
		b.WriteString(" Frame: square.")
	}
	if r.Transparent {
		b.WriteString(" Background: transparent.")
	}
	b.WriteString(" When done, answer with one short line.\n\nBrief:\n")
	b.WriteString(r.Prompt)
	return b.String()
}

func (s *Service) run(j *job, r Request) {
	defer close(j.done)
	defer func() {
		snap, ok := s.Get(j.snap.ID)
		if ok {
			s.saveJob(snap)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), RunTimeout)
	defer cancel()
	out := filepath.Join(s.Dir, j.snap.ID)
	fail := func(msg string) {
		s.update(j, func(sn *Snapshot) {
			sn.State, sn.EndedAt = Failed, time.Now().UnixMilli()
			if len(sn.Images) > 0 {
				sn.State = Done
				if len(sn.Images) < sn.Count {
					sn.Error = fmt.Sprintf("only %d of %d pictures were drawn: %s", len(sn.Images), sn.Count, msg)
				}
				return
			}
			sn.Error = msg
		})
	}
	if err := os.MkdirAll(out, 0o700); err != nil {
		fail("could not make the pictures' folder: " + err.Error())
		return
	}
	work, err := os.MkdirTemp("", "hl-imagegen-")
	if err != nil {
		fail("could not make a folder to run in: " + err.Error())
		return
	}
	defer os.RemoveAll(work)
	bin := s.Binary
	if bin == "" {
		bin = "codex"
	}
	args := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "read-only",
		"-c", `approval_policy="never"`, "-c", "mcp_servers={}"}
	brief := Brief(r)
	if r.Source != "" {
		attached, err := prepareSource(r, work)
		if err != nil {
			fail(err.Error())
			return
		}
		args = append(args, "-i", attached, "--")
		brief = editBrief(r)
	}
	cmd := exec.CommandContext(ctx, bin, append(args, brief)...)
	cmd.Dir = work
	if s.Env != nil {
		cmd.Env = s.Env()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fail(err.Error())
		return
	}
	var stderr tailBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		fail("codex did not start: " + err.Error())
		return
	}
	threads := make(chan string, 1)
	var said lastSaid
	go readEvents(stdout, threads, &said)

	var thread string
	seen := map[string]bool{}
	collect := func() {
		if thread == "" {
			return
		}
		for _, f := range newPictures(filepath.Join(s.CodexHome, "generated_images", thread), seen) {
			seen[f] = true
			n := len(seen)
			dst := filepath.Join(out, fmt.Sprintf("%d%s", n, filepath.Ext(f)))
			if err := copyFile(f, dst); err != nil {
				continue
			}
			if wrong := finishEdit(r, dst); wrong != "" {
				os.Remove(dst)
				s.update(j, func(sn *Snapshot) { sn.Error = wrong })
				continue
			}
			f = dst
			im := Image{N: n, Path: dst}
			if r.CopyTo != "" {
				name := r.Name
				if name == "" {
					name = "image-" + j.snap.ID[:6]
				}
				c := filepath.Join(r.CopyTo, fmt.Sprintf("%s-%d%s", name, n, filepath.Ext(f)))
				if dir, ok := saveDir(r.CopyTo, r.SaveOK); ok && copyFile(f, filepath.Join(dir, filepath.Base(c))) == nil {
					im.Copy = filepath.Join(dir, filepath.Base(c))
				}
			}
			s.update(j, func(sn *Snapshot) { sn.Images = append(sn.Images, im) })
		}
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	tick := time.NewTicker(watchEvery)
	defer tick.Stop()
	for {
		select {
		case t := <-threads:
			thread = t
		case <-tick.C:
			collect()
		case err := <-exited:
			if thread == "" {
				select {
				case thread = <-threads:
				case <-time.After(time.Second):
				}
			}
			time.Sleep(watchEvery)
			collect()
			snap, _ := s.Get(j.snap.ID)
			switch {
			case ctx.Err() == context.DeadlineExceeded:
				fail("the pictures took longer than " + RunTimeout.String())
			case len(snap.Images) == 0 && snap.Error != "":
				fail(snap.Error)
			case len(snap.Images) == 0:
				msg := strings.TrimSpace(said.get())
				if msg == "" {
					msg = strings.TrimSpace(stderr.String())
				}
				if msg == "" && err != nil {
					msg = err.Error()
				}
				if msg == "" {
					msg = "Codex drew no picture"
				}
				fail("Codex drew no picture: " + clip(msg, 600))
			case len(snap.Images) < snap.Count:
				fail("Codex stopped early")
			default:
				s.update(j, func(sn *Snapshot) { sn.State, sn.EndedAt = Done, time.Now().UnixMilli() })
			}
			return
		}
	}
}

// readEvents reads Codex's --json stream: the thread's id (the folder its pictures go to) and what it last said.
func readEvents(r io.Reader, threads chan<- string, said *lastSaid) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Message  string `json:"message"`
			Item     struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Message string `json:"message"`
			} `json:"item"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "thread.started" && ev.ThreadID != "":
			select {
			case threads <- ev.ThreadID:
			default:
			}
		case ev.Type == "item.completed" && ev.Item.Type == "agent_message":
			said.set(ev.Item.Text)
		case ev.Type == "turn.failed" || ev.Type == "error":
			msg := ev.Error.Message
			if msg == "" {
				msg = ev.Message
			}
			if msg != "" {
				said.set(msg)
			}
		}
	}
	_, _ = io.Copy(io.Discard, r)
}

// newPictures are the finished pictures in dir not seen yet, oldest first. A file still being written (its size
// still changing between two looks) waits for the next look.
func newPictures(dir string, seen map[string]bool) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type pic struct {
		path string
		mod  time.Time
	}
	var out []pic
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() || seen[p] || !isPicture(e.Name()) {
			continue
		}
		st, err := e.Info()
		if err != nil || st.Size() == 0 || time.Since(st.ModTime()) < 300*time.Millisecond {
			continue
		}
		out = append(out, pic{p, st.ModTime()})
	}
	slices.SortFunc(out, func(a, b pic) int { return a.mod.Compare(b.mod) })
	paths := make([]string, len(out))
	for i, p := range out {
		paths[i] = p.path
	}
	return paths
}

func isPicture(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}

// saveDir makes the CopyTo folder and gives it with its links followed, when it may still receive the pictures.
func saveDir(dir string, ok func(string) bool) (string, bool) {
	if ok == nil {
		return "", false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	if !ok(real) {
		return "", false
	}
	return real, true
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// A fresh temp file of a random name (O_EXCL) in the target folder, renamed over the target: a link planted
	// at either name is never written through.
	out, err := os.CreateTemp(filepath.Dir(dst), ".hl-image-*.part")
	if err != nil {
		return err
	}
	tmp := out.Name()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Chmod(0o644); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// safeName keeps a file name's letters, digits, dashes and underscores.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

type lastSaid struct {
	mu sync.Mutex
	s  string
}

func (l *lastSaid) set(s string) { l.mu.Lock(); l.s = s; l.mu.Unlock() }
func (l *lastSaid) get() string  { l.mu.Lock(); defer l.mu.Unlock(); return l.s }

// tailBuffer keeps the last few kilobytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	// stderr is mostly Codex's own log lines; keep the ones that read like a failure.
	var keep []string
	for _, line := range strings.Split(string(t.buf), "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "rmcp::") || strings.Contains(l, "hook") {
			continue
		}
		if strings.Contains(l, "error") || strings.Contains(l, "denied") || strings.Contains(l, "limit") || strings.Contains(l, "login") {
			keep = append(keep, strings.TrimSpace(line))
		}
	}
	return strings.Join(keep, "\n")
}
