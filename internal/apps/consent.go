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
//
// The organization's rule «Не спрашивать разрешений» stands for the person's word: with it on (SetConsentPolicy
// says so), what would wait gets its rights at once, and what waited when the rule came on is granted by GrantWaiting.
// The rule comes only from the hub, where only the organization's admin changes it by a person's session, so an agent
// cannot turn it on from the machine. Turned off, it takes nothing back: the next asks wait for the person again.

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
	// ConsentPolicy: the organization's rule gives the app what it asks for (an install that would be ConsentAsk while
	// the rule is on). The journal says so: consent.policy.
	ConsentPolicy
)

// JournalConsentPolicy is the journal's line of rights given by the organization's rule; its detail is the version.
const JournalConsentPolicy = "consent.policy"

// Grant is what the person agreed to.
type Grant struct {
	Keys []string `json:"keys"`
	At   int64    `json:"at"`
	// Policy: the rights were given by the organization's rule, not by a click.
	Policy bool `json:"policy,omitempty"`
}

// SetConsentPolicy sets what says whether the organization's rule «Не спрашивать разрешений» is on; nil (the default,
// a machine without a hub) asks the person. It is called with the registry's lock held, so it must not use the registry.
func (r *Registry) SetConsentPolicy(on func() bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consentPolicy = on
}

func (r *Registry) policyOnLocked() bool { return r.consentPolicy != nil && r.consentPolicy() }

// GrantWaiting gives what waits for the person its rights by the organization's rule: a pending update takes the place
// of the running version, a first install or a dev folder that grew is turned on. It is called when the rule comes on
// and, while it is on, for the dev folders that grow; with the rule off it does nothing. Each grant is in the journal.
func (r *Registry) GrantWaiting(now time.Time) ([]Installed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.policyOnLocked() {
		return nil, nil
	}
	list, err := r.read()
	if err != nil {
		return nil, err
	}
	at := now.UnixMilli()
	var granted []Installed
	var dropped [][2]Installed
	for i, a := range list {
		switch {
		case a.Pending != nil:
			wants, ok := a.Pending.wants()
			if !ok {
				continue
			}
			next := *a.Pending
			next.InstalledAt, next.Off, next.AskedIn = a.InstalledAt, a.Off, a.AskedIn
			if a.Version != next.Version {
				next.Previous = a.Version
			} else {
				next.Previous = a.Previous
			}
			next.Wants = wants
			next.Grant = &Grant{Keys: slices.Clone(wants), At: at, Policy: true}
			list[i] = next
			granted = append(granted, next)
			dropped = append(dropped, [2]Installed{a, next})
		case a.NeedsConsent():
			wants, ok := a.wants()
			if !ok {
				continue
			}
			a.Wants = wants
			a.Grant = &Grant{Keys: slices.Clone(wants), At: at, Policy: true}
			list[i] = a
			granted = append(granted, a)
		}
	}
	if len(granted) == 0 {
		return nil, nil
	}
	if err := r.write(list); err != nil {
		return nil, err
	}
	for _, d := range dropped {
		r.dropCopy(d[0], d[1])
	}
	for _, a := range granted {
		_ = r.Journal(a.ID, JournalEntry{At: at, What: JournalConsentPolicy, Class: ClassHigh, OK: true, Detail: a.Version})
	}
	return granted, nil
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
	case ConsentPolicy:
		return &Grant{Keys: slices.Clone(wants), At: at, Policy: true}
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

// LiveManifest is the manifest the install runs now: a dev install's folder as it is (the author edits it after the
// install, and NeedsConsent reads it from there), any other install the manifest it was installed with. A dev folder
// that cannot be read leaves the installed one.
func (a Installed) LiveManifest() *Manifest {
	if a.Dev {
		if pkg, _ := CachedPackage(a.Dir); pkg != nil && pkg.Manifest != nil {
			return pkg.Manifest
		}
	}
	return a.Manifest
}

// AccessNow is everything the install reaches now, as the install screen lists it: what a click on it grants. It is
// read from where NeedsConsent reads, so the screen never shows less than the consent asks for.
func (a Installed) AccessNow() []Access {
	pkg, _ := CachedPackage(a.Dir)
	return AccessOf(a.LiveManifest(), pkg)
}

// WaitingAccess is what waits for the person's word: what an install kept off reaches beyond what was granted, or
// what a waiting update adds; empty when nothing waits.
func (a Installed) WaitingAccess() []Access {
	switch {
	case a.NeedsConsent():
		granted := []string{}
		if a.Grant != nil {
			granted = a.Grant.Keys
		} else if a.Dev && a.Wants != nil {
			granted = a.Wants
		}
		return NewAccess(a.AccessNow(), granted)
	case a.Pending != nil:
		next, _ := CachedPackage(a.Pending.Dir)
		return NewAccess(AccessOf(a.Pending.LiveManifest(), next), a.granted())
	}
	return []Access{}
}

// AccessCodes are the codes of a list, as the manifest writes them: what a line of the CLI names.
func AccessCodes(list []Access) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		out = append(out, x.Code)
	}
	return out
}

// ConsentWhere says where the person gives the word an app waits for: only a click of theirs in the host's page, since
// the CLI is every agent's too.
func ConsentWhere(id string) string {
	return "the person agrees in harnessly's page: «Приложения» → " + id + " → «Посмотреть доступы»"
}
