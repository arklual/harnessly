package history

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
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
}

const callTimeout = 15 * time.Second

// New builds the service.
func New(d Deps) *Service {
	s := &Service{db: d.DB, reg: d.Reg, log: d.Log, now: d.Now}
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
	go func() {
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
	}()
}

func owner(m store.Machine) Owner { return Owner{MachineID: m.ID, UserID: m.CreatedBy} }

// Live applies a history.events frame and acks it.
func (s *Service) Live(ctx context.Context, m store.Machine, raw json.RawMessage) {
	if m.CreatedBy == "" {
		return
	}
	var f hubproto.HistoryEvents
	if json.Unmarshal(raw, &f) != nil {
		return
	}
	res, err := ApplyFrame(ctx, s.db, owner(m), f, s.now())
	if err != nil {
		s.log.Warn("history: frame not stored", "machine", m.ID, "ch", f.Ch, "error", err)
		return
	}
	// In order on the link's event loop: an ack overtaken by an older one would make the agent send again what is stored.
	ack := hubproto.HistoryAck{Ch: f.Ch, Epoch: res.Epoch, AckSeq: res.AckSeq, Gap: res.Gap}
	if _, err := s.reg.Do(ctx, m.ID, hubproto.OpHistoryAck, ack, callTimeout); err != nil {
		s.log.Info("history: ack not taken", "machine", m.ID, "ch", f.Ch, "error", err)
	}
}

// Backfill applies a history.backfill frame.
func (s *Service) Backfill(ctx context.Context, m store.Machine, raw json.RawMessage) {
	if m.CreatedBy == "" {
		return
	}
	var f hubproto.HistoryBackfill
	if json.Unmarshal(raw, &f) != nil {
		return
	}
	if err := ApplyBackfill(ctx, s.db, owner(m), f, s.now()); err != nil {
		s.log.Warn("history: backfill not stored", "machine", m.ID, "ch", f.Ch, "error", err)
	}
}
