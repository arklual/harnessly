package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Settings on their way to a machine, by their owner (wire.Settings).
//
//   - The machine's own (the runtime's settings) stay the machine's: the hub passes them on and keeps what the machine
//     answered (machine_cards.settings), so a page still reads them while the machine is away. The copy is read-only:
//     a write to a machine that is away is refused as any other request to it.
//   - The person's are the hub's (personsettings.go). A runtime that works by one of them (wire.RuntimePerson) gets
//     the value of the person the request is made for with the request (wire.PersonHeader); nothing a browser sends
//     under that name reaches a runtime.

// SettingsCopyHeader marks an answer given from the hub's copy of a machine's settings: when the machine gave them,
// in Unix milliseconds.
const SettingsCopyHeader = "X-Harnessly-Copy-At"

// v2Path is the path under /api/v2 a request to the machine id names ("runtime-settings", "voice"), whichever way in
// it came by: the fleet's, the workspace's or the machine's own address.
func v2Path(p, id string) (string, bool) {
	p = strings.TrimPrefix(p, "/m/"+id)
	p = strings.TrimPrefix(p, workspaceBase)
	return strings.CutPrefix(p, "/api/v2/")
}

// machineSettingsDoc says whether the request reads or writes a settings document the machine owns that the hub keeps
// a copy of.
func machineSettingsDoc(r *http.Request, id string) bool {
	rest, ok := v2Path(r.URL.Path, id)
	if !ok || (r.Method != http.MethodGet && r.Method != http.MethodPut) {
		return false
	}
	doc, owner, ok := wire.SettingsDocOf(rest)
	return ok && owner == wire.OwnerMachine && doc == wire.SettingsDocRuntime
}

// keepMachineSettings keeps the settings the machine answered with. Only the answer to a page that reads every part of them
// is kept, so the copy is never a narrowed one.
func (s *Service) keepMachineSettings(ctx context.Context, id string, r *http.Request, t *usageTee) {
	if t.status != http.StatusOK || t.over || !wire.CapsOf(r).Delegation {
		return
	}
	var got wire.RuntimeSettings
	if json.Unmarshal(t.body.Bytes(), &got) != nil || got.Validate() != nil {
		return
	}
	raw, err := json.Marshal(got)
	if err != nil {
		return
	}
	if _, err := s.db.Exec(context.WithoutCancel(ctx), `INSERT INTO machine_cards (machine_id, settings, settings_at) VALUES (?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET settings = excluded.settings, settings_at = excluded.settings_at`,
		id, string(raw), store.Ms(s.now())); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Warn("machine cards: settings not kept", "machine", id, "err", err)
	}
}

// serveSettingsCopy answers a read of the machine's settings from the hub's copy, and says whether it did: not for a
// write, and not when the machine never gave them.
func (s *Service) serveSettingsCopy(w http.ResponseWriter, r *http.Request, id string) bool {
	if r.Method != http.MethodGet || !machineSettingsDoc(r, id) {
		return false
	}
	var raw sql.NullString
	var at sql.NullInt64
	err := s.db.QueryRow(r.Context(), `SELECT settings, settings_at FROM machine_cards WHERE machine_id = ?`, id).Scan(&raw, &at)
	if err != nil || !raw.Valid {
		return false
	}
	var kept wire.RuntimeSettings
	if json.Unmarshal([]byte(raw.String), &kept) != nil || kept.Validate() != nil {
		return false
	}
	w.Header().Set(SettingsCopyHeader, strconv.FormatInt(at.Int64, 10))
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, wire.RuntimeSettingsFor(kept, wire.CapsOf(r)))
	return true
}

// personHeader is the value of wire.PersonHeader for a request the person makes to the machine: their settings the
// runtime works by, for a path whose handler reads them; "" for every other request.
func (s *Service) personHeader(r *http.Request, u store.User, id string) string {
	rest, ok := v2Path(r.URL.Path, id)
	if !ok || strings.Trim(rest, "/") != "voice" || r.Method != http.MethodPost {
		return ""
	}
	me, err := (&hubMe{s: s, r: r, user: u}).load()
	if err != nil {
		return ""
	}
	return wire.EncodePersonHeader(wire.RuntimePersonOf(me.Settings))
}
