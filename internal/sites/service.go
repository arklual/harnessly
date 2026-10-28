package sites

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Person is whoever a management request comes from.
type Person struct {
	ID   string
	Name string
}

// Authority is what the sites service asks of its host: who is calling, what a person may do in a space, and how a
// person who is not signed in at a site gets a ticket. The hub implements it with its sessions and spaces; a
// standalone server with an admin token and a trusted gateway.
type Authority interface {
	// Person is the signed-in person behind a management request; nil, nil when there is none.
	Person(r *http.Request) (*Person, error)
	// Role is the person's role in the space («owner», «member») and "" when they have none.
	Role(ctx context.Context, personID, space string) (string, error)
	// MachineOK says whether the machine is still one of the space's and, when the session names a person it publishes for,
	// whether that person is still a member. A removed machine ends its sessions through this.
	MachineOK(ctx context.Context, machineID, personID, space string) bool
	// CanView says whether the person may open the space's team-only sites.
	CanView(ctx context.Context, personID, space string) (bool, error)
	// ViewerEntry is where an anonymous visitor of a team-only site is sent to sign in; the entry sends them back to
	// returnURL with a ticket.
	ViewerEntry(host, returnURL string) string
	Tickets
	// IssueTicket makes a ticket for the person to open host.
	IssueTicket(personID, host string) string
}

// Config is how the service is set up.
type Config struct {
	// Domain is the wildcard domain sites live under (sites.example.com), with :port when it is not the default. Empty:
	// the path fallback only.
	Domain string
	// PublicURL is the address of the server itself (https://hub.example.com), without a trailing slash; the path
	// fallback and the sign-in entry hang off it.
	PublicURL string
	// Secure says that sites are served over https.
	Secure bool
	Limits Limits
	// AbuseURL is where the 404 page of the bare domain sends a complaint.
	AbuseURL string
	// StripHeaderPrefixes are the lower-case prefixes of request headers the edge drops: the ones a gateway in front uses
	// to carry identity must never reach a site, nor be forged by a visitor. Empty means the default, x-preview-.
	StripHeaderPrefixes []string
	// Key signs the sites' cookies; at least 32 bytes.
	Key []byte
	// SPAFallback is the default for a new site.
	SPAFallback bool
}

// domainHost is Domain without the port.
func (c Config) domainHost() string { return HostName(c.Domain) }

// Service is the sites module.
type Service struct {
	cfg      Config
	repo     repo
	blobs    Blobs
	auth     Authority
	sessions *Sessions
	log      *slog.Logger
	now      func() time.Time

	mu        sync.Mutex
	siteCache map[string]cachedSite
	manifests map[string]loaded
}

type cachedSite struct {
	site  Site
	until time.Time
}

// Deps are what the service is built from.
type Deps struct {
	DB     *store.DB
	Blobs  Blobs
	Auth   Authority
	Config Config
	Log    *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

const (
	pendingTTL    = 2 * time.Hour
	siteCacheTTL  = 2 * time.Second
	maxManifests  = 256
	blobGraceTime = time.Hour
)

// New makes the service.
func New(d Deps) (*Service, error) {
	if d.DB == nil || d.Blobs == nil || d.Auth == nil {
		return nil, errors.New("sites: database, blob store and authority are required")
	}
	if len(d.Config.Key) < 32 {
		return nil, errors.New("sites: the signing key must be at least 32 bytes")
	}
	if d.Config.Limits == (Limits{}) {
		d.Config.Limits = DefaultLimits
	}
	if d.Config.Domain != "" && !strings.Contains(d.Config.domainHost(), ".") && d.Config.domainHost() != "localhost" {
		return nil, fmt.Errorf("sites: the domain %q is not a domain name", d.Config.Domain)
	}
	d.Config.Domain = strings.ToLower(strings.Trim(d.Config.Domain, "."))
	if len(d.Config.StripHeaderPrefixes) == 0 {
		d.Config.StripHeaderPrefixes = []string{"x-preview-"}
	}
	d.Config.PublicURL = strings.TrimRight(d.Config.PublicURL, "/")
	s := &Service{cfg: d.Config, blobs: d.Blobs, auth: d.Auth, log: d.Log, now: d.Now,
		siteCache: map[string]cachedSite{}, manifests: map[string]loaded{}}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	s.repo = repo{db: d.DB, now: s.now}
	s.sessions = NewSessions(d.Config.Key, d.Config.Secure, s.now)
	return s, nil
}

// Config is the configuration the service runs with.
func (s *Service) Config() Config { return s.cfg }

// Subdomains says whether sites get addresses of their own under a wildcard domain; false is the path fallback.
func (s *Service) Subdomains() bool { return s.cfg.Domain != "" }

func (s *Service) scheme() string {
	if s.cfg.Secure {
		return "https"
	}
	return "http"
}

// URL is where the deployment of a site opens.
func (s *Service) URL(a Addr) string {
	if s.Subdomains() {
		return s.scheme() + "://" + a.Label() + "." + s.cfg.Domain + "/"
	}
	return s.cfg.PublicURL + "/sites/" + a.Label() + "/"
}

// SiteURL is the address of a site's current deployment.
func (s *Service) SiteURL(site Site) string {
	return s.URL(Addr{Site: site.Name, Space: site.Space, Kind: KindCurrent})
}

func (s *Service) invalidate(space, name string) {
	s.mu.Lock()
	delete(s.siteCache, space+"/"+name)
	s.mu.Unlock()
}

// ---- sites ----

// CreateSite makes an empty site. A taken name in the space is a conflict.
func (s *Service) CreateSite(ctx context.Context, space, name, access string, spa *bool, by, byID, origin string) (Site, error) {
	if err := ValidSpaceLabel(space); err != nil {
		return Site{}, httpx.Invalid("space", "Имя пространства не подходит для адреса сайта: "+err.Error()+". Переименуйте пространство")
	}
	if err := ValidSiteName(name); err != nil {
		return Site{}, httpx.Invalid("name", "Имя сайта не подходит: "+strings.TrimPrefix(err.Error(), "имя сайта: "))
	}
	if access == "" {
		access = AccessTeam
	}
	if access != AccessTeam && access != AccessLink {
		return Site{}, httpx.Invalid("access", "Доступ: team или link")
	}
	n, err := s.repo.countSites(ctx, space)
	if err != nil {
		return Site{}, err
	}
	if n >= s.cfg.Limits.MaxSitesPerSpace {
		return Site{}, httpx.Conflict("too_many_sites", fmt.Sprintf("В пространстве уже %d сайтов, это предел", s.cfg.Limits.MaxSitesPerSpace))
	}
	site := Site{Space: space, Name: name, Access: access, SPA: s.cfg.SPAFallback, CreatedBy: by, CreatedByID: byID, Origin: origin}
	if spa != nil {
		site.SPA = *spa
	}
	if err := s.repo.createSite(ctx, &site); err != nil {
		if store.IsUnique(err) {
			return Site{}, httpx.Conflict("name_taken", "Сайт с таким именем в пространстве уже есть")
		}
		return Site{}, err
	}
	return site, nil
}

// Site finds a site by its name in a space.
func (s *Service) Site(ctx context.Context, space, name string) (Site, error) {
	site, err := s.repo.site(ctx, space, name)
	if errors.Is(err, ErrNotFound) {
		return Site{}, httpx.ErrNotFound
	}
	return site, err
}

// Exists says whether the host is the bare sites domain or the address of a site that exists: what a certificate may be
// issued for on demand.
func (s *Service) Exists(ctx context.Context, host string) bool {
	if HostName(host) == s.cfg.domainHost() {
		return s.cfg.Domain != ""
	}
	a, ok := ParseHost(host, s.cfg.Domain)
	if !ok {
		return false
	}
	site, err := s.repo.site(ctx, a.Space, a.Site)
	if err != nil {
		return false
	}
	switch a.Kind {
	case KindVersion:
		// A name per version would let anyone ask for certificates without end: only versions that exist count.
		_, err := s.repo.deployment(ctx, site.ID, a.N)
		return err == nil
	case KindPreview:
		return false // previews are reserved: none exists yet
	}
	return true
}

// Sites are a space's sites, the latest changed first.
func (s *Service) Sites(ctx context.Context, space string) ([]Site, error) {
	return s.repo.listSites(ctx, space)
}

// Versions are a site's deployments, newest first.
func (s *Service) Versions(ctx context.Context, site Site) ([]Deployment, error) {
	return s.repo.listDeployments(ctx, site.ID)
}

// Update changes a site's access or single-page fallback.
func (s *Service) Update(ctx context.Context, site Site, access *string, spa *bool, by string) (Site, error) {
	if access != nil && *access != AccessTeam && *access != AccessLink {
		return Site{}, httpx.Invalid("access", "Доступ: team или link")
	}
	if err := s.repo.updateSite(ctx, site.ID, access, spa, by); err != nil {
		return Site{}, err
	}
	s.invalidate(site.Space, site.Name)
	return s.Site(ctx, site.Space, site.Name)
}

// Delete removes a site with all its versions and tokens; its blobs go with the next sweep.
func (s *Service) Delete(ctx context.Context, site Site) error {
	if err := s.repo.deleteSite(ctx, site.ID); err != nil {
		return err
	}
	s.invalidate(site.Space, site.Name)
	return nil
}

// SetCurrent makes a version the one the site serves: the rollback, and «Сделать текущей».
func (s *Service) SetCurrent(ctx context.Context, site Site, version int, by string) (Site, error) {
	if err := s.repo.setCurrent(ctx, site.ID, version, by); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Site{}, httpx.ErrNotFound
		}
		return Site{}, err
	}
	s.invalidate(site.Space, site.Name)
	return s.Site(ctx, site.Space, site.Name)
}

// ---- tokens ----

const tokenPrefix = "hsite_"

// NewToken makes a deploy token for the site. The secret is returned once; only its hash is kept.
func (s *Service) NewToken(ctx context.Context, site Site, name, by string) (Token, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "CI"
	}
	if len([]rune(name)) > 60 {
		return Token{}, "", httpx.Invalid("name", "Название токена длиннее 60 символов")
	}
	secret := tokenPrefix + store.RandomBase32(20)
	t := Token{SiteID: site.ID, Name: name, CreatedBy: by}
	if err := s.repo.addToken(ctx, &t, store.HashToken(secret)); err != nil {
		return Token{}, "", err
	}
	return t, secret, nil
}

// Tokens are the site's deploy tokens.
func (s *Service) Tokens(ctx context.Context, site Site) ([]Token, error) {
	return s.repo.listTokens(ctx, site.ID)
}

// RevokeToken removes a deploy token.
func (s *Service) RevokeToken(ctx context.Context, site Site, id string) error {
	if err := s.repo.deleteToken(ctx, site.ID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return httpx.ErrNotFound
		}
		return err
	}
	return nil
}

// TokenSite is the site a deploy token belongs to; ErrNotFound for an unknown token.
func (s *Service) TokenSite(ctx context.Context, secret string) (Site, Token, error) {
	if !strings.HasPrefix(secret, tokenPrefix) {
		return Site{}, Token{}, ErrNotFound
	}
	t, err := s.repo.tokenSite(ctx, store.HashToken(secret))
	if err != nil {
		return Site{}, Token{}, err
	}
	site, err := s.repo.siteByID(ctx, t.SiteID)
	if err != nil {
		return Site{}, Token{}, err
	}
	if s.now().Sub(t.LastUsedAt) > time.Minute {
		s.repo.touchToken(ctx, t.ID)
	}
	return site, t, nil
}

// ---- deploying ----

// secretsHTTPError is a 422 that lists the files, never the secrets.
func secretsHTTPError(f []Finding) *httpx.Error {
	return httpx.New(http.StatusUnprocessableEntity, "secrets_found",
		"В сайте есть файлы, похожие на секреты. Уберите их или загрузите с allowSecrets, если это не секреты").With("findings", f)
}

// Begin announces a deployment: the manifest is checked (paths, hashes, sizes, limits, secret-looking names), and the
// answer lists the hashes the store does not have yet.
func (s *Service) Begin(ctx context.Context, site Site, m Manifest, source, by string, allowSecrets bool) (Pending, []string, error) {
	if err := m.Validate(s.cfg.Limits); err != nil {
		return Pending{}, nil, httpx.Invalid("manifest", "Сайт не подходит: "+err.Error())
	}
	if !allowSecrets {
		if f := ScanPaths(m); len(f) > 0 {
			return Pending{}, nil, secretsHTTPError(f)
		}
	}
	now := s.now().UTC()
	p := Pending{SiteID: site.ID, Manifest: m, Source: source, CreatedBy: by, CreatedAt: now, ExpiresAt: now.Add(pendingTTL), AllowSecrets: allowSecrets}
	if err := s.repo.addPending(ctx, &p); err != nil {
		return Pending{}, nil, err
	}
	var missing []string
	for _, h := range m.Hashes() {
		// Only what this space has itself uploaded counts: a hash known from another space is not a proof of holding the bytes.
		_, known, err := s.repo.spaceBlob(ctx, site.Space, h)
		if err != nil {
			return Pending{}, nil, err
		}
		if known {
			// Recorded is not the same as held: a store emptied by hand leaves the record behind.
			if ok, err := s.blobs.Has(ctx, h); err != nil {
				return Pending{}, nil, err
			} else if ok {
				continue
			}
		}
		missing = append(missing, h)
	}
	return p, missing, nil
}

func (s *Service) pendingOf(ctx context.Context, site Site, id string) (Pending, error) {
	p, err := s.repo.pending(ctx, site.ID, id)
	if errors.Is(err, ErrNotFound) {
		return Pending{}, httpx.ErrNotFound
	}
	if err != nil {
		return Pending{}, err
	}
	if !s.now().Before(p.ExpiresAt) {
		return Pending{}, httpx.ErrExpired
	}
	return p, nil
}

// spool reads r (at most limit bytes) into a temporary file while hashing it, and scans its start for secrets. The
// caller removes the file.
func spool(r io.Reader, limit int64) (f *os.File, hash string, size int64, finding string, err error) {
	f, err = os.CreateTemp("", "site-blob-*")
	if err != nil {
		return nil, "", 0, "", err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
			f = nil
		}
	}()
	hr := NewHashReader(io.LimitReader(r, limit+1))
	if _, err = io.Copy(f, hr); err != nil {
		return
	}
	if hr.N > limit {
		err = httpx.New(http.StatusRequestEntityTooLarge, "file_too_large", fmt.Sprintf("Файл больше %d МБ", limit>>20))
		return
	}
	head := make([]byte, min(hr.N, MaxScanBytes))
	n, _ := f.ReadAt(head, 0)
	finding = ScanContent(head[:n])
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return
	}
	return f, hr.Sum(), hr.N, finding, nil
}

// store puts a spooled file into the blob store and records it.
func (s *Service) storeBlob(ctx context.Context, space string, f *os.File, hash string, size int64) error {
	if err := s.blobs.Put(ctx, hash, f, size); err != nil {
		return err
	}
	if err := s.repo.addBlob(ctx, hash, size); err != nil {
		return err
	}
	return s.repo.linkBlob(ctx, space, hash)
}

// PutBlob takes the content of one file of a pending deployment. The content must hash to the announced sha256 and
// size; a file that looks like a secret is refused unless the uploader allowed it.
func (s *Service) PutBlob(ctx context.Context, site Site, pendingID, hash string, body io.Reader) error {
	p, err := s.pendingOf(ctx, site, pendingID)
	if err != nil {
		return err
	}
	paths := p.Manifest.PathsOf(hash)
	if !ValidHash(hash) || len(paths) == 0 {
		return httpx.Invalid("hash", "Такого файла нет в манифесте")
	}
	want := p.Manifest[paths[0]].Size
	f, got, size, finding, err := spool(body, want)
	if err != nil {
		return err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	if got != hash || size != want {
		return httpx.Invalid("hash", "Содержимое не совпадает с хэшем или размером из манифеста")
	}
	if finding != "" && !p.AllowSecrets {
		fs := make([]Finding, 0, len(paths))
		for _, pth := range paths {
			fs = append(fs, Finding{Path: pth, Rule: finding})
		}
		return secretsHTTPError(fs)
	}
	return s.storeBlob(ctx, site.Space, f, hash, size)
}

// Commit finishes a deployment: every blob must be in the store, then the manifest becomes the site's next version and,
// unless asked not to, the current one, in one step. Old versions beyond the keep limit go.
func (s *Service) Commit(ctx context.Context, site Site, pendingID, by string, makeCurrent bool) (Deployment, error) {
	p, err := s.pendingOf(ctx, site, pendingID)
	if err != nil {
		return Deployment{}, err
	}
	hashes := p.Manifest.Hashes()
	var missing []string
	// Every file must have come from this space, with the size the manifest said.
	for pth, f := range p.Manifest {
		size, ok, err := s.repo.spaceBlob(ctx, site.Space, f.Hash)
		if err != nil {
			return Deployment{}, err
		}
		if !ok {
			missing = append(missing, f.Hash)
		} else if size != f.Size {
			return Deployment{}, httpx.Invalid("manifest", "Размер файла "+pth+" в манифесте не совпадает с загруженным")
		}
	}
	if len(missing) > 0 {
		return Deployment{}, httpx.Conflict("blobs_missing", fmt.Sprintf("Не все файлы загружены (%d)", len(missing))).With("missing", missing)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	var firstErr error
	for _, h := range hashes {
		wg.Add(1)
		sem <- struct{}{}
		go func(h string) {
			defer func() { <-sem; wg.Done() }()
			ok, err := s.blobs.Has(ctx, h)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			} else if err == nil && !ok {
				missing = append(missing, h)
			}
		}(h)
	}
	wg.Wait()
	if firstErr != nil {
		return Deployment{}, firstErr
	}
	if len(missing) > 0 {
		return Deployment{}, httpx.Conflict("blobs_missing", fmt.Sprintf("Не все файлы загружены (%d)", len(missing))).With("missing", missing)
	}
	d, err := s.repo.commit(ctx, p, makeCurrent, by)
	if err != nil {
		return Deployment{}, err
	}
	s.invalidate(site.Space, site.Name)
	cur := d.Version
	if !makeCurrent {
		if fresh, err := s.repo.siteByID(ctx, site.ID); err == nil {
			cur = fresh.CurrentVersion
		}
	}
	if _, err := s.repo.dropOld(ctx, site.ID, cur, s.cfg.Limits.KeepVersions); err != nil {
		s.log.Warn("old site versions not dropped", "site", site.Name, "error", err)
	}
	return d, nil
}

// DeployZip makes a deployment out of a zip archive in one request: the archive is unpacked here, safely.
func (s *Service) DeployZip(ctx context.Context, site Site, body io.Reader, by string, allowSecrets bool) (Deployment, error) {
	lim := s.cfg.Limits
	tmp, err := os.CreateTemp("", "site-zip-*")
	if err != nil {
		return Deployment{}, err
	}
	defer func() { tmp.Close(); os.Remove(tmp.Name()) }()
	n, err := io.Copy(tmp, io.LimitReader(body, lim.MaxDeployBytes+1))
	if err != nil {
		return Deployment{}, err
	}
	if n > lim.MaxDeployBytes {
		return Deployment{}, httpx.New(http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("Архив больше %d МБ", lim.MaxDeployBytes>>20))
	}
	zr, err := zip.NewReader(tmp, n)
	if err != nil {
		return Deployment{}, httpx.Invalid("zip", "Это не zip-архив")
	}

	type entry struct {
		name string
		f    *zip.File
	}
	var entries []entry
	for _, zf := range zr.File {
		name := strings.ReplaceAll(zf.Name, "\\", "/")
		if zf.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			continue
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			return Deployment{}, httpx.Invalid("zip", "В архиве есть символическая ссылка: "+zf.Name)
		}
		base := path.Base(name)
		if strings.HasPrefix(name, "__MACOSX/") || base == ".DS_Store" || base == "Thumbs.db" {
			continue
		}
		entries = append(entries, entry{name: name, f: zf})
	}
	if len(entries) > lim.MaxFiles {
		return Deployment{}, httpx.Invalid("zip", fmt.Sprintf("Файлов больше %d", lim.MaxFiles))
	}
	// A folder zipped from the outside has one top-level folder around everything: the site is what is inside it.
	if root := commonRoot(entries, func(e entry) string { return e.name }); root != "" {
		for i := range entries {
			entries[i].name = strings.TrimPrefix(entries[i].name, root)
		}
	}

	m := Manifest{}
	spooled := map[string]*os.File{}
	defer func() {
		for _, f := range spooled {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	var total int64
	var findings []Finding
	for _, e := range entries {
		clean, err := CleanPath(e.name)
		if err != nil {
			return Deployment{}, httpx.Invalid("zip", err.Error())
		}
		if e.f.UncompressedSize64 > uint64(lim.MaxFileBytes) {
			return Deployment{}, httpx.Invalid("zip", fmt.Sprintf("%s: файл больше %d МБ", clean, lim.MaxFileBytes>>20))
		}
		rc, err := e.f.Open()
		if err != nil {
			return Deployment{}, httpx.Invalid("zip", "Не удалось прочитать "+clean)
		}
		// The header's size is a claim: what is read decides, and a bomb stops at the limit.
		f, hash, size, finding, err := spool(rc, lim.MaxFileBytes)
		rc.Close()
		if err != nil {
			return Deployment{}, err
		}
		total += size
		if total > lim.MaxDeployBytes {
			f.Close()
			os.Remove(f.Name())
			return Deployment{}, httpx.Invalid("zip", fmt.Sprintf("Сайт больше %d МБ после распаковки", lim.MaxDeployBytes>>20))
		}
		if finding != "" {
			findings = append(findings, Finding{Path: clean, Rule: finding})
		}
		m[clean] = File{Hash: hash, Size: size}
		if old, ok := spooled[hash]; ok {
			_ = old // same content twice: keep one
			f.Close()
			os.Remove(f.Name())
		} else {
			spooled[hash] = f
		}
	}
	if err := m.Validate(lim); err != nil {
		return Deployment{}, httpx.Invalid("zip", "Сайт не подходит: "+err.Error())
	}
	if !allowSecrets {
		findings = append(findings, ScanPaths(m)...)
		if len(findings) > 0 {
			return Deployment{}, secretsHTTPError(findings)
		}
	}
	for hash, f := range spooled {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return Deployment{}, err
		}
		st, err := f.Stat()
		if err != nil {
			return Deployment{}, err
		}
		if err := s.storeBlob(ctx, site.Space, f, hash, st.Size()); err != nil {
			return Deployment{}, err
		}
	}
	now := s.now().UTC()
	p := Pending{SiteID: site.ID, Manifest: m, Source: "zip", CreatedBy: by, CreatedAt: now, ExpiresAt: now.Add(pendingTTL), AllowSecrets: allowSecrets}
	if err := s.repo.addPending(ctx, &p); err != nil {
		return Deployment{}, err
	}
	return s.Commit(ctx, site, p.ID, by, true)
}

// commonRoot is "dir/" when every entry lives under the same single top-level folder, else "".
func commonRoot[T any](items []T, name func(T) string) string {
	root := ""
	for _, it := range items {
		first, rest, ok := strings.Cut(name(it), "/")
		if !ok || rest == "" {
			return ""
		}
		if root == "" {
			root = first
		} else if root != first {
			return ""
		}
	}
	if root == "" {
		return ""
	}
	return root + "/"
}

// ---- sweep ----

// Sweep forgets the uploads that expired and removes the blobs nothing refers to (and that are old enough not to be
// somebody's upload in flight). It returns how many blobs went.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	now := s.now()
	if err := s.repo.dropExpiredPending(ctx, now); err != nil {
		return 0, err
	}
	candidates, err := s.repo.blobsBefore(ctx, now.Add(-blobGraceTime))
	if err != nil || len(candidates) == 0 {
		return 0, err
	}
	refs, err := s.repo.referencedHashes(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, h := range candidates {
		if refs[h] {
			continue
		}
		if err := s.blobs.Delete(ctx, h); err != nil {
			s.log.Warn("site blob not removed", "error", err)
			continue
		}
		if err := s.repo.forgetBlob(ctx, h); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// Run sweeps now and then until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if n, err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("sites sweep failed", "error", err)
		} else if n > 0 {
			s.log.Info("sites sweep", "blobsRemoved", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
