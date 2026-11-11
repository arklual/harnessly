package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/repl"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// Caller asks a machine's agent for an op (link.Registry).
type Caller interface {
	Do(ctx context.Context, machineID, op string, params any, timeout time.Duration) (json.RawMessage, error)
}

// Deps are what the service is built from.
type Deps struct {
	DB  *store.DB
	Reg Caller
	Log *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Service takes the machines' chats: on connect it tells the agent where the copy stands (history.start), and it
// applies the frames the agent sends, answering each frame of live events with how far its channel is stored.
// Frames of one machine come in the order the agent sent them (the link hands its events over one by one).
type Service struct {
	db  *store.DB
	reg Caller
	log *slog.Logger
	now func() time.Time
	// apply stores a frame of live events (ApplyFrame).
	apply func(ctx context.Context, db *store.DB, o Owner, f hubproto.HistoryEvents, now time.Time) (FrameResult, error)

	mu       sync.Mutex
	unstored map[string]int // machine → the frames in a row the hub could not store
}

const callTimeout = 15 * time.Second

// lagWarn is how many frames of a machine in a row may fail before the log says the copy is behind as an error.
const lagWarn = 5

// New builds the service.
func New(d Deps) *Service {
	s := &Service{db: d.DB, reg: d.Reg, log: d.Log, now: d.Now, apply: ApplyFrame, unstored: map[string]int{}}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Events are the events a machine with the feature is subscribed to.
func Events(features []string) []string {
	if !slices.Contains(features, hubproto.FeatureHistory) {
		return nil
	}
	return []string{hubproto.EventHistoryEvents, hubproto.EventHistoryBackfill}
}

// Connected tells an agent with the feature where the hub's copy of its chats stands. The owner of what it sends is
// the person who added the machine.
func (s *Service) Connected(m store.Machine, features []string) {
	if !slices.Contains(features, hubproto.FeatureHistory) || m.CreatedBy == "" {
		return
	}
	s.start(m)
}

// Resync tells the agent again where the copy stands, after frames of it were dropped on the way in: the agent sends
// from the cursors what the hub did not store, as on a fresh link. Only a machine that sends frames is told.
func (s *Service) Resync(m store.Machine) {
	if m.CreatedBy == "" {
		return
	}
	s.start(m)
}

func (s *Service) start(m store.Machine) {
	safego.Go("history start", func() {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		st, err := Start(ctx, s.db, m.ID)
		if err != nil {
			s.log.Warn("history: where the copy stands not read", "machine", m.ID, "error", err)
			return
		}
		if _, err := s.reg.Do(ctx, m.ID, hubproto.OpHistoryStart, st, callTimeout); err != nil {
			s.log.Warn("history: history.start not taken", "machine", m.ID, "error", err)
			return
		}
		s.log.Info("history: the agent was told where the copy stands", "machine", m.ID, "channels", len(st.Cursors),
			"chats", len(st.Known), "pending", len(st.Pending))
	})
}

func owner(m store.Machine) Owner { return Owner{MachineID: m.ID, UserID: m.CreatedBy} }

// Live applies a history.events frame and acks it.
func (s *Service) Live(ctx context.Context, m store.Machine, raw json.RawMessage) {
	_ = s.live(ctx, m, raw, false)
}

// LiveBatch is Live for a frame that came as a repl.batch: a frame the hub could not store is the batch's to send
// again (the error), not a gap of the channel; a frame that can never be stored is repl.ErrBad.
func (s *Service) LiveBatch(ctx context.Context, m store.Machine, raw json.RawMessage) error {
	return s.live(ctx, m, raw, true)
}

func (s *Service) live(ctx context.Context, m store.Machine, raw json.RawMessage, batch bool) error {
	if m.CreatedBy == "" {
		return nil
	}
	var f hubproto.HistoryEvents
	if json.Unmarshal(raw, &f) != nil {
		return fmt.Errorf("history.events: %w", repl.ErrBad)
	}
	res, err := s.apply(ctx, s.db, owner(m), f, s.now())
	if err != nil {
		if batch {
			s.countUnstored(ctx, m, f, err)
			if errors.Is(err, ErrBadFrame) {
				return fmt.Errorf("%w: %w", repl.ErrBad, err)
			}
			return err
		}
		s.notStored(ctx, m, f, err)
		return nil
	}
	s.mu.Lock()
	delete(s.unstored, m.ID)
	s.mu.Unlock()
	// In order on the link's event loop: an ack overtaken by an older one would make the agent send again what is stored.
	ack := hubproto.HistoryAck{Ch: f.Ch, Epoch: res.Epoch, AckSeq: res.AckSeq, Gap: res.Gap}
	if _, err := s.reg.Do(ctx, m.ID, hubproto.OpHistoryAck, ack, callTimeout); err != nil {
		s.log.Info("history: ack not taken", "machine", m.ID, "ch", f.Ch, "error", err)
	}
	return nil
}

// Backfill applies a history.backfill frame.
func (s *Service) Backfill(ctx context.Context, m store.Machine, raw json.RawMessage) {
	if err := s.BackfillBatch(ctx, m, raw); err != nil && !errors.Is(err, repl.ErrBad) {
		s.log.Warn("history: backfill not stored", "machine", m.ID, "error", err)
	}
}

// BackfillBatch applies a history.backfill frame and says why it was not stored: the batch of a repl.batch is sent
// again, a frame on the old event is lost as before.
func (s *Service) BackfillBatch(ctx context.Context, m store.Machine, raw json.RawMessage) error {
	if m.CreatedBy == "" {
		return nil
	}
	var f hubproto.HistoryBackfill
	if json.Unmarshal(raw, &f) != nil {
		return fmt.Errorf("history.backfill: %w", repl.ErrBad)
	}
	err := ApplyBackfill(ctx, s.db, owner(m), f, s.now())
	if errors.Is(err, ErrBadFrame) {
		return fmt.Errorf("%w: %w", repl.ErrBad, err)
	}
	if err != nil {
		return fmt.Errorf("ch %s: %w", f.Ch, err)
	}
	return nil
}

// Lag is how many frames of a machine in a row the hub could not store: the copy of its chats is behind by at least
// that much until a frame is stored again.
func (s *Service) Lag(machineID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unstored[machineID]
}

// countUnstored counts a frame of the machine the hub could not store and says so in the log.
func (s *Service) countUnstored(ctx context.Context, m store.Machine, f hubproto.HistoryEvents, cause error) {
	s.mu.Lock()
	s.unstored[m.ID]++
	behind := s.unstored[m.ID]
	s.mu.Unlock()
	level := slog.LevelWarn
	if behind >= lagWarn {
		level = slog.LevelError
	}
	s.log.Log(ctx, level, "history: frame not stored", "machine", m.ID, "ch", f.Ch, "fromSeq", f.FromSeq,
		"events", len(f.Events), "unstoredFrames", behind, "error", cause)
}

// notStored answers a frame that was not stored with where its channel stands and a gap, so the agent sends from
// there again (it paces a repeated refusal itself). A frame that can never be stored gets no answer.
func (s *Service) notStored(ctx context.Context, m store.Machine, f hubproto.HistoryEvents, cause error) {
	s.countUnstored(ctx, m, f, cause)
	if errors.Is(cause, ErrBadFrame) {
		return
	}
	// The frame's own time may be what ran out.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callTimeout)
	defer cancel()
	// Without a cursor of this epoch nothing of the frame's run is stored: the agent sends the frame itself again.
	ack := hubproto.HistoryAck{Ch: f.Ch, Epoch: f.Epoch, AckSeq: f.FromSeq - 1, Gap: true}
	var epoch string
	var seq int64
	err := s.db.Tx(ctx, func(st store.Store) error {
		return st.Q().QueryRow(ctx, `SELECT epoch, seq FROM history_cursors WHERE machine_id = ? AND ch = ?`, m.ID, f.Ch).Scan(&epoch, &seq)
	})
	switch {
	case err == nil && epoch == f.Epoch:
		ack.AckSeq = seq
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		// Where the copy stands is not known either: the agent's own timer sends again.
		s.log.Warn("history: cursor not read after a frame was not stored", "machine", m.ID, "ch", f.Ch, "error", err)
		return
	}
	if _, err := s.reg.Do(ctx, m.ID, hubproto.OpHistoryAck, ack, callTimeout); err != nil {
		s.log.Info("history: ack not taken", "machine", m.ID, "ch", f.Ch, "error", err)
	}
}
