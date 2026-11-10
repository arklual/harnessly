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
	"sort"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// HRN-898. Under a hub the team's apps travel through the space's apps collection of the hub's store, not only
// through the platform's snapshot: an app installed for the space reaches every machine of it, and a machine that
// joins (or was off) finds it there, whichever machine installed it is on or off. A machine whose team changed (an
// owner's install or uninstall, a publish, a rollback) writes the team (TeamStateFile) and the packages' archives
// (team/archives/<id>/<version>.tgz, by their sha256); every machine applies the team it reads there as it applies the
// platform's snapshot (ApplyTeam: the newer At wins, archives checked against their sums), keeping its own person's
// role. On a hub that also relays the platform's snapshot the two meet in ApplyTeam.

// TeamStateFile is the team in the apps collection.
const TeamStateFile = "team/state.json"

// TeamClient is the store's API the team speaks (filesync.Client).
type TeamClient interface {
	Current(ctx context.Context, path string) (*filestore.Entry, error)
	GetBlob(ctx context.Context, sha string, w io.Writer) error
	Commit(ctx context.Context, req filestore.CommitRequest) (filestore.CommitResponse, error)
	PutBlob(ctx context.Context, sha string, data []byte) error
	Put(ctx context.Context, path string, data []byte, author string) (filestore.Entry, error)
}

// storedTeam is the team as the store keeps it: the snapshot without the person's role and without the archives,
// which are files of their own.
type storedTeam struct {
	Space    string            `json:"space"`
	At       int64             `json:"at"`
	Catalog  []CatalogEntry    `json:"catalog"`
	Installs []SnapshotInstall `json:"installs"`
}

func teamArchivePath(id, version string) string {
	return "team/archives/" + id + "/" + version + ".tgz"
}

// PublishTeam writes this machine's team into the store: the archives the store does not hold yet, then the team, on
// top of the one there unless that one is newer (then this machine takes it when it follows the store).
func (r *Registry) PublishTeam(ctx context.Context, c TeamClient) error {
	snap, err := r.Snapshot()
	if err != nil {
		return err
	}
	if snap.Space == "" {
		return nil
	}
	put := func(id, version string, raw []byte) error {
		if raw == nil {
			return nil
		}
		path := teamArchivePath(id, version)
		sum := sha256.Sum256(raw)
		if e, err := c.Current(ctx, path); err == nil && e != nil && !e.Deleted && e.Sha == hex.EncodeToString(sum[:]) {
			return nil
		}
		_, err := c.Put(ctx, path, raw, "apps-team")
		return err
	}
	st := storedTeam{Space: snap.Space, At: snap.At, Catalog: []CatalogEntry{}, Installs: []SnapshotInstall{}}
	for _, e := range snap.Catalog {
		if err := put(e.ID, e.Version, e.Archive); err != nil {
			return err
		}
		if e.Previous != "" {
			if err := put(e.ID, e.Previous, e.PreviousArchive); err != nil {
				return err
			}
		}
		st.Catalog = append(st.Catalog, e.CatalogEntry)
	}
	for _, i := range snap.Installs {
		if err := put(i.ID, i.Version, i.Archive); err != nil {
			return err
		}
		i.Archive = nil
		st.Installs = append(st.Installs, i)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	for attempt := 0; attempt < 5; attempt++ {
		cur, err := c.Current(ctx, TeamStateFile)
		if err != nil {
			return err
		}
		var base int64
		if cur != nil {
			base = cur.Rev
			if !cur.Deleted {
				if cur.Sha == sha {
					return nil
				}
				there, err := readStoredTeam(ctx, c, cur.Sha)
				if err == nil && there.At > st.At {
					return nil
				}
			}
		}
		if err := c.PutBlob(ctx, sha, raw); err != nil {
			return err
		}
		out, err := c.Commit(ctx, filestore.CommitRequest{Path: TeamStateFile, Base: base, Sha: sha, Size: int64(len(raw)), Author: "apps-team"})
		if err != nil {
			return err
		}
		if !out.Conflict {
			return nil
		}
	}
	return errors.New("apps: the team in the store kept changing under the write")
}

// teamClockSkew is how far ahead of this machine's clock a team in the store may be.
const teamClockSkew = 5 * time.Minute

// maxStoredTeam bounds the team's file (the archives are not in it).
const maxStoredTeam = 4 << 20

func readStoredTeam(ctx context.Context, c TeamClient, sha string) (storedTeam, error) {
	var b bytes.Buffer
	var st storedTeam
	if err := c.GetBlob(ctx, sha, &b); err != nil {
		return st, err
	}
	if b.Len() > maxStoredTeam {
		return st, errors.New("apps: the team's file is too large")
	}
	return st, json.Unmarshal(b.Bytes(), &st)
}

// ApplyStoredTeam applies the team the store has (the revision e of TeamStateFile) as the platform's snapshot is
// applied, with this machine's own role and the archives it does not hold yet read from the store.
func (r *Registry) ApplyStoredTeam(ctx context.Context, c TeamClient, e filestore.Entry, host string, now time.Time) ([]string, error) {
	if e.Deleted {
		return nil, nil
	}
	st, err := readStoredTeam(ctx, c, e.Sha)
	if err != nil {
		return nil, err
	}
	mine, err := r.Team()
	if err != nil {
		return nil, err
	}
	// The store carries the team's apps, never the membership: a machine applies only the team of the space it was
	// told it is in (the platform, or `harnessly app team set`), with its own role, and no team from the future,
	// which would hold every later snapshot off as stale.
	switch {
	case mine.Space == "" || st.Space != mine.Space:
		return nil, fmt.Errorf("apps: the store's team is of space %q, this machine is in %q", st.Space, mine.Space)
	case st.At > now.Add(teamClockSkew).UnixMilli():
		return nil, fmt.Errorf("apps: the store's team is from the future (%d)", st.At)
	}
	snap := TeamSnapshot{Space: mine.Space, Role: mine.Role, At: st.At, Catalog: []SnapshotEntry{}, Installs: []SnapshotInstall{}}
	fetch := func(id, version string) ([]byte, error) {
		if version == "" || statErr(r.catalogArchive(id, version)) == nil {
			return nil, nil
		}
		ae, err := c.Current(ctx, teamArchivePath(id, version))
		if err != nil {
			return nil, err
		}
		if ae == nil || ae.Deleted {
			return nil, fmt.Errorf("%s %s: the store has no archive", id, version)
		}
		var b bytes.Buffer
		if err := c.GetBlob(ctx, ae.Sha, &b); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	var problems []string
	for _, ce := range st.Catalog {
		se := SnapshotEntry{CatalogEntry: ce}
		if se.Archive, err = fetch(ce.ID, ce.Version); err != nil {
			problems = append(problems, err.Error())
		}
		if se.PreviousArchive, err = fetch(ce.ID, ce.Previous); err != nil {
			problems = append(problems, err.Error())
		}
		snap.Catalog = append(snap.Catalog, se)
	}
	for _, si := range st.Installs {
		if si.Archive, err = fetch(si.ID, si.Version); err != nil {
			problems = append(problems, err.Error())
		}
		snap.Installs = append(snap.Installs, si)
	}
	more, err := r.ApplyTeam(snap, host, now)
	if errors.Is(err, ErrStaleTeam) {
		return problems, nil
	}
	return append(problems, more...), err
}

// TeamArchivePath is where the store keeps the package of an app's version.
func TeamArchivePath(id, version string) string { return teamArchivePath(id, version) }

// MaxTeamState bounds the team's file a reader takes.
const MaxTeamState = maxStoredTeam

// TeamApp is an app installed for a space, as the store's team has it: the pinned version and its archive's sum, and
// what the catalog says of it.
type TeamApp struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Sum     string   `json:"-"`
	Targets []string `json:"targets"`
}

// TeamInstalls is the apps installed for the space in the store's team (TeamStateFile's bytes), sorted by id.
func TeamInstalls(raw []byte) ([]TeamApp, error) {
	var st storedTeam
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	names := map[string]CatalogEntry{}
	for _, e := range st.Catalog {
		names[e.ID] = e
	}
	out := []TeamApp{}
	for _, i := range st.Installs {
		if !idRe.MatchString(i.ID) || !semverRe.MatchString(i.Version) || i.Sum == "" {
			continue
		}
		e := names[i.ID]
		name := e.Name
		if name == "" {
			name = i.ID
		}
		out = append(out, TeamApp{ID: i.ID, Name: name, Version: i.Version, Sum: i.Sum, Targets: e.Targets})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}
