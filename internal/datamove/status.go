package datamove

import (
	"context"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Status is how far the move is, by the machine's own journal. Hub says whether the hub keeps the journal too ("on")
// or why not; Running that a pass is going now.
type Status struct {
	hubproto.MigrationStatus
	Hub     string `json:"hub"`
	Running bool   `json:"running"`
	// KeptUntil is when the local copies may go: three versions after the one that finished the move. Nothing removes
	// them yet; it is said so that a person knows they are still there.
	KeptUntil string `json:"keptUntil,omitempty"`
}

// Status is the move's status.
func (m *Mover) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{MigrationStatus: m.statusLocked(), Hub: m.hub, Running: m.running}
	if m.st.VerifiedIn != "" {
		st.KeptUntil = "three versions after " + m.st.VerifiedIn
	}
	return st
}

func (m *Mover) statusLocked() hubproto.MigrationStatus {
	st := hubproto.MigrationStatus{Migration: ID, Version: m.o.Version, Doc: "docs/migrations/" + ID + ".md"}
	for _, r := range m.sorted() {
		st.Total++
		switch r.State {
		case hubproto.MigrationVerified:
			st.Verified++
		case hubproto.MigrationSkipped:
			st.Skipped++
		case hubproto.MigrationFailed:
			st.Failed++
			if r.Retry {
				st.Retrying++
			}
			if len(st.Failures) < hubproto.MigrationFailuresMax {
				st.Failures = append(st.Failures, hubproto.MigrationObject{Kind: r.Kind, Object: r.Object, Sha256: r.Sha256,
					Size: r.Size, State: r.State, Code: r.Code, Error: r.Error, Attempts: r.Attempts, ChangedAt: ms(r.At)})
			}
		default:
			st.Pending++
		}
	}
	st.StartedAt, st.ChangedAt, st.FinishedAt = ms(m.st.StartedAt), ms(m.st.ChangedAt), ms(m.st.FinishedAt)
	return st
}

func ms(v int64) string {
	if v == 0 {
		return ""
	}
	return time.UnixMilli(v).UTC().Format(time.RFC3339)
}

// Retry asks to try failed objects again (none named: every failed one) and wakes the move. It says how many.
func (m *Mover) Retry(keys []hubproto.MigrationKey) int {
	n := m.mark(keys, func(r *record) bool {
		if r.State != hubproto.MigrationFailed {
			return false
		}
		r.Retry = true
		return true
	})
	m.Wake()
	return n
}

// Skip lets objects stay on the machine only: the move can be over without them. Named objects only.
func (m *Mover) Skip(keys []hubproto.MigrationKey) int {
	if len(keys) == 0 {
		return 0
	}
	n := m.mark(keys, func(r *record) bool {
		if r.State == hubproto.MigrationVerified || r.State == hubproto.MigrationSkipped {
			return false
		}
		r.State, r.Retry, r.Dirty = hubproto.MigrationSkipped, false, true
		return true
	})
	m.Wake()
	return n
}

func (m *Mover) mark(keys []hubproto.MigrationKey, f func(*record) bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	if len(keys) == 0 {
		for _, r := range m.st.Records {
			if f(r) {
				n++
			}
		}
	}
	for _, k := range keys {
		if r := m.st.Records[key(k.Kind, k.Object)]; r != nil && f(r) {
			n++
		}
	}
	return n
}

// Verified is what a check of the verified objects found.
type Verified struct {
	Checked int                     `json:"checked"`
	Missing []hubproto.MigrationKey `json:"missing,omitempty"`
}

// Verify asks the store again for every verified object: one it does not have is moved again (it waits, and the move
// wakes).
func (m *Mover) Verify(ctx context.Context) (Verified, error) {
	m.mu.Lock()
	var check []*record
	for _, r := range m.sorted() {
		// The sidebar is no blob: the hub checks it itself on each report.
		if r.State == hubproto.MigrationVerified && r.Kind != KindSidebar {
			check = append(check, &record{Kind: r.Kind, Object: r.Object, Sha256: r.Sha256})
		}
	}
	m.mu.Unlock()
	var out Verified
	for _, c := range check {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		has, err := m.o.Target.HasBlob(ctx, c.Kind, c.Sha256)
		if err != nil {
			return out, err
		}
		out.Checked++
		if has {
			continue
		}
		out.Missing = append(out.Missing, hubproto.MigrationKey{Kind: c.Kind, Object: c.Object})
		m.mu.Lock()
		if r := m.st.Records[key(c.Kind, c.Object)]; r != nil {
			r.State, r.Dirty, r.Size, r.MTime = hubproto.MigrationPending, true, 0, 0
		}
		m.mu.Unlock()
	}
	if len(out.Missing) > 0 {
		m.Wake()
	}
	return out, nil
}
