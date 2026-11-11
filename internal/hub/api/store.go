package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
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
		Meta: o.StoreMeta, Blobs: o.StoreBlobs, Design: designView{h.Design}, Kits: h.storeKits(o),
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

// kitAccess is the access model for UI kits, when resource access is on.
func kitAccess(cfg *config.Config, db *store.DB, o Options) *resaccess.Service {
	if !cfg.ResourceAccess {
		return nil
	}
	return resaccess.New(db, o.Now, cfg.PublicURL)
}

// storeKits hands the store the kits' access, when resource access is on: without it the kits collection is open
// to the machines of a space.
func (h *Hub) storeKits(o Options) storehub.Kits {
	if !h.cfg.ResourceAccess {
		return nil
	}
	return kitView{svc: resaccess.New(h.db, o.Now, h.cfg.PublicURL), db: h.db}
}

// kitView is the store's view of the kits over the access model: the person is the one the machine runs for.
type kitView struct {
	svc *resaccess.Service
	db  *store.DB
}

func (v kitView) person(ctx context.Context, m store.Machine) (email, name string, err error) {
	u, err := v.db.S().UserByID(ctx, m.CreatedBy)
	if err != nil {
		return "", "", err
	}
	return u.Email, u.Name, nil
}

// actor is the machine's person as the access model knows a person: the account and the addresses it has proved.
func (v kitView) actor(ctx context.Context, m store.Machine) (resaccess.Actor, error) {
	u, err := v.db.S().UserByID(ctx, m.CreatedBy)
	if err != nil {
		return resaccess.Actor{}, err
	}
	verified, err := v.db.S().VerifiedEmails(ctx, u.ID)
	if err != nil {
		return resaccess.Actor{}, err
	}
	return resaccess.Actor{UserID: u.ID, Email: u.Email, Name: u.Name, Verified: verified}, nil
}

func (v kitView) KitView(ctx context.Context, m store.Machine) (map[string]storehub.KitGrant, error) {
	a, err := v.actor(ctx, m)
	if err != nil {
		return map[string]storehub.KitGrant{}, nil // a machine nobody made sees no kit and claims none
	}
	view, err := v.svc.KitView(ctx, m.SpaceID, a)
	if err != nil {
		return nil, err
	}
	out := make(map[string]storehub.KitGrant, len(view))
	for id, g := range view {
		out[id] = storehub.KitGrant{See: g.See, Edit: g.Edit, Own: g.Own}
	}
	return out, nil
}

func (v kitView) ClaimKit(ctx context.Context, m store.Machine, kit string) (bool, error) {
	email, name, err := v.person(ctx, m)
	if err != nil {
		return false, nil
	}
	owner, err := v.svc.ClaimKit(ctx, m.SpaceID, kit, email, name, false)
	return err == nil && strings.EqualFold(owner, email), err
}

func (v kitView) OpenLegacyKit(ctx context.Context, m store.Machine, kit string) error {
	email, _, err := v.person(ctx, m)
	if err != nil {
		return nil
	}
	return v.svc.OpenLegacyKit(ctx, m.SpaceID, kit, email)
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
