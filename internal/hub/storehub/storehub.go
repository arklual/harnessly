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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
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

	mu       sync.Mutex
	machines map[string]cachedMachine
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
	s := &Store{key: o.Key, db: o.DB, admit: o.Admit, log: o.Log, now: o.Now, machines: map[string]cachedMachine{}}
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
		Path: hubproto.StoreAPIPath, Collections: filestore.Kinds})
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
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return filestore.Access{}, filestore.ErrUnauthorized
	}
	machineID, spaceID, ok := s.verify(tok)
	if !ok {
		return filestore.Access{}, filestore.ErrUnauthorized
	}
	m, err := s.machine(r.Context(), machineID)
	if errors.Is(err, store.ErrNotFound) {
		return filestore.Access{}, filestore.ErrUnauthorized
	}
	if err != nil {
		return filestore.Access{}, err
	}
	if m.RevokedAt != nil || m.DisabledAt != nil || m.SpaceID != spaceID {
		return filestore.Access{}, filestore.ErrUnauthorized
	}
	return filestore.Access{Member: memberName(m), Room: s.Room(spaceID, coll)}, nil
}

// memberName is how a machine is written into the revisions: its name, else its id.
func memberName(m store.Machine) string {
	if n := strings.TrimSpace(m.Name); n != "" && len(n) <= 80 {
		return n
	}
	return m.ID
}
