package ext

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// The UI an adapter brings to the hub's own web (the tail of HRN-753): a page of the hub's settings and the
// hub's branding. It is the hub-settings and branding slots of docs/apps.md, served from the extensions file, which only
// the hub's operator writes. A view runs in a sandboxed frame without the hub's origin; the hub's page and the view talk
// by postMessage and the view gets the context the page gives it and nothing else. Off unless HARNESSLY_UI_SLOTS=on.

// SlotHubSettings is the only slot the hub draws views for.
const SlotHubSettings = "hub-settings"

// UIView is one page of the hub's settings an adapter brings.
type UIView struct {
	// ID is the view's name, unique in the adapter.
	ID string `json:"id"`
	// Slot is where it shows: hub-settings.
	Slot string `json:"slot"`
	// Label is the menu item's text; Icon its glyph from the hub's set.
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	// URL is the https address the frame loads. Its origin is added to the page's frame-src; nothing else is.
	URL string `json:"url"`
}

// Branding is what an adapter sets for the hub's look: the product name and the accent.
type Branding struct {
	Name       string `json:"name"`
	Accent     string `json:"accent,omitempty"`
	SupportURL string `json:"supportUrl,omitempty"`
}

var (
	viewIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	accentRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

func (s Spec) validateUI() error {
	seen := map[string]bool{}
	for _, v := range s.UI {
		switch {
		case !viewIDRE.MatchString(v.ID):
			return fmt.Errorf("extension %q: ui view id %q is lowercase letters, digits, dots and dashes", s.Name, v.ID)
		case seen[v.ID]:
			return fmt.Errorf("extension %q: ui view %q is listed twice", s.Name, v.ID)
		case v.Slot != SlotHubSettings:
			return fmt.Errorf("extension %q: ui view %q: the hub draws %s only", s.Name, v.ID, SlotHubSettings)
		case strings.TrimSpace(v.Label) == "" || len(v.Label) > 60:
			return fmt.Errorf("extension %q: ui view %q: a label of 1 to 60 characters", s.Name, v.ID)
		}
		if _, err := ViewOrigin(v.URL); err != nil {
			return fmt.Errorf("extension %q: ui view %q: %w", s.Name, v.ID, err)
		}
		seen[v.ID] = true
	}
	if b := s.Branding; b != nil {
		switch {
		case strings.TrimSpace(b.Name) == "" || len(b.Name) > 60:
			return fmt.Errorf("extension %q: branding.name is 1 to 60 characters", s.Name)
		case b.Accent != "" && !accentRE.MatchString(b.Accent):
			return fmt.Errorf("extension %q: branding.accent is #rrggbb", s.Name)
		}
		if b.SupportURL != "" {
			if u, err := url.Parse(b.SupportURL); err != nil || (u.Scheme != "https" && u.Scheme != "mailto") {
				return fmt.Errorf("extension %q: branding.supportUrl is an https or mailto address", s.Name)
			}
		}
	}
	return nil
}

// ViewOrigin is the origin of a view's address; the address has to be https with a host and no credentials.
func ViewOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%q is not an https address", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

// HostView is a view as the hub's page is given it.
type HostView struct {
	Ext   string `json:"ext"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	URL   string `json:"url"`
	// Origin is what the page checks the view's messages against.
	Origin string `json:"origin"`
}

// HostUI is what the hub's page draws for the adapters: their views and the branding (the first adapter's, by name).
type HostUI struct {
	Views    []HostView `json:"views"`
	Branding *Branding  `json:"branding,omitempty"`
}

// UI collects the views and the branding of every adapter, whatever its health: the page shows a view that does not
// load as failed, inside its own frame.
func (r *Registry) UI() HostUI {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := HostUI{Views: []HostView{}}
	for _, n := range r.order {
		s := r.entries[n].spec
		for _, v := range s.UI {
			o, err := ViewOrigin(v.URL)
			if err != nil {
				continue
			}
			out.Views = append(out.Views, HostView{Ext: s.Name, ID: v.ID, Label: v.Label, Icon: v.Icon, URL: v.URL, Origin: o})
		}
		if out.Branding == nil && s.Branding != nil {
			b := *s.Branding
			out.Branding = &b
		}
	}
	return out
}

// FrameOrigins are the origins the page's frame-src allows, sorted and without repeats.
func (r *Registry) FrameOrigins() []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range r.UI().Views {
		if !seen[v.Origin] {
			seen[v.Origin] = true
			out = append(out, v.Origin)
		}
	}
	sort.Strings(out)
	return out
}
