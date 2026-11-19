package mediagen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A job is kept as <Dir>/<id>/job.json each time it changes, so the jobs of earlier runs are read back: the card of an
// old chat still finds its clip, and a job that was waiting on the provider when the machine restarted is told so.

func (s *Service) save(snap Snapshot) {
	dir := filepath.Join(s.Dir, snap.ID)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, "job.json.part")
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(dir, "job.json"))
	}
}

// loadLocked reads the jobs of earlier runs once. A job that had not ended is the earlier run's: it ends as failed, with
// its request id when it had one (the provider may have finished it; the person looks in its console).
func (s *Service) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	if s.jobs == nil {
		s.jobs = map[string]*job{}
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	var snaps []Snapshot
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(s.Dir, e.Name(), "job.json"))
		if err != nil {
			continue
		}
		var snap Snapshot
		if json.Unmarshal(raw, &snap) != nil || snap.ID == "" || snap.ID != e.Name() {
			continue
		}
		if !snap.State.Ended() {
			snap.State = Failed
			snap.Error = "the machine restarted while the job was running"
			if snap.RequestID != "" {
				snap.Error += "; the request " + snap.RequestID + " may be finished at Higgsfield (see its console)"
			}
		}
		if snap.Files == nil {
			snap.Files = []File{}
		}
		snaps = append(snaps, snap)
	}
	slices.SortStableFunc(snaps, func(a, b Snapshot) int { return int(a.StartedAt - b.StartedAt) })
	for _, snap := range snaps {
		if _, have := s.jobs[snap.ID]; have || strings.Contains(snap.ID, "/") {
			continue
		}
		done := make(chan struct{})
		close(done)
		s.jobs[snap.ID] = &job{snap: snap, done: done, cancel: func() {}}
		s.order = append(s.order, snap.ID)
	}
	s.forgetLocked()
}
