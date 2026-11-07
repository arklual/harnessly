package apps

import (
	"errors"
	"os"
	"slices"
	"time"
)

// The person's consent. What an app reaches is decided once, on the install screen of the host's page, by a
// click of the person: an agent cannot give an app rights, through the CLI or a channel, since it can drive both. So an
// install the person did not click through waits:
//
//   - a first install that reaches anything is kept off until the person agrees (State waiting, reason consent);
//   - an update that reaches more than the person agreed to waits beside the running version as Pending; without the
//     word the old version keeps running;
//   - an update that reaches no more goes on as before, a narrowing silently.
//
// Installs made before consent was asked keep what they had (Grant nil): rights are never taken back after the fact.

// ConsentFrom is whose word an install carries.
type ConsentFrom int

const (
	// ConsentHost: the host installs on its own (its built-in apps, the organization's environment, an eval); what it
	// installs is not the person's to agree to, so it keeps what it reaches as before.
	ConsentHost ConsentFrom = iota
	// ConsentGiven: the person clicked «Установить» on the install screen, which listed everything the app reaches.
	ConsentGiven
	// ConsentAsk: nobody agreed yet (the CLI, a channel's sync): rights not agreed before wait for the person.
	ConsentAsk
)

// Grant is what the person agreed to.
type Grant struct {
	Keys []string `json:"keys"`
	At   int64    `json:"at"`
}

var (
	// ErrSameVersionGrew: an update reaches more under the same version; a new right comes with a new version only.
	ErrSameVersionGrew = errors.New("apps: the package reaches more than the installed one under the same version; raise its version")
	// ErrNoConsentWaiting: nothing of the app waits for the person's word (or another version does).
	ErrNoConsentWaiting = errors.New("apps: nothing waits for consent")
	// ErrConsentChanged: the package reaches more than the install screen showed (it changed after it was drawn).
	ErrConsentChanged = errors.New("apps: the app reaches more than the install screen showed")
)

// granted are the keys the person agreed to; an install from before consent has what it was installed with.
func (a Installed) granted() []string {
	switch {
	case a.Grant != nil:
		return a.Grant.Keys
	case a.Wants != nil:
		return a.Wants
	}
	// Installed before Wants was kept: what its package reaches now.
	pkg, _ := CachedPackage(a.Dir)
	return AccessKeys(AccessOf(a.Manifest, pkg))
}

// wants is what the install reaches now. A dev install runs the author's folder as it is, so it is read from there:
// an edit that adds a right, a server or a hook takes the app off until the person agrees again. ok is false when the
// folder of a dev install cannot be read: what it reaches is not known, so it waits (fails closed).
func (a Installed) wants() (keys []string, ok bool) {
	if !a.Dev {
		return a.Wants, true
	}
	pkg, _ := CachedPackage(a.Dir)
	if pkg == nil || pkg.Manifest == nil {
		return nil, false
	}
	return AccessKeys(AccessOf(pkg.Manifest, pkg)), true
}

// NeedsConsent: the install reaches what the person did not agree to, so it is kept off.
func (a Installed) NeedsConsent() bool {
	if a.Grant == nil && !(a.Dev && a.Wants != nil) {
		// Installed before consent (or by the host itself): it keeps its rights.
		return false
	}
	wants, ok := a.wants()
	if !ok {
		return true
	}
	if a.Grant != nil {
		return !covered(wants, a.Grant.Keys)
	}
	// A dev install from before consent keeps what it was installed with, not what its folder grows to.
	return !covered(wants, a.Wants)
}

// WaitsConsent: the install or a newer version of it waits for the person's word.
func (a Installed) WaitsConsent() bool { return a.NeedsConsent() || a.Pending != nil }

func covered(wants, granted []string) bool {
	for _, k := range wants {
		if !slices.Contains(granted, k) {
			return false
		}
	}
	return true
}

// grantFor is the grant of a new entry: the host's install keeps the old semantics (nil, all it reaches), the person's
// click grants what it reaches, an install nobody agreed to keeps what was agreed before.
func grantFor(from ConsentFrom, old *Installed, wants []string, at int64) *Grant {
	switch from {
	case ConsentGiven:
		return &Grant{Keys: slices.Clone(wants), At: at}
	case ConsentAsk:
		if old == nil {
			if len(wants) == 0 {
				return &Grant{Keys: []string{}, At: at}
			}
			return &Grant{Keys: []string{}}
		}
		if old.Grant == nil {
			return &Grant{Keys: slices.Clone(old.granted()), At: old.InstalledAt}
		}
		g := *old.Grant
		g.Keys = slices.Clone(g.Keys)
		return &g
	}
	if old != nil && old.Grant != nil {
		return &Grant{Keys: slices.Clone(wants), At: at}
	}
	return nil
}

// Consent is the person's word on what waits: version and shown are the version and the keys the install screen showed,
// so a click cannot agree to a version, or a package, that came after it was drawn. A pending update takes the place of
// the running version; a first install is turned on. What is granted is only what was shown (with what was granted
// before, for an update): a package that reaches more is refused with ErrConsentChanged.
func (r *Registry) Consent(id, version string, shown []string, now time.Time) (Installed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list, err := r.read()
	if err != nil {
		return Installed{}, err
	}
	i := slices.IndexFunc(list, func(a Installed) bool { return a.ID == id })
	if i < 0 {
		return Installed{}, ErrNotInstalled
	}
	a := list[i]
	at := now.UnixMilli()
	switch {
	case a.Pending != nil && a.Pending.Version == version:
		if wants, ok := a.Pending.wants(); !ok || !covered(wants, slices.Concat(a.granted(), shown)) {
			return Installed{}, ErrConsentChanged
		}
		next := *a.Pending
		next.InstalledAt, next.Off = a.InstalledAt, a.Off
		if a.Version != next.Version {
			next.Previous = a.Version
		} else {
			next.Previous = a.Previous
		}
		next.Wants, _ = next.wants()
		next.Grant = &Grant{Keys: slices.Clone(next.Wants), At: at}
		list[i] = next
		if err := r.write(list); err != nil {
			return Installed{}, err
		}
		r.dropCopy(a, next)
		return next, nil
	case a.Pending == nil && a.NeedsConsent() && a.Version == version:
		wants, ok := a.wants()
		if !ok || !covered(wants, shown) {
			return Installed{}, ErrConsentChanged
		}
		a.Wants = wants
		a.Grant = &Grant{Keys: slices.Clone(wants), At: at}
		list[i] = a
		return a, r.write(list)
	}
	return Installed{}, ErrNoConsentWaiting
}

// Decline is the person's «Отмена»: a pending update is dropped and the running version stays; a first install that
// waited is removed.
func (r *Registry) Decline(id string) (removed bool, err error) {
	r.mu.Lock()
	list, err := r.read()
	if err != nil {
		r.mu.Unlock()
		return false, err
	}
	i := slices.IndexFunc(list, func(a Installed) bool { return a.ID == id })
	if i < 0 {
		r.mu.Unlock()
		return false, ErrNotInstalled
	}
	a := list[i]
	switch {
	case a.Pending != nil:
		if !a.Pending.Dev && a.Pending.Dir != a.Dir {
			_ = os.RemoveAll(a.Pending.Dir)
		}
		list[i].Pending = nil
		err = r.write(list)
		r.mu.Unlock()
		return false, err
	case a.NeedsConsent():
		r.mu.Unlock()
		return true, r.Remove(id)
	}
	r.mu.Unlock()
	return false, ErrNoConsentWaiting
}

// SetOff is the switch of an app the person installed: off, it starts in no new chat and its views' calls are refused at
// once (they go through Get). A team's or the environment's app is turned off with MuteTeamApp.
func (r *Registry) SetOff(id string, off bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list, err := r.read()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(a Installed) bool { return a.ID == id })
	if i < 0 {
		return ErrNotInstalled
	}
	list[i].Off = off
	return r.write(list)
}
