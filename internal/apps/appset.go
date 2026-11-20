package apps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// The set of apps a person has installed is the same on every VM of theirs. A VM keeps the set in the person's
// room of the hub's store (filestore.KindAppSet, one room per person, only their own machines open it):
//
//	apps/<id>.json         one record per app: version, the sum of its package, on/off, the settings' values and the
//	                       rights the person agreed to; or a tombstone (removed) so the others take it off too
//	archives/<sum>.tgz     the package, packed (Pack) and addressed by its sha256, so an app installed from a folder of
//	                       one VM is on the others without that folder
//
// Reconcile compares three things per app: what the registry has here (L), the record in the room (R) and what both were
// at the last pass (the state file, <apps>/appset-state.json). Only L changed: publish it. Only R changed: apply it.
// Both: the later change wins. So an install, an update, a removal, a switch or a setting made on one VM reaches the
// others, and nothing is overwritten by a VM that was merely off for a while.
//
// Never in the set: an app installed for development (Dev, the author's live folder: it stays on that VM), the team's and
// the environment's apps (they come by their own ways), the app's secrets (the hub's safe shares them) and its
// stored data. A trusted-tier app runs code on the machine, so it is listed but installed by hand on each VM, as the
// team's are.

const (
	// SetRecordVersion is the version of a record's shape.
	SetRecordVersion = 1
	setStateFile     = "appset-state.json"
	setPrefix        = "apps/"
)

// SetRecord is one app of the person's set as the room keeps it.
type SetRecord struct {
	V       int    `json:"v"`
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	// Sum is the SHA-256 of the packed package at archives/<sum>.tgz.
	Sum     string `json:"sum,omitempty"`
	Removed bool   `json:"removed,omitempty"`
	Off     bool   `json:"off,omitempty"`
	Trust   string `json:"trust,omitempty"`
	// Settings are the values of the app's settings form (the install area's SettingsKey).
	Settings json.RawMessage `json:"settings,omitempty"`
	// Grant is what the person agreed to on the VM that wrote the record.
	Grant *Grant `json:"grant,omitempty"`
	// Source is where the VM that wrote the record installed it from (a note, never read back).
	Source string `json:"source,omitempty"`
	At     int64  `json:"at"`
	By     string `json:"by,omitempty"`
}

// SetPath is the path of an app's record in the room; SetArchivePath that of a package.
func SetPath(id string) string         { return setPrefix + id + ".json" }
func SetArchivePath(sum string) string { return "archives/" + sum + ".tgz" }
func setIDOf(path string) (string, bool) {
	id, ok := strings.CutPrefix(path, setPrefix)
	if !ok {
		return "", false
	}
	id, ok = strings.CutSuffix(id, ".json")
	return id, ok && idRe.MatchString(id)
}

// SetRoom is the person's room (filesync.Client of filestore.KindAppSet).
type SetRoom interface {
	Changes(ctx context.Context, since int64, wait time.Duration) (filestore.Changes, error)
	Current(ctx context.Context, path string) (*filestore.Entry, error)
	GetBlob(ctx context.Context, sha string, w io.Writer) error
	Put(ctx context.Context, path string, data []byte, author string) (filestore.Entry, error)
}

// hash is what two records are compared by: what they say, not when or by whom.
func (r SetRecord) hash() string {
	c := r
	c.At, c.By, c.Source, c.V = 0, "", "", 0
	c.Settings = canonicalJSON(c.Settings)
	if c.Grant != nil {
		keys := slices.Clone(c.Grant.Keys)
		sort.Strings(keys)
		c.Grant = &Grant{Keys: keys, Policy: c.Grant.Policy}
	}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// canonicalJSON is raw with its keys in order, so two spellings of one value compare equal; nil for none.
func canonicalJSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return nil
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return out
}

// setState is what a VM remembers of an app between passes.
type setState struct {
	// RemoteHash is the hash of the record at the last pass; LocalHash that of this VM's own record ("-": the app was
	// not installed here).
	RemoteHash string `json:"remote"`
	LocalHash  string `json:"local"`
	// LocalAt is when this VM's own change was noticed.
	LocalAt int64 `json:"localAt,omitempty"`
}

// AppSet reconciles one VM's registry with the person's set in the hub.
type AppSet struct {
	Reg     *Registry
	Storage *Storage
	Room    SetRoom
	// Machine names this VM in the records it writes.
	Machine string
	// Host is the runtime's version, which a package is checked against.
	Host string
	// CarryConsent: the rights the person agreed to on another VM are agreed here too, as far as they cover what the
	// package reaches (the room is the person's own machines'). Off, an app that reaches anything waits for a click.
	CarryConsent bool
	Log          *slog.Logger
	Now          func() time.Time

	mu     sync.Mutex
	sums   map[string]sumCache
	recs   map[string]SetRecord // by the sha of the record's file
	failed map[string]string    // app -> the record hash that could not be applied
}

type sumCache struct {
	version   string
	updatedAt int64
	dir       string
	sum       string
}

// SetReport says what a pass did.
type SetReport struct {
	Published []string `json:"published,omitempty"`
	Installed []string `json:"installed,omitempty"`
	Updated   []string `json:"updated,omitempty"`
	Removed   []string `json:"removed,omitempty"`
	Changed   []string `json:"changed,omitempty"` // on/off, settings or rights
	// LocalOnly are the apps installed for development: they stay on this VM.
	LocalOnly []string `json:"localOnly,omitempty"`
	Problems  []string `json:"problems,omitempty"`
}

// Empty tells whether the pass changed nothing.
func (r SetReport) Empty() bool {
	return len(r.Published)+len(r.Installed)+len(r.Updated)+len(r.Removed)+len(r.Changed)+len(r.Problems) == 0
}

func (s *AppSet) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *AppSet) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *AppSet) statePath() string { return filepath.Join(s.Reg.Root, setStateFile) }

func (s *AppSet) loadState() map[string]setState {
	out := map[string]setState{}
	if raw, err := os.ReadFile(s.statePath()); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (s *AppSet) saveState(st map[string]setState) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.statePath(), append(raw, '\n'))
}

// readRemote is the room's records by app. A file that is not a record of its app is left out.
func (s *AppSet) readRemote(ctx context.Context) (map[string]SetRecord, error) {
	out := map[string]SetRecord{}
	var since int64
	for {
		ch, err := s.Room.Changes(ctx, since, 0)
		if err != nil {
			return nil, err
		}
		for _, e := range ch.Entries {
			id, ok := setIDOf(e.Path)
			if !ok || e.Deleted || e.Sha == "" {
				delete(out, id)
				continue
			}
			rec, ok := s.recs[e.Sha]
			if !ok {
				var buf bytes.Buffer
				if err := s.Room.GetBlob(ctx, e.Sha, &buf); err != nil {
					return nil, err
				}
				if json.Unmarshal(buf.Bytes(), &rec) != nil {
					continue
				}
				if s.recs == nil {
					s.recs = map[string]SetRecord{}
				}
				s.recs[e.Sha] = rec
			}
			if rec.ID != id || rec.V < 1 || !rec.Removed && (!semverRe.MatchString(rec.Version) || len(rec.Sum) != 64) {
				continue
			}
			out[id] = rec
		}
		if !ch.More || ch.Rev <= since {
			return out, nil
		}
		since = ch.Rev
	}
}

// sumOf is the sum of the installed package: the one it came with from the room, else the pack of its folder.
func (s *AppSet) sumOf(a Installed) (string, error) {
	if c, ok := s.sums[a.ID]; ok && c.version == a.Version && c.updatedAt == a.UpdatedAt && c.dir == a.Dir {
		return c.sum, nil
	}
	_, sum, err := Pack(a.Dir)
	if err != nil {
		return "", err
	}
	s.rememberSum(a, sum)
	return sum, nil
}

func (s *AppSet) rememberSum(a Installed, sum string) {
	if s.sums == nil {
		s.sums = map[string]sumCache{}
	}
	s.sums[a.ID] = sumCache{version: a.Version, updatedAt: a.UpdatedAt, dir: a.Dir, sum: sum}
}

// localRecord is this VM's record of an installed app.
func (s *AppSet) localRecord(a Installed) (SetRecord, error) {
	sum, err := s.sumOf(a)
	if err != nil {
		return SetRecord{}, err
	}
	rec := SetRecord{V: SetRecordVersion, ID: a.ID, Version: a.Version, Sum: sum, Off: a.Off, Source: a.Source}
	if a.Manifest != nil {
		rec.Trust = a.Manifest.Trust
	}
	if a.Grant != nil {
		g := *a.Grant
		g.Keys = slices.Clone(g.Keys)
		rec.Grant = &g
	}
	if s.Storage != nil {
		raw, ok, err := s.Storage.Get(a.ID, ScopeInstall, "", SettingsKey)
		if err != nil {
			return SetRecord{}, err
		}
		if ok {
			rec.Settings = canonicalJSON(raw)
		}
	}
	return rec, nil
}

// Reconcile makes one pass: it publishes what changed here, applies what changed in the room, and says what it did.
func (s *AppSet) Reconcile(ctx context.Context) (SetReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rep SetReport
	remote, err := s.readRemote(ctx)
	if err != nil {
		return rep, err
	}
	installs, err := s.Reg.userInstalls()
	if err != nil {
		return rep, err
	}
	state := s.loadState()
	local := map[string]Installed{}
	dev := map[string]bool{}
	for _, a := range installs {
		if a.Manifest == nil {
			continue
		}
		if a.Dev {
			dev[a.ID] = true
			rep.LocalOnly = append(rep.LocalOnly, a.ID)
			continue
		}
		local[a.ID] = a
	}
	ids := map[string]bool{}
	for id := range remote {
		ids[id] = true
	}
	for id := range local {
		ids[id] = true
	}
	for id := range state {
		ids[id] = true
	}
	order := make([]string, 0, len(ids))
	for id := range ids {
		if !dev[id] {
			order = append(order, id)
		}
	}
	sort.Strings(order)
	defer func() { _ = s.saveState(state) }()
	for _, id := range order {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		s.pass(ctx, id, local[id], remote[id], state, &rep)
	}
	sort.Strings(rep.LocalOnly)
	return rep, nil
}

// pass reconciles one app.
func (s *AppSet) pass(ctx context.Context, id string, inst Installed, rec SetRecord, state map[string]setState, rep *SetReport) {
	problem := func(format string, a ...any) {
		rep.Problems = append(rep.Problems, id+": "+fmt.Sprintf(format, a...))
	}
	st, known := state[id]
	hasLocal := inst.ID == id
	var mine SetRecord
	lh := ""
	switch {
	case hasLocal:
		var err error
		if mine, err = s.localRecord(inst); err != nil {
			problem("not read for the set: %v", err)
			return
		}
		lh = mine.hash()
	case known:
		lh = "-"
	}
	rh := ""
	if rec.ID != "" {
		rh = rec.hash()
	}
	if lh == rh && lh != "" && lh != "-" {
		state[id] = setState{RemoteHash: rh, LocalHash: lh, LocalAt: st.LocalAt}
		return
	}
	if !hasLocal && rec.Removed && !known {
		state[id] = setState{RemoteHash: rh, LocalHash: "-"}
		return
	}
	localChanged := lh != "" && lh != st.LocalHash
	remoteChanged := rh != st.RemoteHash
	localAt := st.LocalAt
	if localChanged {
		localAt = s.now().UnixMilli()
		if !known && hasLocal {
			localAt = inst.UpdatedAt
		}
	}
	switch {
	case !localChanged && !remoteChanged:
		return
	case localChanged && remoteChanged && rec.ID != "" && rec.At > localAt:
		localChanged = false
	}
	if localChanged {
		if err := s.publish(ctx, id, inst, hasLocal, mine, state, localAt); err != nil {
			problem("not put into the person's set: %v", err)
			return
		}
		rep.Published = append(rep.Published, id)
		return
	}
	if rec.ID == "" {
		return
	}
	if s.failed[id] == rh {
		return
	}
	s.apply(ctx, id, inst, hasLocal, mine, rec, rh, state, localAt, rep, problem)
}

// publish puts this VM's record of the app, or the tombstone of an app removed here, into the room.
func (s *AppSet) publish(ctx context.Context, id string, inst Installed, hasLocal bool, mine SetRecord, state map[string]setState, localAt int64) error {
	rec := SetRecord{V: SetRecordVersion, ID: id, Removed: true}
	lh := "-"
	if hasLocal {
		rec = mine
		lh = mine.hash()
		if err := s.putArchive(ctx, inst, &rec); err != nil {
			return err
		}
	}
	rec.At, rec.By = s.now().UnixMilli(), s.Machine
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := s.Room.Put(ctx, SetPath(id), raw, s.Machine); err != nil {
		return err
	}
	state[id] = setState{RemoteHash: rec.hash(), LocalHash: lh, LocalAt: localAt}
	if hasLocal && rec.Sum != mine.Sum {
		s.rememberSum(inst, rec.Sum)
		state[id] = setState{RemoteHash: rec.hash(), LocalHash: rec.hash(), LocalAt: localAt}
	}
	s.log().Info("apps: the person's set updated", "app", id, "removed", rec.Removed, "version", rec.Version)
	return nil
}

// putArchive makes sure the room has the package of rec; a package this VM cannot pack stops the app from being shared.
func (s *AppSet) putArchive(ctx context.Context, inst Installed, rec *SetRecord) error {
	path := SetArchivePath(rec.Sum)
	if e, err := s.Room.Current(ctx, path); err != nil {
		return err
	} else if e != nil && !e.Deleted && e.Sha == rec.Sum {
		return nil
	}
	raw, sum, err := Pack(inst.Dir)
	if err != nil {
		return fmt.Errorf("the package cannot travel: %w", err)
	}
	rec.Sum = sum
	_, err = s.Room.Put(ctx, SetArchivePath(sum), raw, s.Machine)
	return err
}

func (s *AppSet) fetchArchive(ctx context.Context, sum string) ([]byte, error) {
	e, err := s.Room.Current(ctx, SetArchivePath(sum))
	if err != nil {
		return nil, err
	}
	if e == nil || e.Deleted || e.Sha == "" {
		return nil, fmt.Errorf("the room has no package %s", sum)
	}
	var buf bytes.Buffer
	if err := s.Room.GetBlob(ctx, e.Sha, &buf); err != nil {
		return nil, err
	}
	if ArchiveSum(buf.Bytes()) != sum {
		return nil, fmt.Errorf("the package in the room is not the one %s names", sum[:12])
	}
	return buf.Bytes(), nil
}

// apply makes this VM's app what the record says.
func (s *AppSet) apply(ctx context.Context, id string, inst Installed, hasLocal bool, mine SetRecord, rec SetRecord, rh string,
	state map[string]setState, localAt int64, rep *SetReport, problem func(string, ...any)) {
	fail := func(format string, a ...any) {
		if s.failed == nil {
			s.failed = map[string]string{}
		}
		s.failed[id] = rh
		problem(format, a...)
	}
	done := func() {
		delete(s.failed, id)
		lh := "-"
		if cur, ok := s.installed(id); ok {
			if r, err := s.localRecord(cur); err == nil {
				lh = r.hash()
			}
		}
		state[id] = setState{RemoteHash: rh, LocalHash: lh, LocalAt: localAt}
	}
	if rec.Removed {
		if hasLocal {
			if err := s.Reg.Uninstall(id, false, nil); err != nil && !errors.Is(err, ErrNotInstalled) {
				fail("not removed: %v", err)
				return
			}
			rep.Removed = append(rep.Removed, id)
			s.journal(id, "appset.remove", "")
		}
		done()
		return
	}
	if rec.Trust == TrustTrusted {
		fail("a trusted app runs code on the machine: install it by hand on this VM (harnessly app install)")
		return
	}
	cur, have := inst, hasLocal
	if !hasLocal || inst.Version != rec.Version || mine.Sum != rec.Sum {
		raw, err := s.fetchArchive(ctx, rec.Sum)
		if err != nil {
			fail("%v", err)
			return
		}
		got, ps, err := s.install(raw, rec)
		if err != nil {
			fail("not installed from the person's set: %v%s", err, problemsNote(ps))
			return
		}
		s.rememberSum(got, rec.Sum)
		if have {
			rep.Updated = append(rep.Updated, id)
		} else {
			rep.Installed = append(rep.Installed, id)
		}
		s.journal(id, "appset.install", rec.Version)
		cur = got
	}
	changed := false
	if cur.Off != rec.Off {
		if err := s.Reg.SetDisabled(id, rec.Off); err != nil {
			fail("not switched: %v", err)
			return
		}
		changed = true
	}
	if len(canonicalJSON(rec.Settings)) > 0 && s.Storage != nil {
		old, ok, err := s.Storage.Get(id, ScopeInstall, "", SettingsKey)
		if err != nil {
			fail("settings not read: %v", err)
			return
		}
		if !ok || !bytes.Equal(canonicalJSON(old), canonicalJSON(rec.Settings)) {
			var vals map[string]any
			if err := json.Unmarshal(rec.Settings, &vals); err != nil {
				fail("settings of the set are not values: %v", err)
				return
			}
			if bad := cur.Manifest.CheckSettings(vals); bad != nil {
				fail("settings of the set do not fit this version: %v", bad)
				return
			}
			if err := s.Storage.Set(id, ScopeInstall, "", SettingsKey, rec.Settings); err != nil {
				fail("settings not kept: %v", err)
				return
			}
			changed = true
		}
	}
	if s.CarryConsent && rec.Grant != nil && !rec.Grant.Policy && len(rec.Grant.Keys) > 0 {
		if latest, ok := s.installed(id); ok && latest.WaitsConsent() {
			waiting := latest.Version
			if latest.Pending != nil {
				waiting = latest.Pending.Version
			}
			if waiting == rec.Version {
				// Consent refuses what the package reaches beyond the keys agreed to: nothing grows by the way.
				if _, err := s.Reg.Consent(id, waiting, rec.Grant.Keys, s.now()); err == nil {
					changed = true
					s.journal(id, "appset.consent", waiting)
				}
			}
		}
	}
	if changed && !slices.Contains(rep.Installed, id) && !slices.Contains(rep.Updated, id) {
		rep.Changed = append(rep.Changed, id)
	}
	done()
}

func problemsNote(ps Problems) string {
	if len(ps.Errors()) == 0 {
		return ""
	}
	return " (" + ps.Error() + ")"
}

func (s *AppSet) installed(id string) (Installed, bool) {
	list, err := s.Reg.userInstalls()
	if err != nil {
		return Installed{}, false
	}
	for _, a := range list {
		if a.ID == id {
			return a, true
		}
	}
	return Installed{}, false
}

func (s *AppSet) journal(id, what, detail string) {
	_ = s.Reg.Journal(id, JournalEntry{At: s.now().UnixMilli(), What: what, Class: ClassHigh, OK: true, Detail: detail})
}

// install unpacks the package and installs it as the person's other VM did, with no rights beyond what was agreed.
func (s *AppSet) install(raw []byte, rec SetRecord) (Installed, Problems, error) {
	if _, err := VerifyArchive(raw, nil, currentNetwork()); err != nil {
		return Installed{}, nil, err
	}
	tmpRoot := filepath.Join(s.Reg.Root, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return Installed{}, nil, err
	}
	work, err := os.MkdirTemp(tmpRoot, "appset-")
	if err != nil {
		return Installed{}, nil, err
	}
	defer os.RemoveAll(work)
	dir := filepath.Join(work, "pkg")
	if err := Unpack(raw, dir); err != nil {
		return Installed{}, nil, err
	}
	pkg, ps := LoadPackage(dir)
	switch {
	case pkg == nil:
		return Installed{}, ps, ErrInvalid
	case pkg.Manifest.ID != rec.ID || pkg.Manifest.Version != rec.Version:
		return Installed{}, ps, fmt.Errorf("the package is %s %s, the record says %s %s", pkg.Manifest.ID, pkg.Manifest.Version, rec.ID, rec.Version)
	case pkg.Manifest.Trust == TrustTrusted:
		return Installed{}, ps, errors.New("a trusted app is installed by hand")
	}
	return s.Reg.Install(dir, InstallOptions{HostVersion: s.Host, Consent: ConsentAsk, Source: "the person's set (from " + rec.By + ")", Now: s.now})
}
