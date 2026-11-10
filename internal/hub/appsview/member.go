package appsview

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/appstore"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// the apps installed for a space, served to its members from the store with every machine off: the page and
// the view's files from the package the team keeps there (team/archives), the user and space areas of the storage in
// the store. The hub's API checks the session and the role before it hands a request over (ServeMember).

// ServeMember answers /apps/space/<space>/<app>/... for a person of the space, named as the store names them
// (storehub.PersonName).
func (v *View) ServeMember(w http.ResponseWriter, r *http.Request, spaceID, person string) {
	ms := &apps.Members{
		Lookup: func(ctx context.Context, spaceID, appID string) (apps.Installed, error) {
			return v.installed(ctx, spaceID, appID)
		},
		Storage: func(ctx context.Context, m apps.Member) (*apps.Storage, error) {
			room, err := v.o.Rooms.AppsRoom(ctx, m.Space)
			if err != nil {
				return nil, err
			}
			st := &apps.Storage{Root: filepath.Join(v.o.Dir, "storage", m.Space)}
			st.UseCenter(&apps.Center{Files: appstore.RoomFiles(room, "hub", "hub-member"), Person: m.Person})
			return st, nil
		},
	}
	ms.ServeMember(w, r, apps.Member{Space: spaceID, Person: person})
}

// SpaceApps is the apps installed for a space, as the store's team has them.
func (v *View) SpaceApps(ctx context.Context, spaceID string) ([]hubproto.SpaceApp, error) {
	list, err := v.team(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := make([]hubproto.SpaceApp, 0, len(list))
	for _, a := range list {
		sa := hubproto.SpaceApp{ID: a.ID, Name: a.Name, Version: a.Version, Targets: a.Targets}
		if sa.Targets == nil {
			sa.Targets = []string{}
		}
		if slices.Contains(a.Targets, apps.TargetApp) {
			sa.Path = apps.MemberPath(spaceID, a.ID)
		}
		out = append(out, sa)
	}
	return out, nil
}

// team is the store's team of a space; none when it has no team file.
func (v *View) team(ctx context.Context, spaceID string) ([]apps.TeamApp, error) {
	room, err := v.o.Rooms.AppsRoom(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	raw, err := readFile(ctx, room, apps.TeamStateFile, apps.MaxTeamState)
	if errors.Is(err, os.ErrNotExist) {
		return []apps.TeamApp{}, nil
	}
	if err != nil {
		return nil, err
	}
	return apps.TeamInstalls(raw)
}

// installed is an app installed for a space, its pinned package unpacked from the store (checked against its sum).
func (v *View) installed(ctx context.Context, spaceID, appID string) (apps.Installed, error) {
	list, err := v.team(ctx, spaceID)
	if err != nil {
		return apps.Installed{}, err
	}
	i := slices.IndexFunc(list, func(a apps.TeamApp) bool { return a.ID == appID })
	if i < 0 {
		return apps.Installed{}, apps.ErrNotInstalled
	}
	a := list[i]
	v.mu.Lock()
	app, ok := v.apps[a.Sum]
	v.mu.Unlock()
	if ok && app.ID == a.ID && app.Version == a.Version {
		return app, nil
	}
	room, err := v.o.Rooms.AppsRoom(ctx, spaceID)
	if err != nil {
		return apps.Installed{}, err
	}
	archive, err := readFile(ctx, room, apps.TeamArchivePath(a.ID, a.Version), apps.MaxArchiveBytes)
	if errors.Is(err, os.ErrNotExist) {
		return apps.Installed{}, apps.ErrNotInstalled
	}
	if err != nil {
		return apps.Installed{}, err
	}
	dir := filepath.Join(v.o.Dir, "bundles")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return apps.Installed{}, err
	}
	app, err = apps.UnpackBundle(archive, a.Sum, a.ID, a.Version, dir)
	if err != nil {
		v.o.Log.Warn("apps view: a team package refused", "app", a.ID, "error", err)
		return apps.Installed{}, apps.ErrNotInstalled
	}
	v.mu.Lock()
	v.apps[a.Sum] = app
	v.mu.Unlock()
	return app, nil
}
