package chatmove

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// ResumeUploader keeps the resume snapshots of a machine's chats in the hub's store, in the background: a chat whose
// history grew and that has been at rest for a whole tick (the turn is over, nothing more is coming) is sealed and put
// once; a chat that did not change since is not sent again. A turn never waits on it.
type ResumeUploader struct {
	Service *Service
	// Machine is this machine's id in the hub: the folder of the room it writes in.
	Machine string
	// List is every conversation of the machine, without waking any.
	List func() []string
	// State is the file that remembers which fence of each chat the store has, so a restart sends only what changed.
	State string
	// Every is the tick (DefaultResumeEvery when 0).
	Every time.Duration
	Log   *slog.Logger
	Now   func() time.Time

	sent    map[string]string // conversation -> fence the store has
	pending map[string]string // conversation -> fence seen at the last tick, not yet sent
	loaded  bool
}

// DefaultResumeEvery is how often the uploader looks at the chats.
const DefaultResumeEvery = 15 * time.Second

type resumeState struct {
	Sent map[string]string `json:"sent"`
}

// Run ticks until ctx ends.
func (u *ResumeUploader) Run(ctx context.Context) {
	every := u.Every
	if every <= 0 {
		every = DefaultResumeEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		u.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick is one look at the chats. The first one on a machine with no state only takes note of where every chat is: the
// chats that were here before the switch was turned on get a snapshot after their next turn, not all at once.
func (u *ResumeUploader) Tick(ctx context.Context) {
	if !u.loaded {
		u.load()
	}
	baseline := u.sent == nil
	if baseline {
		u.sent = map[string]string{}
	}
	if u.pending == nil {
		u.pending = map[string]string{}
	}
	live := map[string]bool{}
	changed := baseline
	for _, conv := range u.List() {
		if ctx.Err() != nil {
			return
		}
		live[conv] = true
		fence, ok := u.Service.Conv.Fence(conv)
		if !ok || fence == "" {
			continue
		}
		if baseline {
			u.sent[conv] = fence
			continue
		}
		if u.sent[conv] == fence {
			delete(u.pending, conv)
			continue
		}
		if u.pending[conv] != fence {
			u.pending[conv] = fence
			continue
		}
		if u.put(ctx, conv) {
			changed = true
		}
	}
	for conv := range u.sent {
		if !live[conv] {
			delete(u.sent, conv)
			changed = true
		}
	}
	for conv := range u.pending {
		if !live[conv] {
			delete(u.pending, conv)
		}
	}
	if changed {
		u.save()
	}
}

// put seals one chat and puts it; true once the store has it. A chat that cannot have a snapshot now (working again,
// too big) is looked at again after its next change.
func (u *ResumeUploader) put(ctx context.Context, conv string) bool {
	r := u.Service.Resume
	key, err := r.Key(ctx)
	if err != nil {
		u.log().Warn("chat resume: no key in the hub's safe", "err", err)
		return false
	}
	snap, fence, ref := u.Service.SealResume(ctx, u.Machine, conv, key, u.now().UnixMilli())
	if ref != nil {
		delete(u.pending, conv)
		if ref.Code != CodeBusy {
			u.sent[conv], _ = u.Service.Conv.Fence(conv)
			u.log().Info("chat resume: no snapshot of the chat", "conversation", conv, "why", errorText(ref))
			return true
		}
		return false
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return false
	}
	if len(data) > filestore.MaxFileSize {
		u.sent[conv] = fence
		u.log().Info("chat resume: the snapshot is bigger than the store takes", "conversation", conv, "bytes", len(data))
		return true
	}
	if _, err := r.Store.Put(ctx, ResumePath(u.Machine, conv), data, u.Machine); err != nil {
		u.log().Warn("chat resume: the store did not take the snapshot", "conversation", conv, "err", err)
		return false
	}
	delete(u.pending, conv)
	u.sent[conv] = fence
	return true
}

func (u *ResumeUploader) load() {
	u.loaded = true
	if u.State == "" {
		return
	}
	b, err := os.ReadFile(u.State)
	if err != nil {
		return
	}
	var st resumeState
	if json.Unmarshal(b, &st) == nil && st.Sent != nil {
		u.sent = st.Sent
	}
}

func (u *ResumeUploader) save() {
	if u.State == "" {
		return
	}
	b, err := json.Marshal(resumeState{Sent: u.sent})
	if err != nil {
		return
	}
	tmp := u.State + ".tmp"
	err = os.MkdirAll(filepath.Dir(u.State), 0o700)
	if err == nil {
		err = os.WriteFile(tmp, b, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, u.State)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		u.log().Warn("chat resume: cannot keep the state", "err", err)
	}
}

func (u *ResumeUploader) log() *slog.Logger {
	if u.Log != nil {
		return u.Log
	}
	return slog.Default()
}

func (u *ResumeUploader) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}
