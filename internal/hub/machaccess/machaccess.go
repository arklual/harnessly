// Package machaccess keeps a machine's access list (docs/hub/machines-resources.md, «Access»): who of the tenant may open
// a machine and start chats on it. The list is set in the machine card; this package is the one place that decides, so
// that opening a machine in the UI, the hub-origin path, the runtime subdomain and the browser socket cannot disagree.
package machaccess

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

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
var ErrOwnWork = httpx.New(403, "own_work", "Это чужая работа: роль позволяет управлять машиной, но не читать её чаты. Откройте её для отладки с причиной — владелец это увидит — или попросите владельца добавить вас в доступ")

// DebugFor is how long a debug opening lasts (docs/hub/own-work.md, «Debug mode»).
const DebugFor = 30 * time.Minute

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

// Level is how far a machine is open to a person (docs/hub/own-work.md, «Levels»): not at all, to watch the work on it
// (read its chats, terminals and pages) or to give it tasks as well (write in chats, start and stop, type in terminals).
type Level int

const (
	LevelNone Level = iota
	LevelView
	LevelTasks
)

func (l Level) String() string {
	switch l {
	case LevelView:
		return store.AccessView
	case LevelTasks:
		return store.AccessTasks
	}
	return "none"
}

// entryLevel is the level one line of the list gives.
func entryLevel(e store.MachineAccessEntry) Level {
	if e.AccessLevel() == store.AccessView {
		return LevelView
	}
	return LevelTasks
}

// Grant is the rule. A machine's creator, its managers and the administrators of the installation always get tasks; a
// machine with an empty list gives tasks to the tenant; otherwise the person gets the highest level of the lines that
// name them or a team they are in. Under the owner's rule (Subject.OwnWork) only the creator and what the creator listed
// count. Whether the person belongs to the tenant at all is the caller's question, asked before.
func Grant(m store.Machine, entries []store.MachineAccessEntry, who Subject) Level {
	if m.CreatedBy != "" && m.CreatedBy == who.UserID {
		return LevelTasks
	}
	if !who.OwnWork && (who.Admin || who.Manager || len(entries) == 0) {
		return LevelTasks
	}
	best := LevelNone
	for _, e := range entries {
		match := false
		switch e.Kind {
		case store.AccessUser:
			match = e.RefID == who.UserID
		case store.AccessTeam:
			for _, t := range who.Teams {
				if t == e.RefID {
					match = true
					break
				}
			}
		}
		if l := entryLevel(e); match && l > best {
			best = l
		}
	}
	return best
}

// Decide says whether the person gets anything at all of the machine (Grant above LevelNone).
func Decide(m store.Machine, entries []store.MachineAccessEntry, who Subject) bool {
	return Grant(m, entries, who) > LevelNone
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
	// Now is the clock of debug openings; nil means time.Now.
	Now func() time.Time
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Enforcing says whether the list is looked at at all; with it off the checker costs nothing.
func (c *Checker) Enforcing() bool {
	return c != nil && c.Config != nil && c.Config.Machines.AccessEnforce != "" && c.Config.Machines.AccessEnforce != config.AccessOff
}

// ErrViewOnly refuses a write to a person the machine is open to only to watch.
var ErrViewOnly = httpx.New(403, "view_only", "Владелец открыл вам эту машину только смотреть: писать в чаты, запускать и печатать в терминале на ней нельзя")

// Check is nil when the person may open the machine and give it tasks: its runtime, and with it the chats on it. In warn
// mode a refusal is logged and turned into a yes. It does not end anything the person already has open: running chats
// go on.
func (c *Checker) Check(ctx context.Context, userID string, m store.Machine) error {
	return c.check(ctx, userID, m, true, LevelTasks)
}

// View is nil when the person may open the machine at least to watch (Level).
func (c *Checker) View(ctx context.Context, userID string, m store.Machine) error {
	return c.check(ctx, userID, m, true, LevelView)
}

// Level is how far the machine opens to the person, the owner's rule included; below LevelView it is the refusal. In
// warn mode and with the list not enforced it is LevelTasks.
func (c *Checker) Level(ctx context.Context, userID string, m store.Machine) (Level, error) {
	if !c.Enforcing() {
		return LevelTasks, nil
	}
	lvl, err := c.level(ctx, userID, m, true)
	if err != nil {
		return LevelNone, err
	}
	switch {
	case c.refuse(userID, m, lvl, LevelTasks) == nil:
		return LevelTasks, nil // a full opening, or warn mode
	case lvl.got >= LevelView:
		return LevelView, nil
	}
	return LevelNone, c.refuse(userID, m, lvl, LevelView)
}

// Reach is nil when the person may see the machine (lists, peers): the rule before the owner's one, since a role
// manages machines, and any level of the list will do (docs/hub/peers.md).
func (c *Checker) Reach(ctx context.Context, userID string, m store.Machine) error {
	return c.check(ctx, userID, m, false, LevelView)
}

// Send is nil when the person may send the machine a message (peers): Reach, but a line that opens the machine only to
// watch does not let its people give it tasks.
func (c *Checker) Send(ctx context.Context, userID string, m store.Machine) error {
	return c.check(ctx, userID, m, false, LevelTasks)
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

// LevelsOn says whether the person may give a line of the machine's list a level below tasks: levels come out with the
// owner's rule (FeatureOwnWork), for the same people on the same machines.
func (c *Checker) LevelsOn(ctx context.Context, userID string, m store.Machine) bool {
	return c != nil && c.ownWork(ctx, userID, m)
}

// facts gathers what Decide needs about the person on the machine; skip says the machine is open to them without any.
func (c *Checker) facts(ctx context.Context, userID string, m store.Machine, opening bool) (entries []store.MachineAccessEntry, who Subject, skip bool, err error) {
	st := c.DB.S()
	if entries, err = st.MachineAccess(ctx, []string{m.ID}); err != nil {
		return nil, who, false, err
	}
	who = Subject{UserID: userID}
	if opening && m.CreatedBy != userID {
		who.OwnWork = c.ownWork(ctx, userID, m)
	}
	if len(entries) == 0 && !who.OwnWork {
		return entries, who, true, nil // open to the tenant: no facts about the person needed
	}
	if c.IsAdmin != nil {
		if who.Admin, err = c.IsAdmin(ctx, userID); err != nil {
			return nil, who, false, err
		}
	}
	if !who.Admin && m.CreatedBy != userID {
		if who.Manager, err = c.manages(ctx, userID, m); err != nil {
			return nil, who, false, err
		}
	}
	if (who.OwnWork || (!who.Admin && !who.Manager)) && m.CreatedBy != userID {
		teams, err := st.TeamsOfUser(ctx, userID)
		if err != nil {
			return nil, who, false, err
		}
		for _, t := range teams {
			who.Teams = append(who.Teams, t.TeamID)
		}
	}
	return entries, who, false, nil
}

// roleOnly: the owner's rule is the one thing that keeps the person below the level they need, their role would have
// let them in.
func roleOnly(m store.Machine, entries []store.MachineAccessEntry, who Subject, need Level) bool {
	return who.OwnWork && (who.Admin || who.Manager) && Grant(m, entries, who) < need
}

// granted is what level found: the level, and the facts behind it for the refusal.
type granted struct {
	got     Level
	entries []store.MachineAccessEntry
	who     Subject
}

// level works out the person's level; a debug opening gives tasks for its while.
func (c *Checker) level(ctx context.Context, userID string, m store.Machine, opening bool) (granted, error) {
	entries, who, skip, err := c.facts(ctx, userID, m, opening)
	if err != nil {
		return granted{}, err
	}
	if skip {
		return granted{got: LevelTasks}, nil
	}
	g := granted{got: Grant(m, entries, who), entries: entries, who: who}
	if g.got < LevelTasks && roleOnly(m, entries, who, LevelTasks) {
		// Debug mode: the role lets the person in for a while, with a reason the owner sees.
		open, err := c.DB.S().DebugOpen(ctx, m.ID, userID, c.now())
		if err != nil {
			return granted{}, err
		}
		if open {
			g.got = LevelTasks
		}
	}
	return g, nil
}

// refuse is the answer to a person whose level is below need: nil in warn mode, which only logs.
func (c *Checker) refuse(userID string, m store.Machine, g granted, need Level) error {
	if g.got >= need {
		return nil
	}
	if c.Config.Machines.AccessEnforce == config.AccessWarn {
		log := c.Log
		if log == nil {
			log = slog.Default()
		}
		log.Warn("machine access would be refused", "machine", m.ID, "user", userID, "mode", string(ModeOf(m.CreatedBy, g.entries)), "own_work", g.who.OwnWork, "level", g.got.String(), "need", need.String())
		return nil
	}
	switch {
	case g.got >= LevelView:
		return ErrViewOnly
	case roleOnly(m, g.entries, g.who, need):
		return ErrOwnWork
	}
	return ErrAccess
}

func (c *Checker) check(ctx context.Context, userID string, m store.Machine, opening bool, need Level) error {
	if !c.Enforcing() {
		return nil
	}
	g, err := c.level(ctx, userID, m, opening)
	if err != nil {
		return err
	}
	return c.refuse(userID, m, g, need)
}

// MayDebug says whether the person may open the machine for debugging (docs/hub/own-work.md, «Debug mode»): the owner's
// rule is on for them on it and it is the one thing that keeps them out, since their role (an administrator of the
// installation, a manager of the machine's tenant or team) would have let them in. The creator and the people the
// creator listed open the machine anyway and need no debugging; a person with no role has nothing to debug with.
func (c *Checker) MayDebug(ctx context.Context, userID string, m store.Machine) (bool, error) {
	if !c.Enforcing() || m.CreatedBy == userID {
		return false, nil
	}
	entries, who, skip, err := c.facts(ctx, userID, m, true)
	if err != nil || skip {
		return false, err
	}
	return roleOnly(m, entries, who, LevelTasks), nil
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

// Opens says whether the person may read what the machine holds (its chats, the design projects recorded on it) without
// opening it: View under HUB_ACCESS_ENFORCE=on, the owner's rule included, and yes in off and warn, where nothing is
// hidden. Unlike Check it logs nothing, since lists ask it once per machine and person. A failed lookup is a no.
func (c *Checker) Opens(ctx context.Context, userID string, m store.Machine) bool {
	if !c.Hides() {
		return true
	}
	return c.View(ctx, userID, m) == nil
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
