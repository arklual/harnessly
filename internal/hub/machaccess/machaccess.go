// Package machaccess keeps a machine's access list (docs/hub/machines-resources.md, «Access»): who of the tenant may open
// a machine and start chats on it. The list is set in the machine card; this package is the one place that decides, so
// that opening a machine in the UI, the hub-origin path, the runtime subdomain and the browser socket cannot disagree.
package machaccess

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Mode is how a machine is shared, as the machine card names it. It is read off the list, not stored.
type Mode string

const (
	// ModeTenant: no entries, the whole tenant may use the machine.
	ModeTenant Mode = "tenant"
	// ModeOwner: the machine's creator and nobody else.
	ModeOwner Mode = "owner"
	// ModeTeam: only teams are listed.
	ModeTeam Mode = "team"
	// ModePick: people are listed by name (teams may be among them).
	ModePick Mode = "pick"
)

// ErrAccess is the refusal.
var ErrAccess = httpx.New(403, "machine_access", "Эта машина не открыта для вас: попросите владельца добавить вас в доступ")

// ModeOf reads the mode off a machine's access list.
func ModeOf(creator string, entries []store.MachineAccessEntry) Mode {
	if len(entries) == 0 {
		return ModeTenant
	}
	teams, users, others := 0, 0, 0
	for _, e := range entries {
		switch e.Kind {
		case store.AccessTeam:
			teams++
		case store.AccessUser:
			users++
			if e.RefID != creator {
				others++
			}
		}
	}
	switch {
	case users == 1 && teams == 0 && others == 0:
		return ModeOwner
	case users == 0:
		return ModeTeam
	}
	return ModePick
}

// Subject is what is known about the person who opens a machine.
type Subject struct {
	UserID string
	// Admin: an administrator of the installation.
	Admin bool
	// Manager: manages the machine's tenant, or the machine's team.
	Manager bool
	// Teams are the ids of the teams the person is in.
	Teams []string
}

// Decide is the rule. A machine's creator, its managers and the administrators of the installation always pass; a machine
// with an empty list is open to the tenant; otherwise the person must be listed or be in a listed team. Whether the
// person belongs to the tenant at all is the caller's question, asked before.
func Decide(m store.Machine, entries []store.MachineAccessEntry, who Subject) bool {
	if who.Admin || who.Manager || (m.CreatedBy != "" && m.CreatedBy == who.UserID) {
		return true
	}
	if len(entries) == 0 {
		return true
	}
	for _, e := range entries {
		switch e.Kind {
		case store.AccessUser:
			if e.RefID == who.UserID {
				return true
			}
		case store.AccessTeam:
			for _, t := range who.Teams {
				if t == e.RefID {
					return true
				}
			}
		}
	}
	return false
}

// Checker asks the database the facts and applies Decide under HUB_ACCESS_ENFORCE.
type Checker struct {
	Config *config.Config
	DB     *store.DB
	// Authz is the rights module (spaces.Service.Authz); IsAdmin tells an administrator of the installation.
	Authz   func() authz.Authz
	IsAdmin func(ctx context.Context, userID string) (bool, error)
	Log     *slog.Logger
}

// Enforcing says whether the list is looked at at all; with it off the checker costs nothing.
func (c *Checker) Enforcing() bool {
	return c != nil && c.Config != nil && c.Config.Machines.AccessEnforce != "" && c.Config.Machines.AccessEnforce != config.AccessOff
}

// Check is nil when the person may open the machine. In warn mode a refusal is logged and turned into a yes. It does not
// end anything the person already has open: running chats go on.
func (c *Checker) Check(ctx context.Context, userID string, m store.Machine) error {
	if !c.Enforcing() {
		return nil
	}
	st := c.DB.S()
	entries, err := st.MachineAccess(ctx, []string{m.ID})
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil // open to the tenant: no facts about the person needed
	}
	who := Subject{UserID: userID}
	if c.IsAdmin != nil {
		if who.Admin, err = c.IsAdmin(ctx, userID); err != nil {
			return err
		}
	}
	if !who.Admin && m.CreatedBy != userID {
		if who.Manager, err = c.manages(ctx, userID, m); err != nil {
			return err
		}
	}
	if !who.Admin && !who.Manager && m.CreatedBy != userID {
		teams, err := st.TeamsOfUser(ctx, userID)
		if err != nil {
			return err
		}
		for _, t := range teams {
			who.Teams = append(who.Teams, t.TeamID)
		}
	}
	if Decide(m, entries, who) {
		return nil
	}
	if c.Config.Machines.AccessEnforce == config.AccessWarn {
		log := c.Log
		if log == nil {
			log = slog.Default()
		}
		log.Warn("machine access would be refused", "machine", m.ID, "user", userID, "mode", string(ModeOf(m.CreatedBy, entries)))
		return nil
	}
	return ErrAccess
}

// manages: the person manages the machine's tenant, or its team.
func (c *Checker) manages(ctx context.Context, userID string, m store.Machine) (bool, error) {
	if c.Authz == nil || m.SpaceID == "" {
		return false, nil
	}
	a := c.Authz()
	p := authz.Principal{UserID: userID}
	scopes := []authz.Scope{{Kind: store.ScopeTenant, ID: m.SpaceID}}
	if m.TeamID != "" {
		scopes = append(scopes, authz.Scope{Kind: store.ScopeTeam, ID: m.TeamID})
	}
	for _, sc := range scopes {
		_, err := a.Can(ctx, p, authz.Check{Action: authz.Manage, Scope: sc})
		if err == nil {
			return true, nil
		}
		var he *httpx.Error
		if errors.As(err, &he) && he.Status == 403 {
			continue
		}
		return false, err
	}
	return false, nil
}
