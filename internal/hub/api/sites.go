package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/sshinstall"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/sites"
)

// siteAuthority backs the sites module with the hub's sessions and spaces: a space's slug is the space part of a site's
// address, its members are the people a team-only site opens for, and the visitor's ticket is the hub's own ticket store.
type siteAuthority struct {
	h *Hub
}

func (a siteAuthority) Person(r *http.Request) (*sites.Person, error) {
	p, err := a.h.Auth.Authenticate(r)
	if err != nil || p == nil {
		return nil, err
	}
	name := p.User.Name
	if name == "" {
		name = p.User.Email
	}
	return &sites.Person{ID: p.User.ID, Name: name}, nil
}

func (a siteAuthority) Role(ctx context.Context, personID, space string) (string, error) {
	st := a.h.db.S()
	sp, err := st.SpaceBySlug(ctx, space)
	if err != nil {
		if err == store.ErrNotFound {
			return "", nil
		}
		return "", err
	}
	return st.Role(ctx, sp.ID, personID)
}

// MachineOK: the machine is still the space's and not revoked, and the person it publishes for, if the session names
// one, is still a member.
func (a siteAuthority) MachineOK(ctx context.Context, machineID, personID, space string) bool {
	if strings.HasPrefix(machineID, "fleet:") {
		return personID == "" && space == a.h.cfg.FleetSitesSpace
	}
	st := a.h.db.S()
	m, err := st.MachineByID(ctx, machineID)
	if err != nil || m.RevokedAt != nil {
		return false
	}
	sp, err := st.SpaceBySlug(ctx, space)
	if err != nil || sp.ID != m.SpaceID {
		return false
	}
	if personID == "" {
		return true
	}
	role, err := st.Role(ctx, sp.ID, personID)
	return err == nil && role != ""
}

func (a siteAuthority) CanView(ctx context.Context, personID, space string) (bool, error) {
	role, err := a.Role(ctx, personID, space)
	return role != "", err
}

// ViewerEntry: a visitor of a team-only site goes through the hub, which knows who they are. On the hub's own origin
// (the path fallback, host "") the hub's session is the credential and the sign-in page is all that is needed.
func (a siteAuthority) ViewerEntry(host, returnURL string) string {
	if host == "" {
		return "/signin?next=" + url.QueryEscape(returnURL)
	}
	return a.h.cfg.PublicURL + "/api/site-entry?host=" + url.QueryEscape(host) + "&return=" + url.QueryEscape(returnURL)
}

func siteScope(host string) string { return "site:" + sites.HostName(host) }

func (a siteAuthority) Redeem(tk, host string) (string, bool) {
	return a.h.Tickets.Redeem(tk, siteScope(host))
}

func (a siteAuthority) IssueTicket(personID, host string) string {
	return a.h.Tickets.Issue(personID, siteScope(host))
}

var _ sites.Authority = siteAuthority{}

// newSites builds the sites module when the configuration switches it on. Blobs, when set, replace the configured store
// (tests).
func (h *Hub) newSites(o Options) error {
	cfg := h.cfg
	if cfg.SitesMode() == "off" {
		return nil
	}
	blobs := o.SiteBlobs
	if blobs == nil {
		var err error
		if cfg.SitesS3.Endpoint != "" {
			s3, e := sites.NewS3Blobs(sites.S3Config{Endpoint: cfg.SitesS3.Endpoint, Region: cfg.SitesS3.Region, Bucket: cfg.SitesS3.Bucket,
				Prefix: cfg.SitesS3.Prefix, AccessKey: cfg.SitesS3.AccessKey, SecretKey: cfg.SitesS3.SecretKey})
			blobs, err = s3, e
		} else {
			if err = os.MkdirAll(cfg.SitesDir, 0o700); err == nil {
				blobs, err = sites.NewFolderBlobs(cfg.SitesDir)
			}
		}
		if err != nil {
			return fmt.Errorf("sites: %w", err)
		}
	}
	master, err := sshinstall.MasterKey(cfg.DataDir, cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("sites: %w", err)
	}
	key := sha256.Sum256(append([]byte("harnessly-hub/sites-cookie/v1\x00"), master...))
	svc, err := sites.New(sites.Deps{
		DB: h.db, Blobs: blobs, Auth: siteAuthority{h: h}, Log: h.log, Now: o.Now,
		Config: sites.Config{Domain: cfg.SitesHost(), PublicURL: cfg.PublicURL, Secure: cfg.Secure, Key: key[:], AbuseURL: cfg.SitesAbuseURL},
	})
	if err != nil {
		return err
	}
	h.Sites = svc
	return nil
}

// siteEntry is where a visitor of a team-only site is sent to prove who they are: it checks that the person is signed
// in to the hub and a member of the site's space, and sends them back to the site with a one-time ticket.
func (h *Hub) siteEntry(w http.ResponseWriter, r *http.Request) error {
	p, err := h.Auth.Authenticate(r)
	if err != nil {
		return err
	}
	if p == nil {
		http.Redirect(w, r, "/signin?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return nil
	}
	host := sites.HostName(r.URL.Query().Get("host"))
	// Only the path and query of the return address are taken from the request: the scheme, host and port are the hub's
	// own (https when it is secure, the port of the sites domain), so the ticket can go nowhere else, and never over http
	// where the hub is on https.
	ret, err := url.Parse(r.URL.Query().Get("return"))
	if err != nil || ret.User != nil || (ret.Host != "" && sites.HostName(ret.Host) != host) ||
		!strings.HasPrefix(ret.Path, "/") || strings.HasPrefix(ret.Path, "//") || strings.Contains(ret.Path, "\\") {
		return httpx.Invalid("return", "Адрес возврата не подходит")
	}
	addr, ok := sites.ParseHost(host, h.cfg.SitesDomain)
	if !ok {
		return httpx.Invalid("host", "Это не адрес сайта")
	}
	can, err := siteAuthority{h: h}.CanView(r.Context(), p.User.ID, addr.Space)
	if err != nil {
		return err
	}
	if !can {
		return httpx.New(http.StatusForbidden, "forbidden", "Этот сайт открыт только участникам пространства")
	}
	q := ret.Query()
	q.Set(sites.TicketParam, h.Tickets.Issue(p.User.ID, siteScope(host)))
	target := url.URL{Scheme: "http", Host: host, Path: ret.Path, RawQuery: q.Encode()}
	if h.cfg.Secure {
		target.Scheme = "https"
	}
	if _, port, err := net.SplitHostPort(h.cfg.SitesHost()); err == nil {
		target.Host = net.JoinHostPort(host, port)
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target.String(), http.StatusFound)
	return nil
}

// siteCheck answers a reverse proxy that issues certificates on demand (Caddy's on_demand_tls ask): 200 when the host is
// the sites domain or a site that exists, 404 otherwise, so that nobody can make the proxy order certificates for
// names of their choosing. Only the hub's own host and trusted proxies may ask.
func (h *Hub) siteCheck(w http.ResponseWriter, r *http.Request) error {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	addr, err := netip.ParseAddr(ip)
	if err != nil || !(addr.IsLoopback() || h.trusted(addr)) {
		return httpx.ErrForbidden
	}
	if h.Sites.Exists(r.Context(), r.URL.Query().Get("domain")) {
		w.WriteHeader(http.StatusOK)
		return nil
	}
	return httpx.ErrNotFound
}

func (h *Hub) trusted(a netip.Addr) bool {
	for _, p := range h.cfg.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// siteSession gives a machine, which proves itself with the signature of its own key, a publishing session of its space:
// the token its runtime publishes sites with (a Sinopia project, a folder of the machine).
func (h *Hub) siteSession(w http.ResponseWriter, r *http.Request) error {
	m, err := h.Dialer.AdmitSites(r)
	if err != nil {
		return err
	}
	sp, err := h.db.S().SpaceByID(r.Context(), m.SpaceID)
	if err != nil {
		return err
	}
	ms := sites.MachineSession{Space: sp.Slug, MachineID: m.ID, Name: m.Name}
	// The machine may say which person it publishes for (the runtime knows its owner's address). It is believed only
	// when that person registered the machine, or the hub itself gave them a ticket for it lately (they did sign in to
	// its runtime through the hub), and is still a member of the space. In every other case, an unknown address, a
	// non-member, a member who never opened this machine, the session is the machine's own without a person, and the
	// answer is the same, so it does not tell which addresses exist.
	var body struct {
		Person string `json:"person"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	if email := strings.ToLower(strings.TrimSpace(body.Person)); email != "" {
		if u, err := h.db.S().UserByEmail(r.Context(), email); err == nil && (u.ID == m.CreatedBy || h.Tickets.OpenedRecently(u.ID, m.ID)) {
			if role, err := h.db.S().Role(r.Context(), sp.ID, u.ID); err == nil && role != "" {
				ms.PersonID = u.ID
				if u.Name != "" {
					ms.Name = u.Name
				} else {
					ms.Name = u.Email
				}
			}
		}
	}
	token, exp := h.Sites.NewMachineSession(ms)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"space": sp.Slug, "token": token, "expiresAt": httpx.Time(exp), "url": h.cfg.PublicURL})
	return nil
}

// fleetSiteSession lets a fleet gateway publish for its authenticated runtimes without making them hub machines.
func (h *Hub) fleetSiteSession(w http.ResponseWriter, r *http.Request) error {
	secret, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(secret), []byte(h.cfg.FleetSitesSecret)) != 1 {
		return httpx.New(http.StatusUnauthorized, "unauthorized", "Нет доступа")
	}
	var body struct {
		MachineID string `json:"machineId"`
		Name      string `json:"name"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	body.MachineID = strings.TrimSpace(body.MachineID)
	body.Name = strings.TrimSpace(body.Name)
	if body.MachineID == "" || len(body.MachineID) > 200 || len(body.Name) > 200 {
		return httpx.Invalid("machineId", "Некорректная машина")
	}
	sp, err := h.db.S().SpaceBySlug(r.Context(), h.cfg.FleetSitesSpace)
	if err != nil {
		return err
	}
	if body.Name == "" {
		body.Name = body.MachineID
	}
	token, exp := h.Sites.NewMachineSession(sites.MachineSession{Space: sp.Slug, MachineID: "fleet:" + body.MachineID, Name: body.Name})
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"space": sp.Slug, "token": token, "expiresAt": httpx.Time(exp), "url": h.cfg.PublicURL})
	return nil
}
