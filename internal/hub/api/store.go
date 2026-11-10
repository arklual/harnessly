package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/sshinstall"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
)

// newStore opens the store (docs/hub/store.md) unless HUB_STORE=off. Its sessions are signed with a key taken from the
// hub's master key, so every replica checks the others' sessions.
func (h *Hub) newStore(o Options) error {
	cfg := h.cfg
	if cfg.Store.Off {
		return nil
	}
	master, err := sshinstall.MasterKey(cfg.DataDir, cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	key := sha256.Sum256(append([]byte("harnessly-hub/store-session/v1\x00"), master...))
	st, err := storehub.Open(context.Background(), storehub.Options{
		Config: cfg, DB: h.db.S(), Admit: h.Dialer.AdmitStore, Key: key[:], Log: h.log, Now: o.Now,
		Meta: o.StoreMeta, Blobs: o.StoreBlobs, Design: designView{h.Design},
	})
	if err != nil {
		return err
	}
	h.Store = st
	return nil
}

// designView hands the store the design access: which projects a machine's copy of the library holds.
type designView struct{ d *designaccess.Service }

func (v designView) StoreView(ctx context.Context, m store.Machine) (map[string]storehub.Grant, error) {
	if v.d == nil {
		return nil, nil
	}
	view, err := v.d.StoreView(ctx, m)
	if err != nil {
		return nil, err
	}
	out := make(map[string]storehub.Grant, len(view))
	for id, g := range view {
		out[id] = storehub.Grant{See: g.See, Edit: g.Edit, Own: g.Own}
	}
	return out, nil
}

func (v designView) RecordFromStore(ctx context.Context, m store.Machine, project string, ownerFile []byte) (bool, error) {
	if v.d == nil {
		return false, nil
	}
	return v.d.RecordFromStore(ctx, m, project, ownerFile)
}

// mayOpenMachine checks the person may open the machine and see it, as GET /api/machines/{id} does: the journal of its
// move (internal/hub/migration) is no one else's.
func (h *Hub) mayOpenMachine(r *http.Request, p *auth.Principal, machineID string) error {
	m, err := h.Machines.Authorize(r.Context(), p.User.ID, machineID, false)
	if err != nil {
		return err
	}
	if !h.Machines.Visible(r.Context(), p.User.ID, m) {
		return httpx.ErrNotFound
	}
	return nil
}

// mayChangeMachine checks the person may change what the machine does: its own person, or an owner of its space.
func (h *Hub) mayChangeMachine(r *http.Request, p *auth.Principal, machineID string) error {
	m, err := h.Machines.Authorize(r.Context(), p.User.ID, machineID, false)
	if err != nil {
		return err
	}
	if !h.Machines.Visible(r.Context(), p.User.ID, m) {
		return httpx.ErrNotFound
	}
	if m.CreatedBy == p.User.ID {
		return nil
	}
	_, err = h.Machines.Authorize(r.Context(), p.User.ID, machineID, true)
	return err
}
