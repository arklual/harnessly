package apps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// The public catalog: the apps their authors offer to everybody. Anyone browses it and installs from it for themselves,
// no team needed; it is the team's catalog (team.go) made wider, on the same machinery (the package as an archive with its
// SHA-256, Pack and Unpack, the channels), and the same line holds:
//
//   - The trusted tier is in no catalog. It runs code on the machine, so each person installs it by hand.
//   - What goes in is a version on stable, and it passes the checks first (publishChecks): the strict package check, a
//     license, no executables in a package that is not trusted.
//   - An app that asks for the whole network (network: *) is not listed until a person at the platform has looked at it
//     (ReviewPublic). The others are listed at once: the checks are the machine's, and the author answers for the rest.
//   - Installing from the catalog is a person's choice for themselves: they are shown the rights first, and the install
//     names the archive they were shown (the sum), so an entry that changed in between is not installed unseen.
//   - The platform sends a machine the reviewed list (ApplyPublic); the machine keeps it in <apps>/public.json, so a
//     platform that is down does not empty the catalog.

var (
	// ErrTrustedPublic: a trusted app is installed by hand, never from a catalog.
	ErrTrustedPublic = errors.New("apps: a trusted app is installed by hand, it is not in the public catalog")
	// ErrNotOnStable: only the version a person promoted to stable goes public.
	ErrNotOnStable = errors.New("apps: only the version on stable goes to the public catalog")
	// ErrPublishChecks: the package does not pass what the public catalog asks.
	ErrPublishChecks = errors.New("apps: the package does not pass the public catalog's checks")
	// ErrNotPublic: the public catalog has no such app, or it is not listed yet.
	ErrNotPublic = errors.New("apps: the public catalog has no such app")
	// ErrPublicChanged: the entry is not the one the person was shown.
	ErrPublicChanged = errors.New("apps: the catalog's entry changed since it was shown")
)

// Review states of a public entry.
const (
	ReviewApproved = "approved"
	ReviewPending  = "pending"
)

// PublicEntry is an app in the public catalog.
type PublicEntry struct {
	CatalogEntry
	// Review is approved (listed) or pending (waits for a person at the platform).
	Review string `json:"review"`
	// Why says what asks for the look, when Review is pending.
	Why []string `json:"why,omitempty"`
}

type publicFile struct {
	At      int64         `json:"at"`
	Entries []PublicEntry `json:"entries"`
	// PlatformAt is the time of the last list the platform sent: a list older than it is stale. At also moves when
	// this machine publishes, and a list made before that publish is not stale for it.
	PlatformAt int64 `json:"platformAt,omitempty"`
	// Own are the entries published on this machine (app → version) that the platform's list has not shown yet.
	Own map[string]string `json:"own,omitempty"`
}

func (r *Registry) publicFile() string { return filepath.Join(r.Root, "public.json") }

func (r *Registry) readPublic() (publicFile, error) {
	var p publicFile
	raw, err := os.ReadFile(r.publicFile())
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(raw, &p)
}

func (r *Registry) writePublic(p publicFile) error {
	sort.Slice(p.Entries, func(i, j int) bool { return p.Entries[i].ID < p.Entries[j].ID })
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(r.publicFile(), append(raw, '\n'))
}

func (r *Registry) publicArchive(id, version string) string {
	return filepath.Join(r.Root, "public", id, version+".tgz")
}

func (r *Registry) publicPackageDir(id, version string) string {
	return filepath.Join(r.Root, "public-packages", id, version)
}

// licenseFiles are the names a package's license is under.
var licenseFiles = []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING", "COPYING.md"}

// publishChecks are what the public catalog asks of a package beyond a clean install, as the reasons it fails. They run
// on the machine, with no person: the strict check (a warning is an error), a license, and no executable in a package that
// is not trusted (a binary is code the sandbox never meant to carry).
func publishChecks(p *Package, ps Problems) []string {
	var out []string
	for _, pr := range ps {
		out = append(out, pr.String())
	}
	m := p.Manifest
	if m.Trust == TrustTrusted {
		return append(out, "a trusted app is not published to a catalog")
	}
	has := false
	for _, name := range licenseFiles {
		if _, err := os.Stat(filepath.Join(p.Dir, name)); err == nil {
			has = true
		}
	}
	if !has {
		out = append(out, "no license: add a LICENSE file, people install what they may use")
	}
	out = append(out, archiveNameProblems(p.Dir)...)
	_ = filepath.WalkDir(p.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(p.Dir, path)
		if executable(path, d) {
			out = append(out, fmt.Sprintf("%s is an executable: a package that is not trusted carries no binaries", filepath.ToSlash(rel)))
		}
		return nil
	})
	return out
}

// executable says whether a file is a native program: it starts with the magic of an ELF, Mach-O or PE binary. A script
// with the execute bit is text a person can read in the review, not a binary.
func executable(path string, d fs.DirEntry) bool {
	info, err := d.Info()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := f.Read(head)
	head = head[:n]
	for _, magic := range [][]byte{{0x7f, 'E', 'L', 'F'}, {0xfe, 0xed, 0xfa, 0xce}, {0xfe, 0xed, 0xfa, 0xcf}, {0xce, 0xfa, 0xed, 0xfe},
		{0xcf, 0xfa, 0xed, 0xfe}, {0xca, 0xfe, 0xba, 0xbe}, {'M', 'Z'}} {
		if bytes.HasPrefix(head, magic) {
			return true
		}
	}
	return false
}

// reviewReasons are why an entry waits for a look: the whole network asked for.
func reviewReasons(m *Manifest) []string {
	var why []string
	if n := m.Network; n != nil {
		for field, hosts := range map[string][]string{"connect": n.Connect, "resources": n.Resources, "frames": n.Frames} {
			if contains(hosts, "*") {
				why = append(why, "network."+field+": * (any host)")
			}
		}
	}
	sort.Strings(why)
	return why
}

// PublishPublic puts the version on stable into the public catalog. An author's own act, from the machine they publish on.
func (r *Registry) PublishPublic(id, version, by string, now time.Time) (PublicEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.readChannels(id)
	if err != nil {
		return PublicEntry{}, err
	}
	if _, ok := state.Versions[version]; !ok {
		return PublicEntry{}, fmt.Errorf("%w: %s %s", ErrNoVersion, id, version)
	}
	if p := state.Channels[string(ChannelStable)]; p == nil || p.Current != version {
		return PublicEntry{}, fmt.Errorf("%w: %s %s is not what stable points at", ErrNotOnStable, id, version)
	}
	pkg, ps := LoadPackage(r.VersionDir(id, version))
	if pkg == nil || len(ps.Errors()) > 0 {
		return PublicEntry{}, ErrInvalid
	}
	if pkg.Manifest.Trust == TrustTrusted {
		return PublicEntry{}, ErrTrustedPublic
	}
	if failed := publishChecks(pkg, ps); len(failed) > 0 {
		return PublicEntry{}, fmt.Errorf("%w: %s", ErrPublishChecks, strings.Join(failed, "; "))
	}
	raw, sum, err := r.publishedArchive(id, version, pkg.Dir)
	if err != nil {
		return PublicEntry{}, err
	}
	if err := writeFileAtomic(r.publicArchive(id, version), raw); err != nil {
		return PublicEntry{}, err
	}
	keepSig(r.publicArchive(id, version), raw, r.versionSig(id, version))
	m := pkg.Manifest
	e := PublicEntry{CatalogEntry: CatalogEntry{
		ID: id, Channel: string(ChannelStable), Version: version, Name: m.Name, Description: m.Description, Trust: m.Trust,
		Targets: nonNil(m.Targets), Permissions: nonNil(m.Permissions), By: by, At: now.UnixMilli(), Sum: sum,
	}, Review: ReviewApproved}
	e.describe(m)
	if why := reviewReasons(m); len(why) > 0 {
		e.Review, e.Why = ReviewPending, why
	}
	p, err := r.readPublic()
	if err != nil {
		return PublicEntry{}, err
	}
	replaced := false
	for i := range p.Entries {
		if p.Entries[i].ID == id {
			p.Entries[i], replaced = e, true
		}
	}
	if !replaced {
		p.Entries = append(p.Entries, e)
	}
	p.At = now.UnixMilli()
	if p.Own == nil {
		p.Own = map[string]string{}
	}
	p.Own[id] = version
	if err := r.writePublic(p); err != nil {
		return e, err
	}
	if r.onPublicChange != nil {
		r.onPublicChange()
	}
	return e, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// PublicCatalog is what anyone browses: the entries that are reviewed, narrowed by a query (a word of the id, the name or
// the description, any case), by name. The trusted tier is never in it, whatever a file says.
func (r *Registry) PublicCatalog(query string) ([]PublicEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.readPublic()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := []PublicEntry{}
	for _, e := range p.Entries {
		if e.Review != ReviewApproved || e.Trust == TrustTrusted {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.ID+" "+e.Name+" "+e.Description), q) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// PendingPublic are the entries that wait for a look, for the platform's reviewers.
func (r *Registry) PendingPublic() ([]PublicEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.readPublic()
	if err != nil {
		return nil, err
	}
	out := []PublicEntry{}
	for _, e := range p.Entries {
		if e.Review == ReviewPending {
			out = append(out, e)
		}
	}
	return out, nil
}

// ReviewPublic is the look of a person at the platform: approving lists the entry, refusing takes it out.
func (r *Registry) ReviewPublic(id, version string, approve bool, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.readPublic()
	if err != nil {
		return err
	}
	for i := range p.Entries {
		e := &p.Entries[i]
		if e.ID != id || e.Version != version || e.Review != ReviewPending {
			continue
		}
		if approve {
			e.Review, e.Why = ReviewApproved, nil
		} else {
			p.Entries = append(p.Entries[:i], p.Entries[i+1:]...)
		}
		p.At = now.UnixMilli()
		return r.writePublic(p)
	}
	return fmt.Errorf("%w: %s %s waits for no review", ErrNotPublic, id, version)
}

// PublicInstallOptions say how a person installs from the public catalog.
type PublicInstallOptions struct {
	HostVersion string
	// Sum is the archive of the entry the person was shown. An entry that has another is not installed.
	Sum string
	// Consent is whose word the install carries (consent.go); with ConsentGiven, Granted are the keys the install screen
	// listed (nil: all the package reaches).
	Consent ConsentFrom
	Granted []string
}

// InstallFromPublic installs a listed app for the person, from the archive the catalog names. The install is the
// person's own, like any: it can be removed, and it follows no channel.
func (r *Registry) InstallFromPublic(id string, opt PublicInstallOptions) (Installed, Problems, error) {
	r.mu.Lock()
	p, err := r.readPublic()
	r.mu.Unlock()
	if err != nil {
		return Installed{}, nil, err
	}
	var e *PublicEntry
	for i := range p.Entries {
		if p.Entries[i].ID == id && p.Entries[i].Review == ReviewApproved {
			e = &p.Entries[i]
		}
	}
	switch {
	case e == nil:
		return Installed{}, nil, fmt.Errorf("%w: %s", ErrNotPublic, id)
	case e.Trust == TrustTrusted:
		return Installed{}, nil, ErrTrustedPublic
	case opt.Sum == "" || opt.Sum != e.Sum:
		return Installed{}, nil, fmt.Errorf("%w: %s", ErrPublicChanged, id)
	}
	raw, err := os.ReadFile(r.publicArchive(id, e.Version))
	if err != nil {
		return Installed{}, nil, fmt.Errorf("%w: %s %s is not on this machine", ErrNotPublic, id, e.Version)
	}
	// The install is made from a copy of this call's own: the shared folder of the catalog can be swapped by another unpack
	// between the check and the install, a folder nobody else knows cannot.
	work, err := os.MkdirTemp(filepath.Join(r.Root, "tmp"), "install-")
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Join(r.Root, "tmp"), 0o755); mkErr != nil {
			return Installed{}, nil, mkErr
		}
		if work, err = os.MkdirTemp(filepath.Join(r.Root, "tmp"), "install-"); err != nil {
			return Installed{}, nil, err
		}
	}
	defer os.RemoveAll(work)
	// The signature is checked whatever the host version says (an empty one skips only the host's range check).
	verified, err := VerifyArchive(raw, sigOf(r.publicArchive(id, e.Version)), currentNetwork())
	if err != nil {
		return Installed{}, nil, fmt.Errorf("%s %s: %w", id, e.Version, err)
	}
	pkg, err := unpackArchive(raw, id, e.Version, e.Sum, opt.HostVersion, filepath.Join(work, "pkg"), sigOf(r.publicArchive(id, e.Version)))
	if errors.Is(err, ErrTrustedTeam) {
		return Installed{}, nil, ErrTrustedPublic
	}
	if err != nil {
		return Installed{}, nil, err
	}
	// What is installed is what the person was shown: the rights and the tier of the unpacked manifest are the entry's.
	if pkg.Manifest.Trust != e.Trust || !sameSet(pkg.Manifest.Permissions, e.Permissions) {
		return Installed{}, nil, fmt.Errorf("%w: %s asks for other rights than were shown", ErrPublicChanged, id)
	}
	return r.Install(pkg.Dir, InstallOptions{HostVersion: opt.HostVersion, Source: r.publicPackageDir(id, e.Version), Consent: opt.Consent, Granted: opt.Granted, Signed: verified.Signed})
}

// PublicSnapshot is what the platform sends a machine: the reviewed list with the archives, or what a machine sends the
// platform to be reviewed and passed on.
type PublicSnapshot struct {
	At      int64                 `json:"at"`
	Entries []PublicSnapshotEntry `json:"entries"`
}

// PublicSnapshotEntry is a public entry on the wire.
type PublicSnapshotEntry struct {
	PublicEntry
	Archive []byte `json:"archive,omitempty"`
	// Signature is the publisher's signature of the archive, when it has one.
	Signature *Signature `json:"signature,omitempty"`
}

// Snapshot is this machine's public entries with their archives, for the platform to review and send on.
func (r *Registry) PublicSnapshot() (PublicSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.readPublic()
	if err != nil {
		return PublicSnapshot{}, err
	}
	out := PublicSnapshot{At: p.At, Entries: []PublicSnapshotEntry{}}
	for _, e := range p.Entries {
		raw, err := os.ReadFile(r.publicArchive(e.ID, e.Version))
		if err != nil {
			return PublicSnapshot{}, err
		}
		out.Entries = append(out.Entries, PublicSnapshotEntry{PublicEntry: e, Archive: raw, Signature: sigOf(r.publicArchive(e.ID, e.Version))})
	}
	return out, nil
}

// Validate refuses a snapshot this machine must not act on.
func (s PublicSnapshot) Validate() error {
	if s.At <= 0 {
		return errors.New("a snapshot has a time")
	}
	if len(s.Entries) > MaxSnapshotEntries {
		return errors.New("too many entries")
	}
	seen := map[string]bool{}
	for _, e := range s.Entries {
		if !idRe.MatchString(e.ID) || !semverRe.MatchString(e.Version) {
			return fmt.Errorf("entry %q %q is not an app and a version", e.ID, e.Version)
		}
		if seen[e.ID] {
			return fmt.Errorf("entry %s is listed twice", e.ID)
		}
		seen[e.ID] = true
		if e.Review != ReviewApproved && e.Review != ReviewPending {
			return fmt.Errorf("entry %s: %q is not approved or pending", e.ID, e.Review)
		}
	}
	return nil
}

// ApplyPublic takes the platform's list as this machine's public catalog. An entry whose archive is not the one its sum
// names, or holds a trusted app, is left out; the problems come back and the rest applies.
func (r *Registry) ApplyPublic(s PublicSnapshot, host string, now time.Time) ([]string, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	have, err := r.readPublic()
	if err != nil {
		return nil, err
	}
	if s.At < have.PlatformAt {
		return nil, ErrStaleTeam
	}
	var problems []string
	next := publicFile{At: s.At, PlatformAt: s.At, Entries: []PublicEntry{}}
	for _, se := range s.Entries {
		e := se.PublicEntry
		fail := func(format string, a ...any) {
			problems = append(problems, fmt.Sprintf("%s %s: %s", e.ID, e.Version, fmt.Sprintf(format, a...)))
		}
		if e.Trust == TrustTrusted {
			fail("a trusted app is not in the public catalog")
			continue
		}
		raw := se.Archive
		if raw == nil {
			// No bytes came: the entry stands only if this machine already holds that version.
			if raw, err = os.ReadFile(r.publicArchive(e.ID, e.Version)); err != nil {
				fail("no archive")
				continue
			}
		}
		// The archive is judged by what is in it, not by what the entry says it is: the manifest decides the tier.
		// Every field the person is shown comes from the verified archive, never from the snapshot's entry. The host is not
		// asked here: an entry this host cannot run is still listed, and the install says so.
		pkg, err := unpackArchive(raw, e.ID, e.Version, e.Sum, "", r.publicPackageDir(e.ID, e.Version), nil)
		if err != nil {
			fail("%v", err)
			continue
		}
		m := pkg.Manifest
		e.Name, e.Description, e.Trust, e.Targets, e.Permissions = m.Name, m.Description, m.Trust, nonNil(m.Targets), nonNil(m.Permissions)
		if err := writeFileAtomic(r.publicArchive(e.ID, e.Version), raw); err != nil {
			fail("%v", err)
			continue
		}
		if se.Archive != nil {
			keepSig(r.publicArchive(e.ID, e.Version), raw, se.Signature)
		}
		next.Entries = append(next.Entries, e)
	}
	// What this machine published after the list was made may not be in it yet (the platform has not read this
	// machine's snapshot): it stays until a later list still lacks it.
	for _, e := range have.Entries {
		if have.Own[e.ID] != e.Version {
			continue
		}
		i := slices.IndexFunc(next.Entries, func(x PublicEntry) bool { return x.ID == e.ID })
		if i >= 0 && next.Entries[i].Version == e.Version {
			continue // the platform has it now
		}
		if e.At <= s.At {
			continue // the list was made after the publish and lacks it: the platform dropped it
		}
		if _, err := os.Stat(r.publicArchive(e.ID, e.Version)); err != nil {
			continue
		}
		if i >= 0 {
			next.Entries[i] = e
		} else {
			next.Entries = append(next.Entries, e)
		}
		if next.Own == nil {
			next.Own = map[string]string{}
		}
		next.Own[e.ID] = e.Version
		next.At = max(next.At, e.At)
	}
	return problems, r.writePublic(next)
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
