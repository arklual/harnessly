// Package memsync keeps a person's Claude Code memory the same on all of the person's machines (docs/hub/memory-sync.md).
//
// Only the memory notes sync: <projects>/<entry>/memory/*.md, nothing else of the project. A project is known on every
// machine by the same id, its git remote (the folder differs from machine to machine). A folder without a remote syncs
// only when its person asks for it: by a key file in the folder (KeyFile) or, with LocalFolders on, by the folder's path. The hub keeps the notes of the machine's person; which person that is, the hub decides, not this package.
//
// A sync is pull, then push, by the version of each note the machine saw last (kept in .harnessly-sync.json beside the
// notes). A note changed on both sides is a conflict: the local note stays as it is and the other version lands beside
// it as <name>.conflict-<version>.md, so both are kept. A failure leaves the local notes as they are.
package memsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Client is the way to the person's memory in the hub: the agent of this machine (HTTPClient), or any service that
// speaks the same two calls (docs/hub/memory-sync.md).
type Client interface {
	Pull(ctx context.Context, projects []string) ([]hubproto.MemoryNote, error)
	Put(ctx context.Context, put hubproto.MemoryPut) (hubproto.MemoryPutResult, error)
}

// HTTPClient calls POST <URL>/pull and <URL>/put, signed with the runtime's secret for the agent's memory relay.
type HTTPClient struct {
	URL    string
	Secret []byte
	HTTP   *http.Client
}

func (c *HTTPClient) do(ctx context.Context, path string, body, out any) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agent.SignPayload(c.Secret, agent.MemoryRelayAudience, map[string]any{}, time.Minute))
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("the memory relay did not answer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return errors.New(e.Error)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *HTTPClient) Pull(ctx context.Context, projects []string) ([]hubproto.MemoryNote, error) {
	var out hubproto.MemoryNotes
	err := c.do(ctx, "/pull", hubproto.MemoryPull{Projects: projects}, &out)
	return out.Notes, err
}

func (c *HTTPClient) Put(ctx context.Context, put hubproto.MemoryPut) (hubproto.MemoryPutResult, error) {
	var out hubproto.MemoryPutResult
	err := c.do(ctx, "/put", put, &out)
	return out, err
}

// StateFile is the file beside the notes that keeps the project id and the version of each note seen last.
const StateFile = ".harnessly-sync.json"

// TrashDir keeps a copy of each note a sync removed because another machine deleted it.
const TrashDir = ".harnessly-sync-trash"

type state struct {
	Project string            `json:"project"`
	Seen    map[string]string `json:"seen"`
}

// Project is a memory folder and the id it syncs under.
type Project struct {
	ID  string `json:"id"`
	Dir string `json:"dir"`
}

// Conflict is a note changed on this machine and elsewhere: Copy is the file the other version was written to.
type Conflict struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	Copy    string `json:"copy"`
	Machine string `json:"machine,omitempty"`
}

// Report is what a sync did.
type Report struct {
	At        time.Time  `json:"at"`
	Projects  int        `json:"projects"`
	Pulled    []string   `json:"pulled"`
	Pushed    []string   `json:"pushed"`
	Deleted   []string   `json:"deleted"`
	Conflicts []Conflict `json:"conflicts"`
	// Errors are the failures; the local notes they touch are as they were.
	Errors []string `json:"errors"`
}

func newReport(now time.Time) Report {
	return Report{At: now, Pulled: []string{}, Pushed: []string{}, Deleted: []string{}, Conflicts: []Conflict{}, Errors: []string{}}
}

// EntryName is Claude Code's folder name for a project in cwd: every character but a letter or a digit is a dash.
func EntryName(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

var scpRemote = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// NormalizeRemote is a git remote as a project id: host and path, lower case, without the user, the scheme, the port
// and .git. git@example.com:acme/app.git and https://example.com/acme/app are both git:example.com/acme/app.
func NormalizeRemote(remote string) string {
	r := strings.TrimSpace(remote)
	if r == "" {
		return ""
	}
	var host, path string
	if i := strings.Index(r, "://"); i >= 0 {
		rest := r[i+3:]
		if at := strings.LastIndex(strings.SplitN(rest, "/", 2)[0], "@"); at >= 0 {
			rest = rest[at+1:]
		}
		host, path, _ = strings.Cut(rest, "/")
		if h, _, ok := strings.Cut(host, ":"); ok {
			host = h
		}
	} else if m := scpRemote.FindStringSubmatch(r); m != nil {
		host, path = m[1], m[2]
	} else {
		return ""
	}
	path = strings.Trim(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	if host == "" || path == "" {
		return ""
	}
	return "git:" + strings.ToLower(host+"/"+path)
}

// KeyFile is the file in a folder whose first line names the folder's project, whatever its remote or path:
// `dir:<key>` is the id. The same key in folders of different machines is one project.
const KeyFile = ".harnessly-memory-project"

var keyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// FolderKey is the project id a folder names for itself in its KeyFile ("dir:<key>"), "" without a valid one.
func FolderKey(cwd string) string {
	raw, err := os.ReadFile(filepath.Join(cwd, KeyFile))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	if k := strings.TrimSpace(line); keyRe.MatchString(k) {
		return "dir:" + k
	}
	return ""
}

// PathID is the id of a folder without a remote, by its absolute path ("dir:<path>"); "" for a path that is no id (a
// space in it) or for a relative one. Machines that keep the folder at the same path share the project.
func PathID(cwd string) string {
	if !filepath.IsAbs(cwd) {
		return ""
	}
	id := "dir:" + filepath.ToSlash(filepath.Clean(cwd))
	if !hubproto.MemoryProjectOK(id) {
		return ""
	}
	return id
}

// FolderID is the id of the project in cwd: its KeyFile, else its git remote; with localFolders a folder that has
// neither is known by its path. "" when the folder has no id and does not sync.
func FolderID(ctx context.Context, cwd string, localFolders bool) string {
	if id := FolderKey(cwd); id != "" {
		return id
	}
	if id := ProjectID(ctx, cwd); id != "" {
		return id
	}
	if localFolders {
		return PathID(cwd)
	}
	return ""
}

// ProjectID is the id of the project in cwd: its git remote (origin, else the first one); "" without a remote.
func ProjectID(ctx context.Context, cwd string) string {
	run := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if u := run("remote", "get-url", "origin"); u != "" {
		return NormalizeRemote(u)
	}
	if names := strings.Fields(run("remote")); len(names) > 0 {
		return NormalizeRemote(run("remote", "get-url", names[0]))
	}
	return ""
}

func readState(dir string) state {
	var st state
	if raw, err := os.ReadFile(filepath.Join(dir, StateFile)); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	if st.Seen == nil {
		st.Seen = map[string]string{}
	}
	return st
}

func writeState(dir string, st state) error {
	raw, _ := json.MarshalIndent(st, "", "  ")
	return writeFile(filepath.Join(dir, StateFile), raw)
}

// writeFile replaces path at once (a temporary file renamed over it): a reader never sees half a note.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sync-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// localNotes are the notes of a memory folder, by name, with their text.
func localNotes(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if !e.Type().IsRegular() || !hubproto.MemoryNameOK(e.Name()) || IsConflictCopy(e.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || len(raw) > hubproto.MemoryMaxNote {
			continue
		}
		out[e.Name()] = string(raw)
	}
	return out, nil
}

// conflictName is the file the other version of name goes to.
func conflictName(name, version string) string {
	v := version
	if len(v) > 8 {
		v = v[:8]
	}
	return strings.TrimSuffix(name, ".md") + ".conflict-" + v + ".md"
}

// IsConflictCopy says whether name is a copy a sync made of the other version of a note.
func IsConflictCopy(name string) bool { return strings.Contains(name, ".conflict-") }

// Syncer syncs memory folders through a Client; one sync at a time.
type Syncer struct {
	Client Client
	Now    func() time.Time
	mu     sync.Mutex
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Sync pulls and pushes the notes of the projects. A project whose pull fails is left as it is.
func (s *Syncer) Sync(ctx context.Context, projects []Project) Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep := newReport(s.now())
	byID := map[string][]Project{}
	ids := []string{}
	for _, p := range projects {
		if p.ID == "" || !hubproto.MemoryProjectOK(p.ID) {
			continue
		}
		if _, ok := byID[p.ID]; !ok {
			ids = append(ids, p.ID)
		}
		byID[p.ID] = append(byID[p.ID], p)
	}
	sort.Strings(ids)
	if len(ids) > hubproto.MemoryMaxProjects {
		ids = ids[:hubproto.MemoryMaxProjects]
	}
	rep.Projects = len(ids)
	if len(ids) == 0 {
		return rep
	}
	notes, err := s.Client.Pull(ctx, ids)
	if err != nil {
		rep.Errors = append(rep.Errors, "pull: "+err.Error())
		return rep
	}
	remote := map[string]map[string]hubproto.MemoryNote{}
	for _, n := range notes {
		if remote[n.Project] == nil {
			remote[n.Project] = map[string]hubproto.MemoryNote{}
		}
		remote[n.Project][n.Name] = n
	}
	for _, id := range ids {
		for _, p := range byID[id] {
			s.syncDir(ctx, p, remote[id], &rep)
		}
	}
	return rep
}

func (s *Syncer) syncDir(ctx context.Context, p Project, remote map[string]hubproto.MemoryNote, rep *Report) {
	label := func(name string) string { return p.ID + " " + name }
	fail := func(name string, err error) { rep.Errors = append(rep.Errors, label(name)+": "+err.Error()) }
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		fail("", err)
		return
	}
	local, err := localNotes(p.Dir)
	if err != nil {
		fail("", err)
		return
	}
	st := readState(p.Dir)
	if st.Project != "" && st.Project != p.ID {
		// The folder now belongs to another project (a new remote): what it saw of the old one says nothing.
		st.Seen = map[string]string{}
	}
	st.Project = p.ID
	names := map[string]bool{}
	for n := range local {
		names[n] = true
	}
	for n := range remote {
		if !IsConflictCopy(n) {
			names[n] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	conflict := func(name string, other hubproto.MemoryNote) {
		cp := conflictName(name, other.Version)
		path := filepath.Join(p.Dir, cp)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if err := writeFile(path, []byte(other.Body)); err != nil {
				fail(name, err)
				return
			}
		}
		st.Seen[name] = other.Version
		rep.Conflicts = append(rep.Conflicts, Conflict{Project: p.ID, Name: name, Copy: cp, Machine: other.Machine})
	}
	// pushHeld stops the pushes after the first one that failed: the hub is away or asked for a pause, and a note per
	// request would only hammer it. What is left stays unsynced and goes with the next sync.
	pushHeld := false
	push := func(name string, put hubproto.MemoryPut) {
		if pushHeld {
			return
		}
		put.Project, put.Name = p.ID, name
		res, err := s.Client.Put(ctx, put)
		switch {
		case err != nil:
			pushHeld = true
			fail(name, err)
		case res.Conflict != nil:
			if res.Conflict.Deleted {
				// Deleted elsewhere while edited here: the edit wins over the delete next time.
				st.Seen[name] = res.Conflict.Version
				return
			}
			conflict(name, *res.Conflict)
		case res.Note != nil:
			st.Seen[name] = res.Note.Version
			if put.Delete {
				rep.Deleted = append(rep.Deleted, label(name)+" (hub)")
			} else {
				rep.Pushed = append(rep.Pushed, label(name))
			}
		}
	}
	for _, name := range sorted {
		if ctx.Err() != nil {
			fail(name, ctx.Err())
			break
		}
		body, here := local[name]
		h := ""
		if here {
			h = hubproto.MemoryVersion(body)
		}
		base := st.Seen[name]
		r, there := remote[name]
		switch {
		case !here && !there:
			delete(st.Seen, name)
		case !here && r.Deleted:
			st.Seen[name] = r.Version
		case !here:
			if base != "" && base == r.Version {
				// Deleted here since the last sync: delete it in the hub too.
				push(name, hubproto.MemoryPut{Delete: true, Base: base})
				continue
			}
			if err := writeFile(filepath.Join(p.Dir, name), []byte(r.Body)); err != nil {
				fail(name, err)
				continue
			}
			st.Seen[name] = r.Version
			rep.Pulled = append(rep.Pulled, label(name))
		case h == r.Version && there:
			st.Seen[name] = h
		case !there:
			push(name, hubproto.MemoryPut{Body: body, Base: ""})
		case r.Deleted:
			if h == base {
				// Deleted on another machine and not changed here: put it away, a copy stays in the trash.
				if err := s.trash(p.Dir, name, body); err != nil {
					fail(name, err)
					continue
				}
				st.Seen[name] = r.Version
				rep.Deleted = append(rep.Deleted, label(name))
				continue
			}
			push(name, hubproto.MemoryPut{Body: body, Base: r.Version})
		case h == base:
			// Unchanged here, changed elsewhere.
			if err := writeFile(filepath.Join(p.Dir, name), []byte(r.Body)); err != nil {
				fail(name, err)
				continue
			}
			st.Seen[name] = r.Version
			rep.Pulled = append(rep.Pulled, label(name))
		case r.Version == base:
			push(name, hubproto.MemoryPut{Body: body, Base: base})
		default:
			conflict(name, r)
		}
	}
	if err := writeState(p.Dir, st); err != nil {
		fail(StateFile, err)
	}
}

func (s *Syncer) trash(dir, name, body string) error {
	t := filepath.Join(dir, TrashDir)
	if err := os.MkdirAll(t, 0o700); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(t, s.now().UTC().Format("20060102T150405")+"-"+name), []byte(body)); err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, name))
}

// Known are the memory folders under Claude Code's projects folder that synced before (they keep their project id).
func Known(projectsDir string) []Project {
	matches, _ := filepath.Glob(filepath.Join(projectsDir, "*", "memory", StateFile))
	out := []Project{}
	for _, m := range matches {
		dir := filepath.Dir(m)
		if st := readState(dir); st.Project != "" {
			out = append(out, Project{ID: st.Project, Dir: dir})
		}
	}
	return out
}

// MemoryDir is Claude Code's memory folder of the project in cwd.
func MemoryDir(projectsDir, cwd string) string {
	return filepath.Join(projectsDir, EntryName(cwd), "memory")
}
