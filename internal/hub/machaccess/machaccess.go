// Package machaccess keeps a machine's access list (docs/hub/machines-resources.md, «Access»): who of the tenant may open
// a machine and start chats on it. The list is set in the machine card; this package is the one place that decides, so
// that opening a machine in the UI, the hub-origin path, the runtime subdomain and the browser socket cannot disagree.
package machaccess

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/switches"
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

// ErrOwnWork is the refusal of a person whose role would have let them in before the owner's rule: what runs on the
// machine is its owner's work, and a role manages the machine without reading it (docs/hub/own-work.md).
var ErrOwnWork = httpx.New(403, "own_work", "Это чужая работа: роль позволяет управлять машиной, но не читать её чаты. Откроется, если владелец добавит вас в доступ")

// FeatureOwnWork is the rollout switch of the owner's rule (docs/hub/rollout-switches.md), keyed by the person who opens
// and the machine: on, opening a machine is reading its owner's work, so neither a role nor an empty list lets anyone
// in but the creator and the people and teams the creator listed.
const FeatureOwnWork = "own-work"

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
	// OwnWork is the owner's rule (FeatureOwnWork): Admin and Manager open nothing, and an empty list is not an opening.
	OwnWork bool
}

// Decide is the rule. A machine's creator, its managers and the administrators of the installation always pass; a machine
// with an empty list is open to the tenant; otherwise the person must be listed or be in a listed team. Under the
// owner's rule (Subject.OwnWork) only the creator and what the creator listed pass. Whether the person belongs to the
// tenant at all is the caller's question, asked before.
func Decide(m store.Machine, entries []store.MachineAccessEntry, who Subject) bool {
	if m.CreatedBy != "" && m.CreatedBy == who.UserID {
		return true
	}
	if !who.OwnWork && (who.Admin || who.Manager || len(entries) == 0) {
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
	// Switches turns on the owner's rule (FeatureOwnWork) for a person on a machine; nil keeps it off.
	Switches switches.Gate
}

// Enforcing says whether the list is looked at at all; with it off the checker costs nothing.
func (c *Checker) Enforcing() bool {
	return c != nil && c.Config != nil && c.Config.Machines.AccessEnforce != "" && c.Config.Machines.AccessEnforce != config.AccessOff
}

// Check is nil when the person may open the machine: its runtime, and with it the chats on it. In warn mode a refusal is
// logged and turned into a yes. It does not end anything the person already has open: running chats go on.
func (c *Checker) Check(ctx context.Context, userID string, m store.Machine) error {
	return c.check(ctx, userID, m, true)
}

// Reach is nil when the person may see the machine and send it a message (peers): the rule before the owner's one, since
// a role manages machines and the receiving runtime asks its owner about messages itself (docs/hub/peers.md).
func (c *Checker) Reach(ctx context.Context, userID string, m store.Machine) error {
	return c.check(ctx, userID, m, false)
}

// ownWork says whether the owner's rule is on for the person on the machine. A failed lookup keeps it off, as a switch
// table that cannot be read does.
func (c *Checker) ownWork(ctx context.Context, userID string, m store.Machine) bool {
	if c.Switches == nil || c.DB == nil || userID == "" {
		return false
	}
	u, err := c.DB.S().UserByID(ctx, userID)
	if err != nil || u.Email == "" {
		return false
	}
	return c.Switches.On(ctx, FeatureOwnWork, strings.ToLower(u.Email), m.ID)
}

func (c *Checker) check(ctx context.Context, userID string, m store.Machine, opening bool) error {
	if !c.Enforcing() {
		return nil
	}
	st := c.DB.S()
	entries, err := st.MachineAccess(ctx, []string{m.ID})
	if err != nil {
		return err
	}
	who := Subject{UserID: userID}
	if opening && m.CreatedBy != userID {
		who.OwnWork = c.ownWork(ctx, userID, m)
	}
	if len(entries) == 0 && !who.OwnWork {
		return nil // open to the tenant: no facts about the person needed
	}
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
	if (who.OwnWork || (!who.Admin && !who.Manager)) && m.CreatedBy != userID {
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
		log.Warn("machine access would be refused", "machine", m.ID, "user", userID, "mode", string(ModeOf(m.CreatedBy, entries)), "own_work", who.OwnWork)
		return nil
	}
	if who.OwnWork && (who.Admin || who.Manager || len(entries) == 0) {
		return ErrOwnWork
	}
	return ErrAccess
}

// Visible says whether the person may see the machine in lists, details and live events. It follows the list only in
// HUB_ACCESS_ENFORCE=on; off and warn show everything (warn still only logs what opening would refuse). A failed lookup
// hides the machine.
func (c *Checker) Visible(ctx context.Context, userID string, m store.Machine) bool {
	if !c.Hides() {
		return true
	}
	return c.Reach(ctx, userID, m) == nil
}

// Hides says whether the access list hides machines at all (HUB_ACCESS_ENFORCE=on): without it nothing is looked up.
func (c *Checker) Hides() bool {
	return c.Enforcing() && c.Config.Machines.AccessEnforce == config.AccessOn
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
