package api

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/ly-solutions/harnessly/internal/hub/sshinstall"
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
		Meta: o.StoreMeta, Blobs: o.StoreBlobs,
	})
	if err != nil {
		return err
	}
	h.Store = st
	return nil
}
