package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// The WebDAV part of CalDAV the connector needs (RFC 4918, 4791, 6578, 6764): find the principal and the calendar
// home, list the calendars with their ctag and sync token, ask what changed since a token, fetch objects by href or by
// a time range, write and delete one with If-Match. Written here, small, rather than taken from a library: the
// redirects, the hosts a password may go to and the error words are ours.

const (
	maxBody      = 8 << 20
	maxRedirects = 5
	userAgent    = "harnessly-calendar/1 (+https://github.com/ly-solutions/harnessly)"
)

// dav is one account's client: the base it was pointed at, its credentials, the runtime's HTTP client.
type dav struct {
	base     *url.URL
	user     string
	password string
	http     *http.Client
}

func newDAV(raw, user, password string, client *http.Client) (*dav, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, connectors.Sayf("The server address is not valid")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	// Redirects are followed here, not by the client: Go turns a redirected PROPFIND into a GET.
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &dav{base: u, user: user, password: password, http: &c}, nil
}

// site is the last two labels of a host: a password goes only to the hosts of the site the person named (iCloud
// answers the calendars of caldav.icloud.com from p42-caldav.icloud.com).
func site(host string) string {
	host = strings.ToLower(strings.TrimSuffix(strings.Split(host, ":")[0], "."))
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func (d *dav) resolve(href string) (*url.URL, error) {
	ref, err := url.Parse(href)
	if err != nil {
		return nil, err
	}
	u := d.base.ResolveReference(ref)
	if site(u.Host) != site(d.base.Host) || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("the server sent us to another site (%s)", u.Host)
	}
	if d.base.Scheme == "https" && u.Scheme != "https" {
		return nil, errors.New("the server sent us from https to http")
	}
	return u, nil
}

type davError struct {
	status int
	method string
}

func (e *davError) Error() string { return fmt.Sprintf("%s answered %d", e.method, e.status) }

func statusOf(err error) int {
	var de *davError
	if errors.As(err, &de) {
		return de.status
	}
	return 0
}

// do sends a request and follows redirects on the same site, keeping the method and the body.
func (d *dav) do(ctx context.Context, method, href string, header http.Header, body []byte) (*http.Response, *url.URL, error) {
	u, err := d.resolve(href)
	if err != nil {
		return nil, nil, err
	}
	for range maxRedirects + 1 {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		for k, v := range header {
			req.Header[k] = v
		}
		req.Header.Set("User-Agent", userAgent)
		if d.user != "" || d.password != "" {
			req.SetBasicAuth(d.user, d.password)
		}
		res, err := d.http.Do(req)
		if err != nil {
			return nil, nil, err
		}
		switch res.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			loc := res.Header.Get("Location")
			_ = res.Body.Close()
			next, err := url.Parse(loc)
			if err != nil || loc == "" {
				return nil, nil, &davError{status: res.StatusCode, method: method}
			}
			nu := u.ResolveReference(next)
			if site(nu.Host) != site(d.base.Host) {
				return nil, nil, fmt.Errorf("the server sent us to another site (%s)", nu.Host)
			}
			u = nu
			continue
		}
		return res, u, nil
	}
	return nil, nil, errors.New("too many redirects")
}

// call is do with the answer read: a status outside 2xx is a davError.
func (d *dav) call(ctx context.Context, method, href string, header http.Header, body []byte) ([]byte, http.Header, *url.URL, error) {
	res, u, err := d.do(ctx, method, href, header, body)
	if err != nil {
		return nil, nil, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, nil, nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return raw, res.Header, u, &davError{status: res.StatusCode, method: method}
	}
	return raw, res.Header, u, nil
}

// The multistatus answer, with the properties the connector asks for.
type multistatus struct {
	Responses []response `xml:"DAV: response"`
	SyncToken string     `xml:"DAV: sync-token"`
}

type response struct {
	Href     string     `xml:"DAV: href"`
	Status   string     `xml:"DAV: status"`
	Propstat []propstat `xml:"DAV: propstat"`
}

type propstat struct {
	Status string `xml:"DAV: status"`
	Prop   prop   `xml:"DAV: prop"`
}

type hrefs struct {
	Href []string `xml:"DAV: href"`
}

type prop struct {
	CurrentUserPrincipal hrefs  `xml:"DAV: current-user-principal"`
	CalendarHomeSet      hrefs  `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
	ScheduleOutbox       hrefs  `xml:"urn:ietf:params:xml:ns:caldav schedule-outbox-URL"`
	AddressSet           hrefs  `xml:"urn:ietf:params:xml:ns:caldav calendar-user-address-set"`
	DisplayName          string `xml:"DAV: displayname"`
	ResourceType         struct {
		Calendar *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
	} `xml:"DAV: resourcetype"`
	Components struct {
		Comp []struct {
			Name string `xml:"name,attr"`
		} `xml:"urn:ietf:params:xml:ns:caldav comp"`
	} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
	CalendarZone string `xml:"urn:ietf:params:xml:ns:caldav calendar-timezone"`
	CTag         string `xml:"http://calendarserver.org/ns/ getctag"`
	SyncToken    string `xml:"DAV: sync-token"`
	ETag         string `xml:"DAV: getetag"`
	CalendarData string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
	Privileges   struct {
		Privilege []struct {
			Write    *struct{} `xml:"DAV: write"`
			WriteAll *struct{} `xml:"DAV: all"`
			WriteCt  *struct{} `xml:"DAV: write-content"`
		} `xml:"DAV: privilege"`
	} `xml:"DAV: current-user-privilege-set"`
}

func (r response) ok() (prop, bool) {
	for _, ps := range r.Propstat {
		if strings.Contains(ps.Status, " 200") {
			return ps.Prop, true
		}
	}
	return prop{}, false
}

func (r response) gone() bool { return strings.Contains(r.Status, " 404") }

func (d *dav) multi(ctx context.Context, method, href, depth, body string) (multistatus, *url.URL, error) {
	h := http.Header{"Content-Type": {"application/xml; charset=utf-8"}}
	if depth != "" {
		h.Set("Depth", depth)
	}
	raw, _, u, err := d.call(ctx, method, href, h, []byte(body))
	if err != nil {
		return multistatus{}, nil, err
	}
	var ms multistatus
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return multistatus{}, nil, fmt.Errorf("the server's answer is not WebDAV: %w", err)
	}
	return ms, u, nil
}

const xmlHead = `<?xml version="1.0" encoding="utf-8"?>`

// principal finds the account's principal: at the address the person gave, else at /.well-known/caldav of its host.
func (d *dav) principal(ctx context.Context) (string, error) {
	body := xmlHead + `<d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>`
	var firstErr error
	for _, href := range []string{d.base.String(), "/.well-known/caldav"} {
		ms, u, err := d.multi(ctx, "PROPFIND", href, "0", body)
		if statusOf(err) == http.StatusUnauthorized {
			return "", err
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, r := range ms.Responses {
			if p, ok := r.ok(); ok && len(p.CurrentUserPrincipal.Href) > 0 {
				ref, _ := url.Parse(p.CurrentUserPrincipal.Href[0])
				return u.ResolveReference(ref).String(), nil
			}
		}
	}
	if firstErr == nil {
		firstErr = connectors.Sayf("This address has no CalDAV calendars")
	}
	return "", firstErr
}

// home is the principal's calendar home and the addresses the account is known by (mailto:me@example.com).
func (d *dav) home(ctx context.Context, principal string) (string, []string, error) {
	body := xmlHead + `<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-home-set/><c:calendar-user-address-set/></d:prop></d:propfind>`
	ms, u, err := d.multi(ctx, "PROPFIND", principal, "0", body)
	if err != nil {
		return "", nil, err
	}
	for _, r := range ms.Responses {
		if p, ok := r.ok(); ok && len(p.CalendarHomeSet.Href) > 0 {
			ref, _ := url.Parse(p.CalendarHomeSet.Href[0])
			return u.ResolveReference(ref).String(), p.AddressSet.Href, nil
		}
	}
	return "", nil, connectors.Sayf("The account has no calendar home")
}

// calendar is one calendar collection of the home.
type calendar struct {
	Href      string
	Name      string
	CTag      string
	SyncToken string
	ReadOnly  bool
	// Zone is the TZID of the calendar's own time zone, if it says one.
	Zone string
}

// tzidOf reads the TZID of a VTIMEZONE text.
func tzidOf(vtz string) string {
	for _, line := range strings.Split(vtz, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "TZID:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// calendars lists the home's collections that hold events, with their ctag and sync token.
func (d *dav) calendars(ctx context.Context, home string) ([]calendar, error) {
	body := xmlHead + `<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:cs="http://calendarserver.org/ns/"><d:prop>` +
		`<d:resourcetype/><d:displayname/><c:supported-calendar-component-set/><cs:getctag/><d:sync-token/><d:current-user-privilege-set/><c:calendar-timezone/>` +
		`</d:prop></d:propfind>`
	ms, u, err := d.multi(ctx, "PROPFIND", home, "1", body)
	if err != nil {
		return nil, err
	}
	var out []calendar
	for _, r := range ms.Responses {
		p, ok := r.ok()
		if !ok || p.ResourceType.Calendar == nil {
			continue
		}
		events := len(p.Components.Comp) == 0
		for _, c := range p.Components.Comp {
			events = events || strings.EqualFold(c.Name, "VEVENT")
		}
		if !events {
			continue
		}
		ref, _ := url.Parse(r.Href)
		href := u.ResolveReference(ref).Path
		name := strings.TrimSpace(p.DisplayName)
		if name == "" {
			name = strings.Trim(href[strings.LastIndex(strings.TrimSuffix(href, "/"), "/")+1:], "/")
		}
		ro := len(p.Privileges.Privilege) > 0
		for _, pr := range p.Privileges.Privilege {
			if pr.Write != nil || pr.WriteAll != nil || pr.WriteCt != nil {
				ro = false
			}
		}
		out = append(out, calendar{Href: href, Name: name, CTag: p.CTag, SyncToken: p.SyncToken, ReadOnly: ro, Zone: tzidOf(p.CalendarZone)})
	}
	return out, nil
}

// object is one calendar object: its href, ETag and iCalendar text.
type object struct {
	Href string
	ETag string
	Data string
}

func collect(ms multistatus, u *url.URL) (objs []object, gone []string) {
	for _, r := range ms.Responses {
		ref, _ := url.Parse(r.Href)
		href := u.ResolveReference(ref).Path
		if r.gone() {
			gone = append(gone, href)
			continue
		}
		p, ok := r.ok()
		if !ok {
			if len(r.Propstat) > 0 && strings.Contains(r.Propstat[0].Status, " 404") {
				gone = append(gone, href)
			}
			continue
		}
		objs = append(objs, object{Href: href, ETag: p.ETag, Data: p.CalendarData})
	}
	return objs, gone
}

// changes asks what changed in a calendar since a sync token (RFC 6578): the changed hrefs with their ETags, the
// removed ones, and the new token. An expired token is a davError of 403 or 409.
func (d *dav) changes(ctx context.Context, href, token string) (changed []object, gone []string, next string, err error) {
	body := xmlHead + `<d:sync-collection xmlns:d="DAV:"><d:sync-token>` + xmlEscape(token) + `</d:sync-token><d:sync-level>1</d:sync-level>` +
		`<d:prop><d:getetag/></d:prop></d:sync-collection>`
	ms, u, err := d.multi(ctx, "REPORT", href, "", body)
	if err != nil {
		return nil, nil, "", err
	}
	changed, gone = collect(ms, u)
	return changed, gone, ms.SyncToken, nil
}

// fetch takes objects by href (calendar-multiget).
func (d *dav) fetch(ctx context.Context, cal string, list []string) ([]object, error) {
	if len(list) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString(xmlHead + `<c:calendar-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/><c:calendar-data/></d:prop>`)
	for _, h := range list {
		b.WriteString("<d:href>" + xmlEscape(h) + "</d:href>")
	}
	b.WriteString(`</c:calendar-multiget>`)
	ms, u, err := d.multi(ctx, "REPORT", cal, "1", b.String())
	if err != nil {
		return nil, err
	}
	objs, _ := collect(ms, u)
	return objs, nil
}

// query takes the events of a calendar that touch a time range (calendar-query), whole: recurring ones with their
// rule, expanded on the machine.
func (d *dav) query(ctx context.Context, cal, start, end string) ([]object, error) {
	body := xmlHead + `<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/><c:calendar-data/></d:prop>` +
		`<c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="` + start + `" end="` + end + `"/>` +
		`</c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	ms, u, err := d.multi(ctx, "REPORT", cal, "1", body)
	if err != nil {
		return nil, err
	}
	objs, _ := collect(ms, u)
	return objs, nil
}

// get takes one object with its ETag.
func (d *dav) get(ctx context.Context, href string) (object, error) {
	raw, h, _, err := d.call(ctx, http.MethodGet, href, http.Header{"Accept": {"text/calendar"}}, nil)
	if err != nil {
		return object{}, err
	}
	return object{Href: href, ETag: h.Get("ETag"), Data: string(raw)}, nil
}

// put writes an object: a new one only if none is there, a changed one only if nobody changed it since etag.
func (d *dav) put(ctx context.Context, href, etag string, data []byte) error {
	h := http.Header{"Content-Type": {"text/calendar; charset=utf-8"}}
	if etag == "" {
		h.Set("If-None-Match", "*")
	} else {
		h.Set("If-Match", etag)
	}
	_, _, _, err := d.call(ctx, http.MethodPut, href, h, data)
	return err
}

func (d *dav) remove(ctx context.Context, href, etag string) error {
	h := http.Header{}
	if etag != "" {
		h.Set("If-Match", etag)
	}
	_, _, _, err := d.call(ctx, http.MethodDelete, href, h, nil)
	return err
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
