// Package storehub mounts the store (internal/filestore) in the hub: the revisions and contents of every space's
// collections, a Sinopia library first (docs/hub/store.md). A machine of a space proves itself with its own key
// (POST /agent/store), gets a short session, and speaks the store's API under /store with it. A collection of a
// space is a room of its own, `<kind>-<space id>`, so a machine never reads another space's files.
package storehub

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/appstore"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/migration"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// SessionTTL is how long a store session works; the machine asks for a new one before, or on a 401.
const SessionTTL = time.Hour

// machineCache is how long the store believes a machine is still in its space without asking the database.
const machineCache = 30 * time.Second

// Machines is what the store needs of the hub's database: the machine a session names, as it is now.
type Machines interface {
	MachineByID(ctx context.Context, id string) (store.Machine, error)
	SpaceBySlug(ctx context.Context, slug string) (store.Space, error)
}

// Admit checks a machine's signed request for a session (link.Dialer.AdmitStore).
type Admit func(r *http.Request) (store.Machine, error)

// Grant is what a machine may do with one recorded design project: see it (its files come to the machine), edit it.
type Grant struct{ See, Edit, Own bool }

// Design is the hub's design access as the store needs it: a machine's copy of its space's library holds only
// the projects its person has a role in, and takes writes only to those they may edit; a project's owner file reaching
// the store records the project if the hub has no row for it yet.
type Design interface {
	StoreView(ctx context.Context, m store.Machine) (map[string]Grant, error)
	RecordFromStore(ctx context.Context, m store.Machine, project string, ownerFile []byte) (bool, error)
}

// viewCache is how long a machine's view of the library is kept before it is read again.
const viewCache = 15 * time.Second

// Store is the hub's store.
type Store struct {
	srv    *filestore.Server
	meta   filestore.MetaStore
	blobs  filestore.BlobStore
	key    []byte
	db     Machines
	admit  Admit
	log    *slog.Logger
	now    func() time.Time
	legacy string // the space id whose rooms are bare kinds; "" for none

	design Design

	mu       sync.Mutex
	machines map[string]cachedMachine
	views    map[string]cachedView
}

type cachedView struct {
	v     map[string]Grant
	scope string
	at    time.Time
}

type cachedMachine struct {
	m  store.Machine
	at time.Time
}

// Options builds a Store.
type Options struct {
	Config *config.Config
	DB     Machines
	Admit  Admit
	// Key signs the sessions; every replica must have the same.
	Key []byte
	Log *slog.Logger
	Now func() time.Time
	// Meta and Blobs replace the configured drivers (tests); the Store does not close them.
	Meta  filestore.MetaStore
	Blobs filestore.BlobStore
	// Design filters the design collection by the projects' access; nil: every machine of a space sees all of it.
	Design Design
}

// Open opens the configured drivers. Nil, nil when the store is off.
func Open(ctx context.Context, o Options) (*Store, error) {
	cfg := o.Config
	if cfg.Store.Off {
		return nil, nil
	}
	if len(o.Key) < 16 {
		return nil, errors.New("store: no session key")
	}
	s := &Store{key: o.Key, db: o.DB, admit: o.Admit, log: o.Log, now: o.Now, machines: map[string]cachedMachine{},
		design: o.Design, views: map[string]cachedView{}}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	owned := o.Meta == nil
	s.meta, s.blobs = o.Meta, o.Blobs
	if s.meta == nil {
		var err error
		if s.meta, s.blobs, err = OpenDrivers(ctx, cfg); err != nil {
			return nil, err
		}
	}
	if cfg.Store.LegacySpace != "" {
		sp, err := o.DB.SpaceBySlug(ctx, cfg.Store.LegacySpace)
		if err != nil {
			if owned {
				s.meta.Close()
				s.blobs.Close()
			}
			return nil, fmt.Errorf("store: HUB_STORE_LEGACY_SPACE %q: %w", cfg.Store.LegacySpace, err)
		}
		s.legacy = sp.ID
	}
	s.srv = filestore.NewServer(s.meta, s.blobs, s.authorize)
	if !owned {
		s.meta, s.blobs = nil, nil
	}
	return s, nil
}

// OpenDrivers opens the store's metadata and contents as the configuration names them.
func OpenDrivers(ctx context.Context, cfg *config.Config) (filestore.MetaStore, filestore.BlobStore, error) {
	meta, err := filestore.OpenMetaStoreIn(ctx, cfg.Store.DatabaseURL, cfg.Store.PGPrefix)
	if err != nil {
		return nil, nil, fmt.Errorf("store database: %w", err)
	}
	var blobs filestore.BlobStore
	if cfg.Store.S3.Endpoint != "" {
		s3 := cfg.Store.S3
		blobs, err = filestore.NewS3BlobsWith(filestore.S3Config{Endpoint: s3.Endpoint, Region: s3.Region, Bucket: s3.Bucket,
			Prefix: s3.Prefix, AccessKey: s3.AccessKey, SecretKey: s3.SecretKey})
	} else {
		blobs, err = filestore.NewFileBlobs(cfg.Store.Dir)
	}
	if err != nil {
		meta.Close()
		return nil, nil, fmt.Errorf("store files: %w", err)
	}
	return meta, blobs, nil
}

// Close closes the drivers the Store opened.
func (s *Store) Close() {
	if s == nil || s.meta == nil {
		return
	}
	s.meta.Close()
	s.blobs.Close()
}

// Server is the store's API (the drivers are reached through it).
func (s *Store) Server() *filestore.Server { return s.srv }

// Mount adds the store's routes: the session a machine asks for and the API under /store.
func (s *Store) Mount(mux *http.ServeMux) {
	mux.Handle("POST "+hubproto.StorePath, httpx.Handler(s.session))
	mux.Handle(hubproto.StoreAPIPath+"/v1/", http.StripPrefix(hubproto.StoreAPIPath, s.srv.Handler()))
}

// HasBlob, OpenBlob and PutBlob reach a collection's contents from inside the hub (the workspace's uploads,
// HRN-879): a blob is the same bytes under its sha256 whoever put it, and PutBlob refuses bytes that hash otherwise.
func (s *Store) HasBlob(ctx context.Context, spaceID, kind, sha string) (bool, error) {
	if !filestore.ValidSha(sha) {
		return false, nil
	}
	return s.blobs.Has(ctx, s.Room(spaceID, kind), sha)
}

// OpenBlob opens a blob of a space's collection (os.ErrNotExist when it is not there).
func (s *Store) OpenBlob(ctx context.Context, spaceID, kind, sha string) (io.ReadCloser, error) {
	if !filestore.ValidSha(sha) {
		return nil, os.ErrNotExist
	}
	return s.blobs.Open(ctx, s.Room(spaceID, kind), sha)
}

// PutBlob stores bytes in a space's collection under their sha256.
func (s *Store) PutBlob(ctx context.Context, spaceID, kind, sha string, r io.Reader) error {
	return s.blobs.Put(ctx, s.Room(spaceID, kind), sha, r)
}

// DesignRoom is a space's design collection as the hub reads it itself: the views of the projects that no
// machine serves.
func (s *Store) DesignRoom(ctx context.Context, spaceID string) (*filestore.Store, error) {
	return s.srv.Room(ctx, s.Room(spaceID, filestore.KindDesign))
}

// AppsRoom is a space's apps collection as the hub reads it itself: the guest links it serves.
func (s *Store) AppsRoom(ctx context.Context, spaceID string) (*filestore.Store, error) {
	return s.srv.Room(ctx, s.Room(spaceID, filestore.KindApps))
}

// AppsSpaces are the spaces whose apps collection has a room.
func (s *Store) AppsSpaces(ctx context.Context) ([]string, error) {
	rooms, err := s.srv.Meta().Rooms(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, room := range rooms {
		switch {
		case room == filestore.KindApps && s.legacy != "":
			out = append(out, s.legacy)
		case strings.HasPrefix(room, filestore.KindApps+"-"):
			out = append(out, strings.TrimPrefix(room, filestore.KindApps+"-"))
		}
	}
	return out, nil
}

// Room is the room of a space's collection.
func (s *Store) Room(spaceID, kind string) string {
	if spaceID != "" && spaceID == s.legacy {
		return kind
	}
	return kind + "-" + spaceID
}

func (s *Store) session(w http.ResponseWriter, r *http.Request) error {
	m, err := s.admit(r)
	if err != nil {
		return err
	}
	if m.SpaceID == "" || !filestore.ValidRoom(s.Room(m.SpaceID, filestore.KindDesign)) {
		return httpx.New(http.StatusForbidden, "forbidden", "Машина не в пространстве")
	}
	exp := s.now().Add(SessionTTL)
	s.remember(m)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, hubproto.StoreSession{Token: s.sign(m.ID, m.SpaceID, exp), ExpiresAt: httpx.Time(exp),
		Path: hubproto.StoreAPIPath, Collections: filestore.Kinds, Person: PersonName(m.CreatedBy)})
	return nil
}

const tokenPrefix = "hls1."

var enc = base64.RawURLEncoding

func (s *Store) mac(payload string) []byte {
	h := hmac.New(sha256.New, s.key)
	_, _ = h.Write([]byte("harnessly-store-session/v1\x00"))
	_, _ = h.Write([]byte(payload))
	return h.Sum(nil)
}

func (s *Store) sign(machineID, spaceID string, exp time.Time) string {
	payload := enc.EncodeToString([]byte(machineID)) + "." + enc.EncodeToString([]byte(spaceID)) + "." + strconv.FormatInt(exp.Unix(), 10)
	return tokenPrefix + payload + "." + enc.EncodeToString(s.mac(payload))
}

// verify reads a session; it says nothing of whether the machine is still in the space.
func (s *Store) verify(token string) (machineID, spaceID string, ok bool) {
	rest, found := strings.CutPrefix(token, tokenPrefix)
	parts := strings.Split(rest, ".")
	if !found || len(parts) != 4 {
		return "", "", false
	}
	payload := strings.Join(parts[:3], ".")
	mac, err := enc.DecodeString(parts[3])
	if err != nil || !hmac.Equal(mac, s.mac(payload)) {
		return "", "", false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || s.now().Unix() >= exp {
		return "", "", false
	}
	m, err1 := enc.DecodeString(parts[0])
	sp, err2 := enc.DecodeString(parts[1])
	if err1 != nil || err2 != nil {
		return "", "", false
	}
	return string(m), string(sp), true
}

func (s *Store) remember(m store.Machine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.machines[m.ID] = cachedMachine{m: m, at: s.now()}
}

// machine is the machine as it is now (cached for a little while): a revoked, switched off or moved machine loses
// the store with its session still in hand.
func (s *Store) machine(ctx context.Context, id string) (store.Machine, error) {
	s.mu.Lock()
	c, ok := s.machines[id]
	s.mu.Unlock()
	if ok && s.now().Sub(c.at) < machineCache {
		return c.m, nil
	}
	m, err := s.db.MachineByID(ctx, id)
	if err != nil {
		return store.Machine{}, err
	}
	s.remember(m)
	return m, nil
}

// authorize is the store's Authorizer: a live session of a machine that is still in the space it names.
func (s *Store) authorize(r *http.Request, coll string) (filestore.Access, error) {
	m, err := s.sessionMachine(r)
	if err != nil {
		return filestore.Access{}, err
	}
	acc := filestore.Access{Member: memberName(m), Room: s.Room(m.SpaceID, coll)}
	if coll == filestore.KindDesign && s.design != nil {
		if err := s.filter(r.Context(), m, &acc); err != nil {
			return filestore.Access{}, err
		}
	}
	if coll == filestore.KindApps {
		if err := s.appsFilter(r.Context(), m, &acc); err != nil {
			return filestore.Access{}, err
		}
	}
	return acc, nil
}

// sessionMachine is the machine of a live store session that is still in the space the session names.
func (s *Store) sessionMachine(r *http.Request) (store.Machine, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return store.Machine{}, filestore.ErrUnauthorized
	}
	machineID, spaceID, ok := s.verify(tok)
	if !ok {
		return store.Machine{}, filestore.ErrUnauthorized
	}
	m, err := s.machine(r.Context(), machineID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Machine{}, filestore.ErrUnauthorized
	}
	if err != nil {
		return store.Machine{}, err
	}
	if m.RevokedAt != nil || m.DisabledAt != nil || m.SpaceID != spaceID {
		return store.Machine{}, filestore.ErrUnauthorized
	}
	return m, nil
}

// SessionMachine is the machine whose live store session the request carries, its space and the person who made it
// (the journal of the move of its data, internal/hub/migration): 401 without one.
func (s *Store) SessionMachine(r *http.Request) (migration.Session, error) {
	m, err := s.sessionMachine(r)
	if errors.Is(err, filestore.ErrUnauthorized) {
		return migration.Session{}, httpx.New(http.StatusUnauthorized, "unauthorized", "Нет сессии store")
	}
	if err != nil {
		return migration.Session{}, err
	}
	return migration.Session{MachineID: m.ID, SpaceID: m.SpaceID, OwnerID: m.CreatedBy}, nil
}

// PersonName is the name of a person's user areas in the apps collection: not their id, which a file name would show
// every machine of the space, and the same on every hub of theirs. "" for nobody.
func PersonName(userID string) string {
	if userID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("harnessly-apps-person/v1\x00" + userID))
	return "p-" + hex.EncodeToString(sum[:16])
}

// appsRules are what a machine may read and write of its space's apps collection:
//
//   - storage/<app>/user/<person>.json, a person's user area: only their machines;
//   - storage/<app>/chat/<person>.<chat>.json, a chat's area: only the machines of the person whose chat it is;
//   - storage/<app>/doc/<project>.json, a document's area: as the design project (seen by who may see it, written by
//     who may edit it); any other name (a project the hub has no row for, a guest link's area) is the space's;
//   - storage/<app>/space/area.json: the space's;
//   - team/state.json and team/archives/<app>/<version>.tgz, the team's apps: read by the space, written only by the
//     machines of the space's owners;
//   - guests/<person>/<hash>.json, a person's guest links: only their machines; bundles/<app>/<sum>.tgz, the packages
//     the links show: the space's;
//
// and nothing else is written there. An area is no larger than its scope's limit.
type appsRules struct {
	person string
	view   map[string]Grant
	owner  bool
}

func (a appsRules) sees(p string) bool { return a.may(p, false) }

func (a appsRules) writes(p string) bool { return a.may(p, true) }

func (a appsRules) may(p string, write bool) bool {
	parts := strings.Split(p, "/")
	switch {
	case parts[0] == "team":
		ok := p == "team/state.json" || len(parts) == 4 && parts[1] == "archives" && appstore.ValidApp(parts[2]) && strings.HasSuffix(parts[3], ".tgz")
		return ok && (!write || a.owner)
	case parts[0] == "guests":
		// A person's guest links: only their machines put them there; the hub reads them itself.
		return len(parts) == 3 && a.person != "" && parts[1] == a.person && hex64(strings.TrimSuffix(parts[2], ".json")) && strings.HasSuffix(parts[2], ".json")
	case parts[0] == "bundles":
		// An app's package by the sum of its archive, which the hub checks before it serves a byte of it.
		return len(parts) == 3 && appstore.ValidApp(parts[1]) && hex64(strings.TrimSuffix(parts[2], ".tgz")) && strings.HasSuffix(parts[2], ".tgz")
	case parts[0] != "storage":
		return !write
	case len(parts) != 4 || !appstore.ValidApp(parts[1]) || !strings.HasSuffix(parts[3], ".json"):
		return false
	}
	name := strings.TrimSuffix(parts[3], ".json")
	switch appstore.Scope(parts[2]) {
	case appstore.ScopeUser:
		return a.person != "" && name == a.person
	case appstore.ScopeChat:
		return a.person != "" && strings.HasPrefix(name, a.person+".")
	case appstore.ScopeSpace:
		return name == "area"
	case appstore.ScopeDoc:
		// A guest link's area (guest-<id>) is no project the hub has a row for, so it is the space's; a name that
		// is a recorded project's goes as the project whatever it looks like.
		g, known := a.view[name]
		if !known {
			return true
		}
		if write {
			return g.Edit
		}
		return g.See
	}
	return false
}

func hex64(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// admits refuses an area larger than its scope allows (the runtime refuses it first; this is the hub's own word). The
// size is the one the commit names: a blob larger than it says hashes to its sha only whole, and every reader of an
// area reads at most appstore.MaxAreaBytes of it, so such an area is refused when read, not served.
func (a appsRules) admits(req filestore.CommitRequest) error {
	parts := strings.Split(req.Path, "/")
	if req.Deleted || parts[0] != "storage" || len(parts) != 4 {
		return nil
	}
	if limit := appstore.Limit[appstore.Scope(parts[2])]; req.Size > int64(limit) {
		return fmt.Errorf("%s is over its scope's limit of %d bytes", req.Path, limit)
	}
	return nil
}

// roles is the part of the hub's tables that says a person's role in a space.
type roles interface {
	Role(ctx context.Context, spaceID, userID string) (string, error)
}

func (s *Store) appsFilter(ctx context.Context, m store.Machine, acc *filestore.Access) error {
	rules := appsRules{person: PersonName(m.CreatedBy)}
	if s.design != nil {
		view, _, err := s.view(ctx, m)
		if err != nil {
			return err
		}
		rules.view = view
	}
	if rr, ok := s.db.(roles); ok && m.CreatedBy != "" {
		role, err := rr.Role(ctx, m.SpaceID, m.CreatedBy)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		rules.owner = role == store.RoleOwner
	}
	acc.Sees, acc.Writes, acc.Admits = rules.sees, rules.writes, rules.admits
	return nil
}

// filter limits a machine to the design projects its person has a role in: the files of another person's
// project never reach a machine that has nothing to do with it, and a write to a project the person may not edit is
// refused. Paths outside a project folder (the shelves under .folders) and projects the hub has no row for are open.
func (s *Store) filter(ctx context.Context, m store.Machine, acc *filestore.Access) error {
	view, scope, err := s.view(ctx, m)
	if err != nil {
		return err
	}
	acc.Scope = scope
	acc.Sees = func(p string) bool {
		id, ok := projectOf(p)
		if !ok {
			return true
		}
		g, known := view[id]
		return !known || g.See
	}
	acc.Writes = func(p string) bool {
		if foreignFolder(p) {
			return false
		}
		id, ok := projectOf(p)
		if !ok {
			return true
		}
		g, known := view[id]
		if !known {
			return true
		}
		// Who owns a recorded project is the hub's word; its owner file changes only with the owner.
		if strings.EqualFold(p, id+"/"+filestore.OwnerFile) {
			return g.Own
		}
		return g.Edit
	}
	room := acc.Room
	acc.Committed = func(ctx context.Context, e filestore.Entry) {
		id, ok := projectOf(e.Path)
		// Only a real project id records a project (lower case, as the hub keeps it).
		if !ok || e.Deleted || e.Path != id+"/"+filestore.OwnerFile {
			return
		}
		if _, known := view[id]; known {
			return
		}
		s.record(ctx, m, room, id, e.Sha)
	}
	return nil
}

// record makes the hub's row for a project whose owner file just reached the store, and forgets the space's views so
// the other machines stop seeing a personal project at once.
func (s *Store) record(ctx context.Context, m store.Machine, room, project, sha string) {
	rs, err := s.srv.Room(ctx, room)
	if err != nil {
		return
	}
	// Only a new project is recorded from its owner file: one whose folder, in any letter case, holds nothing but this
	// owner file. An owner file written into a folder already filled (a kit, a project made before access, someone
	// else's) takes nothing over; who wrote the files does not enter into it, so a machine's name proves nothing.
	if !projectIDRe.MatchString(project) {
		return
	}
	n, err := rs.CountUnder(ctx, project+"/", 2)
	if err != nil || n != 1 {
		if n > 1 {
			s.log.Warn("store: an owner file over a filled folder does not record the project", "project", project, "machine", m.ID)
		}
		return
	}
	f, err := rs.OpenBlob(ctx, sha)
	if err != nil {
		s.log.Warn("store: an owner file not read", "project", project, "err", err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, 8<<10))
	f.Close()
	if err != nil {
		return
	}
	done, err := s.design.RecordFromStore(ctx, m, project, data)
	if err != nil {
		s.log.Warn("store: a project not recorded", "project", project, "machine", m.ID, "err", err)
		return
	}
	if done {
		s.log.Info("store: project recorded from its owner file", "project", project, "machine", m.ID)
		s.forgetViews(m.SpaceID)
	}
}

// view is the machine's design view (cached for a little while) and its scope: a name of what it hides and what it
// may not edit, which changes whenever that does.
func (s *Store) view(ctx context.Context, m store.Machine) (map[string]Grant, string, error) {
	s.mu.Lock()
	c, ok := s.views[m.ID]
	s.mu.Unlock()
	if ok && s.now().Sub(c.at) < viewCache {
		return c.v, c.scope, nil
	}
	v, err := s.design.StoreView(ctx, m)
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(v))
	for id, g := range v {
		if !g.See || !g.Edit {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	h := sha256.New()
	_, _ = h.Write([]byte("design-view/v1\x00"))
	for _, id := range ids {
		g := v[id]
		fmt.Fprintf(h, "%s:%t:%t\n", id, g.See, g.Edit)
	}
	scope := "v1." + hex.EncodeToString(h.Sum(nil)[:12])
	s.mu.Lock()
	s.views[m.ID] = cachedView{v: v, scope: scope, at: s.now()}
	s.mu.Unlock()
	return v, scope, nil
}

// forgetViews drops the cached views of a space's machines (all of them: the cache is small).
func (s *Store) forgetViews(string) {
	s.mu.Lock()
	s.views = map[string]cachedView{}
	s.mu.Unlock()
}

// projectOf is the project folder a library path is in; false for a path at the top (the shelves).
// The id is lower case, as the hub keeps it: on a filesystem that ignores case, `Secret/` is the folder `secret/`.
func projectOf(p string) (string, bool) {
	id, rest, ok := strings.Cut(p, "/")
	if !ok || rest == "" || strings.HasPrefix(id, ".") {
		return "", false
	}
	return strings.ToLower(id), true
}

// projectIDRe is a Sinopia project id as the runtime makes it (design.IsLibraryID).
var projectIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// foreignFolder: the path's first folder is not plain ASCII. Project ids are ASCII (projectIDRe), and a folder with
// another letter could pass for a project's where case is folded or code points are ignored (the Kelvin sign is a «k»,
// a joiner disappears), so the store takes no write into one: it fails closed rather than guessing look-alikes.
func foreignFolder(p string) bool {
	id, _, _ := strings.Cut(p, "/")
	return !isASCII(id)
}

func isASCII(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] >= 0x80 {
			return false
		}
	}
	return true
}

// memberName is how a machine is written into the revisions: its name, else its id.
func memberName(m store.Machine) string {
	if n := strings.TrimSpace(m.Name); n != "" && len(n) <= 80 {
		return n
	}
	return m.ID
}
