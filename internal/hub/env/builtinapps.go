package env

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The platform's apps (docs/hub/environment.md, «Built-in apps»): «Доски» and «Документы» are in every organization
// with no setting up, as `app` items of the organization's layer, while the hub has the address of their service. They
// are on for everybody and not pinned: a person turns one off for themselves on their machine, the organization turns
// one off for everybody. Like «Рекомендованное», the items and the entries live in the code, not in the database.

// BuiltInApp is one app of the platform.
type BuiltInApp struct {
	// Key names the app in ids and in the organization's switch: "boards", "docs".
	Key string
	// AppID is the id in the package's manifest; Service names it for a person («Открыть доски»).
	AppID string
	Name  string
	Hint  string
	// InstallKey is the installation's value (InstallValues) whose presence puts the app in: the service's address.
	InstallKey string
	// RepoURL, Branch and Path are where the package is.
	RepoURL, Branch, Path string
}

// The default places of the packages: the folder harnessly-app of each service's public repository.
const (
	DefaultBoardsAppRepo = "https://github.com/ly-solutions/boards"
	DefaultDocsAppRepo   = "https://github.com/ly-solutions/artifacts"
	DefaultAppBranch     = "main"
	builtInAppPath       = "harnessly-app"
)

// BuiltInAppIDs are the manifest ids of the platform's apps.
const (
	BoardsAppID = "harnessly.boards"
	DocsAppID   = "harnessly.docs"
)

// BuiltInAppSource says where the hub takes an app's package from; empty fields are the defaults.
type BuiltInAppSource struct{ RepoURL, Branch string }

// BuiltInApps are the platform's apps with their packages at boards and docs.
func BuiltInApps(boards, docs BuiltInAppSource) []BuiltInApp {
	or := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	return []BuiltInApp{
		{Key: "boards", AppID: BoardsAppID, Name: "Доски", Hint: "доски и схемы: рисует человек и агент", InstallKey: "boardsUrl",
			RepoURL: or(boards.RepoURL, DefaultBoardsAppRepo), Branch: or(boards.Branch, DefaultAppBranch), Path: builtInAppPath},
		{Key: "docs", AppID: DocsAppID, Name: "Документы", Hint: "документы команды: пишет человек и агент", InstallKey: "artifactsUrl",
			RepoURL: or(docs.RepoURL, DefaultDocsAppRepo), Branch: or(docs.Branch, DefaultAppBranch), Path: builtInAppPath},
	}
}

// builtIns is what the platform itself puts into every organization.
type builtIns struct {
	// recommended: the group «Рекомендованное» (HUB_ENV_RECOMMENDED).
	recommended bool
	// apps are the platform's apps (nil: HUB_ENV_APPS=off); install are the installation's values (InstallValues).
	apps    []BuiltInApp
	install map[string]string
	// feat says which organizations have a person's own layer (FeatureOwn); nil: none.
	feat FeatureGate
}

const builtInAppPrefix = "app-"

// BuiltInAppItemID is the catalog id of a platform's app.
func BuiltInAppItemID(key string) string { return recommendedItemPrefix + builtInAppPrefix + key }

// builtInAppFlag is the organization's switch of a platform's app: a row of org_features that is not one of the hub's
// flags (features.Known), so the flags screen does not list it. No row, or enabled: the app is on; enabled false: the
// organization turned it off for everybody.
func builtInAppFlag(key string) string { return "env.app." + key }

// builtInApp finds a platform's app of this hub by its catalog id.
func (b builtIns) builtInApp(itemID string) (BuiltInApp, bool) {
	for _, a := range b.apps {
		if BuiltInAppItemID(a.Key) == itemID && b.install[a.InstallKey] != "" {
			return a, true
		}
	}
	return BuiltInApp{}, false
}

func builtInAppSource(a BuiltInApp, off bool) string {
	src := map[string]any{"from": "repo", "repoUrl": a.RepoURL, "branch": a.Branch, "path": a.Path}
	if off {
		src["off"] = true
	}
	raw, _ := json.Marshal(src)
	return string(raw)
}

// addBuiltInApps puts the platform's apps whose service has an address into an organization's world; off are the apps
// the organization turned off (by key).
func (w *world) addBuiltInApps(b builtIns, off map[string]bool) {
	for i, a := range b.apps {
		if b.install[a.InstallKey] == "" {
			continue
		}
		id := BuiltInAppItemID(a.Key)
		w.items[id] = store.CatalogItem{ID: id, Kind: store.CatalogApp, Name: a.Name, Hint: a.Hint, Source: builtInAppSource(a, off[a.Key])}
		w.entries = append(w.entries, store.EnvEntry{ID: recommendedEntryPrefix + builtInAppPrefix + a.Key, OrgID: w.org.ID, Layer: store.EnvLayerOrg,
			ScopeID: w.org.ID, ItemID: id, Enabled: true, Audience: AudienceJSON(Audience{Mode: AudienceTenant}),
			Position: 2*recommendedPosition + i})
	}
}

// hasApps: the hub has a platform's app to give, its service's address known.
func (b builtIns) hasApps() bool {
	for _, a := range b.apps {
		if b.install[a.InstallKey] != "" {
			return true
		}
	}
	return false
}

// bareWorld is an organization with nothing of its own in the environment: the platform's apps alone. It is what the
// machines of an organization without «Окружение» hold, so that the apps of the installation's services need no
// setting up at all.
func bareWorld(ctx context.Context, st store.Store, o store.Org, b builtIns) (*world, error) {
	w := &world{org: o, items: map[string]store.CatalogItem{}, install: b.install}
	off, err := builtInAppsOff(ctx, st, o.ID, b)
	if err != nil {
		return nil, err
	}
	w.addBuiltInApps(b, off)
	return w, nil
}

// builtInAppsOff reads which of the platform's apps the organization turned off.
func builtInAppsOff(ctx context.Context, st store.Store, orgID string, b builtIns) (map[string]bool, error) {
	if len(b.apps) == 0 {
		return nil, nil
	}
	rows, err := st.OrgFeatures(ctx, orgID)
	if err != nil {
		return nil, err
	}
	off := map[string]bool{}
	for _, a := range b.apps {
		for _, f := range rows {
			if f.Feature == builtInAppFlag(a.Key) && !f.Enabled {
				off[a.Key] = true
			}
		}
	}
	return off, nil
}

// appPlace names where an app item's package is, for telling two items of the same app apart from two apps: the
// repository's address without its case, a trailing slash or ".git", and the folder. "" when the item names none.
func (w *world) appPlace(src json.RawMessage) string {
	var s struct{ Repo, RepoURL, Path string }
	if json.Unmarshal(src, &s) != nil {
		return ""
	}
	u := s.RepoURL
	if u == "" && s.Repo != "" {
		var r struct{ URL string }
		if json.Unmarshal([]byte(w.items[s.Repo].Source), &r) == nil {
			u = r.URL
		}
	}
	u = strings.TrimSuffix(strings.TrimRight(strings.ToLower(strings.TrimSpace(u)), "/"), ".git")
	if u == "" || s.Path == "" {
		return ""
	}
	return u + "#" + path.Clean(s.Path)
}

// resolve is Resolve for a person of the organization, without the platform's apps the organization has an item of
// its own for (the same package: the same repository and folder): one app is one item, and the organization's wins,
// with its pin, its audience and its switch.
func (w *world) resolve(p Person) []Effective {
	eff := Resolve(w.resolverItems(), w.resolverEntries(), p)
	own := map[string]bool{}
	builtIn := false
	for _, e := range eff {
		if e.Kind != hubproto.EnvKindApp {
			continue
		}
		if hubproto.IsBuiltInEnvItem(e.ID) {
			builtIn = true
		} else if place := w.appPlace(e.Source); place != "" {
			own[place] = true
		}
	}
	if !builtIn || len(own) == 0 {
		return eff
	}
	out := eff[:0]
	for _, e := range eff {
		if e.Kind == hubproto.EnvKindApp && hubproto.IsBuiltInEnvItem(e.ID) && own[w.appPlace(e.Source)] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// patchBuiltInApp is PATCH …/catalog/{itemId} for a platform's app: the one thing an organization changes in it is
// `source.off`, «Выключено у всех». Anything else of the item is the platform's.
func (s *Service) patchBuiltInApp(w http.ResponseWriter, r *http.Request, p *auth.Principal, a BuiltInApp, in catalogIn) error {
	if in.Name != nil || in.Hint != nil || in.Archived != nil || in.Source == nil {
		return httpx.Conflict("built_in", "У встроенного приложения меняют только «Выключено у всех»")
	}
	var src struct {
		Off bool `json:"off"`
	}
	if err := json.Unmarshal(in.Source, &src); err != nil {
		return httpx.Invalid("source", "Нужен объект с полем off")
	}
	id := BuiltInAppItemID(a.Key)
	var q *req
	var out store.CatalogItem
	touched := false
	err := s.db.Tx(r.Context(), func(st store.Store) error {
		var err error
		if q, err = s.open(r, st, p, FeatureEnv); err != nil {
			return err
		}
		if err := q.requireOrgAdmin(); err != nil {
			return err
		}
		c, ok := q.w.items[id]
		if !ok {
			return httpx.ErrNotFound
		}
		was := appOff(c.Source)
		out = c
		if was == src.Off {
			return nil
		}
		touched = true
		if err := st.SetOrgFeature(r.Context(), q.w.org.ID, builtInAppFlag(a.Key), !src.Off, p.User.ID, s.now()); err != nil {
			return err
		}
		out.Source = builtInAppSource(a, src.Off)
		verb := "включено"
		if src.Off {
			verb = "выключено у всех"
		}
		return s.catalogChanged(q, st, p, out, "Каталог: приложение «"+a.Name+"» "+verb, "catalog.update",
			map[string]any{"off": was}, map[string]any{"off": src.Off})
	})
	if err != nil {
		return err
	}
	if touched {
		s.publish(q.w.org.ID, scopeOf(store.EnvLayerOrg, q.w.org.ID).Key(), "", 0)
		s.kick()
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"item": catalogView(out, true)})
	return nil
}

// appOff reads `off` of an app item's source.
func appOff(src string) bool {
	var s struct {
		Off bool `json:"off"`
	}
	return json.Unmarshal([]byte(src), &s) == nil && s.Off
}
