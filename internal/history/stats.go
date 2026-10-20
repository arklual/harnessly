package history

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

var sessionID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ValidSessionID is whether id can name a stats file: anything but a UUID is ignored everywhere, which also keeps a
// session id from walking out of the folder.
func ValidSessionID(id string) bool { return sessionID.MatchString(id) }

// SessionIDOfTranscript is the session id a transcript path names, or "".
func SessionIDOfTranscript(path string) string {
	if base := strings.TrimSuffix(filepath.Base(path), ".jsonl"); ValidSessionID(base) {
		return base
	}
	return ""
}

// Stats is session-stats/<id>.json: the statusline figures of one session, merged field by field.
type Stats struct {
	Dir   string
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (s *Stats) lock(id string) func() {
	s.mu.Lock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	l, ok := s.locks[id]
	if !ok {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (s *Stats) file(id string) string { return filepath.Join(s.Dir, id+".json") }

// Read is a session's stats as stored (keys in their order), or nil.
func (s *Stats) Read(id string) *jsvalue.Object {
	if !ValidSessionID(id) {
		return nil
	}
	raw, err := os.ReadFile(s.file(id))
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse(raw)
	o, _ := v.(*jsvalue.Object)
	if err != nil {
		return nil
	}
	return o
}

// Update merges patch over the stored stats as {...stored, ...patch, sessionId} and returns what was stored: keys
// keep their first place, the session id (patch's own value when it has one) stays. drop are keys the patch sets to
// undefined: JSON leaves them out.
func (s *Stats) Update(id string, patch *jsvalue.Object, drop ...string) (*jsvalue.Object, error) {
	if !ValidSessionID(id) {
		return nil, nil
	}
	defer s.lock(id)()
	var sid any = id
	if v, ok := patch.Get("sessionId"); ok {
		sid = v
	}
	merged := s.Read(id)
	if merged == nil {
		merged = jsvalue.NewObject("sessionId", sid)
	}
	merged.Assign(patch)
	for _, k := range drop {
		merged.Delete(k)
	}
	merged.Set("sessionId", sid)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	target := s.file(id)
	tmp := target + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, jsvalue.Stringify(merged, ""), 0o600); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return merged, nil
}

// Delete removes a session's stats.
func (s *Stats) Delete(id string) {
	if ValidSessionID(id) {
		_ = os.Remove(s.file(id))
	}
}

// CleanupOrphans removes the stats of sessions that have no transcript left in any project folder.
func (s *Stats) CleanupOrphans(projectsDir string) int {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return 0
	}
	alive := map[string]bool{}
	projects, _ := os.ReadDir(projectsDir)
	for _, p := range projects {
		dir := filepath.Join(projectsDir, p.Name())
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			if id, ok := strings.CutSuffix(f.Name(), ".jsonl"); ok && ValidSessionID(id) {
				alive[id] = true
			}
		}
	}
	removed := 0
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if ok && ValidSessionID(id) && !alive[id] {
			s.Delete(id)
			removed++
		}
	}
	return removed
}
