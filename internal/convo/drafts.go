package convo

import (
	"slices"
	"sort"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The composers' drafts (wire.Draft): one register per field with a revision. A write is taken only when it names the
// next revision, so the device that has not seen the last write is refused and catches up from the convs channel
// instead of putting an older text back; that is what makes a clear on one device stay cleared on the others. Every
// taken write publishes draft_upsert; drafts.json is written by the saver once a second (and at the stop), not on
// every keystroke.

// keptDraft is a draft as the runtime holds it: the draft and the seq of the event that showed it, so the same write
// sent again (a retry of a lost answer) gets the answer the first one got.
type keptDraft struct {
	Draft wire.Draft `json:"draft"`
	Seq   int64      `json:"seq"`
}

// draftBook is the drafts by key. Not safe for concurrent use: the runtime holds it under draftMu.
type draftBook map[string]keptDraft

// verdict of a write against the book: take it (a new revision), it is the one already taken (same device, revision
// and text), or refuse it.
type draftVerdict int

const (
	draftTake draftVerdict = iota
	draftSame
	draftConflict
)

func (b draftBook) judge(device string, m wire.DraftSetCmd) draftVerdict {
	cur, ok := b[m.Key]
	if ok && m.Rev == cur.Draft.Rev && m.SameField(cur.Draft) && device == cur.Draft.DeviceID {
		return draftSame
	}
	if m.Rev != cur.Draft.Rev+1 {
		return draftConflict
	}
	return draftTake
}

// prune keeps the DraftsMax most recently written drafts.
func (b draftBook) prune() {
	if len(b) <= wire.DraftsMax {
		return
	}
	keys := make([]string, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return b[keys[i]].Draft.At < b[keys[j]].Draft.At })
	for _, k := range keys[:len(keys)-wire.DraftsMax] {
		delete(b, k)
	}
}

// list is every draft, by key, for the convs snapshot; nil when there is none.
func (b draftBook) list() []wire.Draft {
	if len(b) == 0 {
		return nil
	}
	out := make([]wire.Draft, 0, len(b))
	for _, k := range b {
		out = append(out, k.Draft)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// draftSet writes a draft from device. It does not go through the op records: the revision makes it idempotent, and
// a person typing would otherwise crowd the answers of the sends out of them.
func (r *Runtime) draftSet(device string, m wire.DraftSetCmd) wire.CmdResult {
	if device == "" {
		return wire.Refused(wire.RefuseBadRequest)
	}
	r.draftMu.Lock()
	defer r.draftMu.Unlock()
	switch r.drafts.judge(device, m) {
	case draftSame:
		kept := r.drafts[m.Key]
		if kept.Seq == 0 {
			// Kept from before a restart: its seq was of another epoch, the page is shown it again.
			kept.Seq = r.d.Publisher.Publish(wire.ChannelConvs, wire.DraftUpsertEv{Draft: kept.Draft})
			r.drafts[m.Key] = kept
		}
		res := wire.Accepted(wire.ChannelConvs, kept.Seq)
		res.Rev = &m.Rev
		return res
	case draftConflict:
		return wire.Refused(wire.RefuseConflict)
	}
	d := wire.Draft{Key: m.Key, Text: m.Text, Rev: m.Rev, DeviceID: device, At: ms(r.d.Clock.Now())}
	if len(m.Attachments) > 0 {
		d.Attachments = slices.Clone(m.Attachments)
	}
	if len(m.Refs) > 0 {
		d.Refs = slices.Clone(m.Refs)
	}
	seq := r.d.Publisher.Publish(wire.ChannelConvs, wire.DraftUpsertEv{Draft: d})
	r.drafts[m.Key] = keptDraft{Draft: d, Seq: seq}
	r.drafts.prune()
	r.draftsDirty = true
	res := wire.Accepted(wire.ChannelConvs, seq)
	res.Rev = &m.Rev
	return res
}

func (r *Runtime) draftList() []wire.Draft {
	r.draftMu.Lock()
	defer r.draftMu.Unlock()
	return r.drafts.list()
}

// saveDrafts writes drafts.json when a draft changed since the last write.
func (r *Runtime) saveDrafts() {
	r.draftMu.Lock()
	if !r.draftsDirty || r.d.Store == nil {
		r.draftMu.Unlock()
		return
	}
	snapshot := make(draftBook, len(r.drafts))
	for k, v := range r.drafts {
		snapshot[k] = v
	}
	r.draftsDirty = false
	r.draftMu.Unlock()
	if err := r.d.Store.saveDrafts(snapshot); err != nil {
		r.d.Log.Warn("drafts not saved", "err", err)
	}
}
