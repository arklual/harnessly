// Package docshare turns a file of the workspace into a document of the cluster's catalog and keeps the two
// bound: every save of a linked file, by the person in the editor or by an agent, becomes a new version of the document,
// no more often than once in Debounce. The file stays the working copy; the document is private to its owner until the
// person opens it (the access dialog, or the artifact CLI). The way to the cluster is the machine agent's
// loopback route /docs/publish (internal/agent/docrelay.go).
package docshare

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/locale"
)

const (
	// FileName is the registry of sources in the data directory.
	FileName = "doc-shares.json"
	// MaxBytes is the biggest document: what the editor opens.
	MaxBytes = 1_000_000

	defaultDebounce = 30 * time.Second
	defaultPoll     = 5 * time.Second
	callTimeout     = 90 * time.Second
)

// Source is a file and the document made of it.
type Source struct {
	Path         string    `json:"path"`
	Slug         string    `json:"slug"`
	Title        string    `json:"title"`
	Conversation string    `json:"conversation,omitempty"`
	Hash         string    `json:"hash"`
	Version      int       `json:"version"`
	Linked       bool      `json:"linked"`
	PublishedAt  time.Time `json:"publishedAt"`
	// RefusedHash is the content a republish was refused for (a secret), so it is not tried again until the file changes.
	RefusedHash string `json:"refusedHash,omitempty"`
	// RefusedLine is the line of the secret in RefusedHash.
	RefusedLine int `json:"refusedLine,omitempty"`
}

// State is what the page shows for a file.
type State struct {
	Shared  bool   `json:"shared"`
	Slug    string `json:"slug,omitempty"`
	Linked  bool   `json:"linked"`
	Version int    `json:"version,omitempty"`
	// Pending: the file has changed since the document was made; the next version waits for the debounce.
	Pending bool `json:"pending,omitempty"`
	// RefusedLine: the newest content holds a secret on this line and is not published.
	RefusedLine int `json:"refusedLine,omitempty"`
}

// Failure is a refusal with the status the page gets.
type Failure struct {
	Status int
	Text   string
	// Line is the first line of a secret, when that is the reason.
	Line int
	Kind string
}

func (f *Failure) Error() string { return f.Text }

// Config is the service's setting.
type Config struct {
	DataDir          string
	AgentTokenHeader string
	HTTP             *http.Client
	Now              func() time.Time
	Debounce         time.Duration
	Poll             time.Duration
	// Call posts to the machine agent; the default is agent.CallLocal. Tests put their own.
	Call func(route string, body any) (int, []byte, error)
}

// Service keeps the registry and watches the linked files.
type Service struct {
	cfg     Config
	mu      sync.Mutex
	sources map[string]*Source
	loaded  bool
	// busy: paths being published now, so the watcher and a click do not make two versions at once.
	busy map[string]bool
}

// New prepares a service.
func New(cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = defaultDebounce
	}
	if cfg.Poll <= 0 {
		cfg.Poll = defaultPoll
	}
	s := &Service{cfg: cfg, sources: map[string]*Source{}, busy: map[string]bool{}}
	if s.cfg.Call == nil {
		s.cfg.Call = func(route string, body any) (int, []byte, error) {
			return agent.CallLocal(cfg.HTTP, cfg.DataDir, cfg.AgentTokenHeader, route, body, callTimeout)
		}
	}
	return s
}

var formats = map[string]string{".md": "md", ".markdown": "md", ".html": "html", ".htm": "html"}

// FormatOf is "md" or "html" for a file name the first version takes, else "".
func FormatOf(path string) string { return formats[strings.ToLower(filepath.Ext(path))] }

func titleOf(path, content, format string) string {
	if format == "md" {
		for _, line := range strings.Split(content, "\n") {
			if t, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok && strings.TrimSpace(t) != "" {
				return clip(strings.TrimSpace(t), 300)
			}
		}
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	if name = strings.TrimSpace(name); name != "" {
		r := []rune(name)
		return clip(strings.ToUpper(string(r[:1]))+string(r[1:]), 300)
	}
	return "Документ"
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// readFile is the file's text, or a refusal in the words of the editor.
func readFile(path string) (string, *Failure) {
	gone := &Failure{Status: 404, Text: locale.Pick("The file no longer exists.", "Файл больше не существует.")}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return "", gone
	}
	f, err := os.Open(path)
	if err != nil {
		return "", gone
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", &Failure{Status: 415, Text: locale.Pick("Only regular text files can be shared.", "Поделиться можно только обычным текстовым файлом.")}
	}
	if st.Size() > MaxBytes {
		return "", &Failure{Status: 413, Text: locale.Pick("A document is at most 1 MB.", "Документ — не больше 1 МБ.")}
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(f); err != nil {
		return "", &Failure{Status: 500, Text: locale.Pick("Could not read the file.", "Не удалось прочитать файл.")}
	}
	if !utf8.Valid(buf.Bytes()) || bytes.IndexByte(buf.Bytes(), 0) >= 0 {
		return "", &Failure{Status: 415, Text: locale.Pick("This file is not UTF-8 text.", "Это не текст в UTF-8.")}
	}
	return buf.String(), nil
}

// Request is "Поделиться" for a file: the absolute, resolved path of a file already checked to lie in the person's project.
type Request struct {
	Path         string
	Conversation string
}

// Publish makes the document of a file, private. A file that is linked already answers with its document.
func (s *Service) Publish(req Request) (State, *Failure) {
	format := FormatOf(req.Path)
	if format == "" {
		return State{}, &Failure{Status: 415, Text: locale.Pick("Markdown and HTML files can be shared for now.", "Пока можно делиться файлами Markdown и HTML.")}
	}
	s.mu.Lock()
	s.load()
	if cur := s.sources[req.Path]; cur != nil && cur.Linked {
		st := stateOf(cur, false)
		s.mu.Unlock()
		return st, nil
	}
	if s.busy[req.Path] {
		s.mu.Unlock()
		return State{}, &Failure{Status: 409, Text: locale.Pick("The file is being published.", "Файл уже публикуется.")}
	}
	s.busy[req.Path] = true
	prev := s.sources[req.Path]
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.busy, req.Path)
		s.mu.Unlock()
	}()

	content, fail := readFile(req.Path)
	if fail != nil {
		return State{}, fail
	}
	title := titleOf(req.Path, content, format)
	slug := ""
	if prev != nil {
		// Unlinked before: sharing again continues the same document.
		slug = prev.Slug
	}
	version, newSlug, fail := s.send(title, format, content, slug, req.Path, req.Conversation, "")
	if fail != nil {
		return State{}, fail
	}
	src := &Source{Path: req.Path, Slug: newSlug, Title: title, Conversation: req.Conversation, Hash: hashOf(content), Version: version, Linked: true, PublishedAt: s.cfg.Now()}
	s.mu.Lock()
	s.sources[req.Path] = src
	st := stateOf(src, false)
	s.save()
	s.mu.Unlock()
	return st, nil
}

// send posts a document to the agent and returns its version and slug.
func (s *Service) send(title, format, content, slug, path, conversation, note string) (int, string, *Failure) {
	body := map[string]any{
		"title": title, "format": format, "content": content, "visibility": "private",
		"source": map[string]string{"path": path, "conversation": conversation, "cwd": filepath.Dir(path)},
	}
	if slug != "" {
		body["slug"] = slug
	}
	if note != "" {
		body["note"] = note
	}
	status, raw, err := s.cfg.Call("/docs/publish", body)
	if err != nil {
		if errors.Is(err, agent.ErrNoAgent) {
			return 0, "", &Failure{Status: 503, Text: locale.Pick("This machine is not connected to the platform, so documents cannot be shared from it.", "Эта машина не подключена к платформе: поделиться отсюда нельзя.")}
		}
		return 0, "", &Failure{Status: 502, Text: locale.Pick("The platform did not answer.", "Платформа не ответила.")}
	}
	refused := locale.Pick("The platform refused the document.", "Платформа не приняла документ.")
	if status == 422 {
		var secret struct {
			Line int    `json:"line"`
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(raw, &secret)
		line := strconv.Itoa(secret.Line)
		return 0, "", &Failure{Status: 422, Line: secret.Line, Kind: secret.Kind, Text: locale.Pick(
			"The file holds a secret on line "+line+"; remove it to share the file.",
			"В файле секрет в строке "+line+": уберите его, чтобы поделиться.")}
	}
	if status != 200 {
		return 0, "", &Failure{Status: 502, Text: errorText(raw, refused)}
	}
	var answer struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(raw, &answer) != nil {
		return 0, "", &Failure{Status: 502, Text: locale.Pick("The platform answered unexpectedly.", "Платформа ответила неожиданно.")}
	}
	if answer.Status >= 400 {
		return 0, "", &Failure{Status: statusFor(answer.Status), Text: errorText(answer.Body, refused)}
	}
	var made struct {
		Slug    string  `json:"slug"`
		Version float64 `json:"version"`
	}
	if json.Unmarshal(answer.Body, &made) != nil || made.Slug == "" {
		return 0, "", &Failure{Status: 502, Text: locale.Pick("The platform answered unexpectedly.", "Платформа ответила неожиданно.")}
	}
	return int(made.Version), made.Slug, nil
}

func statusFor(upstream int) int {
	switch upstream {
	case 400, 403, 404, 409, 413, 429:
		return upstream
	}
	return 502
}

func errorText(raw []byte, fallback string) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && strings.TrimSpace(e.Error) != "" {
		return clip(e.Error, 300)
	}
	return fallback
}

func stateOf(src *Source, pending bool) State {
	return State{Shared: true, Slug: src.Slug, Linked: src.Linked, Version: src.Version, Pending: pending, RefusedLine: src.RefusedLine}
}

// State is the file's state; a file nobody shared answers {Shared: false}.
func (s *Service) State(path string) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	src := s.sources[path]
	if src == nil {
		return State{}
	}
	pending := false
	if src.Linked {
		if content, fail := readFile(path); fail == nil {
			pending = hashOf(content) != src.Hash
		}
	}
	return stateOf(src, pending)
}

// Unlink freezes the document: later saves of the file are not published. The document stays as it is.
func (s *Service) Unlink(path string) (State, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	src := s.sources[path]
	if src == nil {
		return State{}, false
	}
	src.Linked = false
	src.RefusedHash, src.RefusedLine = "", 0
	s.save()
	return stateOf(src, false), true
}

// Run watches the linked files until ctx ends.
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(s.cfg.Poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.Tick()
		}
	}
}

// Tick looks at every linked file once: a changed one, with its last version older than the debounce, becomes a new
// version.
func (s *Service) Tick() {
	s.mu.Lock()
	s.load()
	var due []Source
	for _, src := range s.sources {
		if src.Linked && !s.busy[src.Path] && s.cfg.Now().Sub(src.PublishedAt) >= s.cfg.Debounce {
			due = append(due, *src)
		}
	}
	s.mu.Unlock()
	sort.Slice(due, func(i, j int) bool { return due[i].Path < due[j].Path })
	for _, src := range due {
		s.republish(src)
	}
}

func (s *Service) republish(src Source) {
	content, fail := readFile(src.Path)
	if fail != nil {
		return // the file is gone or unreadable for now: the document keeps its last version
	}
	h := hashOf(content)
	if h == src.Hash || h == src.RefusedHash {
		return
	}
	s.mu.Lock()
	if s.busy[src.Path] {
		s.mu.Unlock()
		return
	}
	s.busy[src.Path] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.busy, src.Path)
		s.mu.Unlock()
	}()
	format := FormatOf(src.Path)
	version, _, fail := s.send(titleOf(src.Path, content, format), format, content, src.Slug, src.Path, src.Conversation, locale.Pick("File saved", "Файл сохранён"))
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.sources[src.Path]
	if cur == nil || !cur.Linked {
		return
	}
	if fail != nil {
		if fail.Status == 422 {
			cur.RefusedHash, cur.RefusedLine = h, fail.Line
			s.save()
		}
		// Any other failure is tried again at the next tick.
		return
	}
	cur.Hash, cur.Version, cur.PublishedAt = h, version, s.cfg.Now()
	cur.RefusedHash, cur.RefusedLine = "", 0
	s.save()
}

func (s *Service) file() string { return filepath.Join(s.cfg.DataDir, FileName) }

// load reads the registry once; the caller holds the lock.
func (s *Service) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	raw, err := os.ReadFile(s.file())
	if err != nil {
		return
	}
	var list []*Source
	if json.Unmarshal(raw, &list) != nil {
		return
	}
	for _, src := range list {
		if src != nil && src.Path != "" && src.Slug != "" {
			s.sources[src.Path] = src
		}
	}
}

// save writes the registry whole and atomically; the caller holds the lock.
func (s *Service) save() {
	list := make([]*Source, 0, len(s.sources))
	for _, src := range s.sources {
		list = append(list, src)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	tmp := s.file() + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) != nil {
		return
	}
	if os.Rename(tmp, s.file()) != nil {
		_ = os.Remove(tmp)
	}
}
