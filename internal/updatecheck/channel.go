package updatecheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The update channels of an installation. "latest" is every published release. "stable" is the release the stable
// pointer names: a small file beside the releases that moves only when somebody promotes a release (tools/relchannel,
// docs/update-channels.md), never by a merge.
const (
	ChannelStable = "stable"
	ChannelLatest = "latest"
)

// DefaultStableURL is the project's stable pointer: an asset of the "channels" release, read without the API.
const DefaultStableURL = "https://github.com/ly-solutions/harnessly/releases/download/channels/stable.json"

// PointerSchema is the version of the pointer's format.
const PointerSchema = 1

// DefaultWindow is how long a release must have been out before it may become stable.
const DefaultWindow = 7 * 24 * time.Hour

// WithdrawnMark in the title or the notes of a release says it was withdrawn: it never becomes stable.
const WithdrawnMark = "[withdrawn]"

// ParseChannel reads a channel setting. Empty is the default, stable. ok is false for a word that is no channel; the
// answer is then stable too: the careful one.
func ParseChannel(raw string) (channel string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ChannelStable:
		return ChannelStable, true
	case ChannelLatest:
		return ChannelLatest, true
	}
	return ChannelStable, false
}

// Pointer is the stable pointer's file.
type Pointer struct {
	Schema  int    `json:"schema"`
	Channel string `json:"channel"`
	// Version is the stable release, X.Y.Z.
	Version string `json:"version"`
	// URL is the release's page.
	URL string `json:"url,omitempty"`
	// ReleasedAt is when the release was published, PromotedAt when it became stable; RFC 3339.
	ReleasedAt string `json:"released_at,omitempty"`
	PromotedAt string `json:"promoted_at,omitempty"`
}

// ParsePointer reads a stable pointer. Fields it does not know are ignored, so a later format that only adds reads too.
func ParsePointer(body []byte) (Pointer, error) {
	var p Pointer
	if err := json.Unmarshal(body, &p); err != nil {
		return Pointer{}, fmt.Errorf("stable pointer: %w", err)
	}
	if p.Schema != PointerSchema {
		return Pointer{}, fmt.Errorf("stable pointer: schema %d, this program reads %d", p.Schema, PointerSchema)
	}
	if p.Channel != ChannelStable {
		return Pointer{}, fmt.Errorf("stable pointer: it is of the channel %q", p.Channel)
	}
	v, ok := Parse(p.Version)
	if !ok || v.Pre != "" {
		return Pointer{}, fmt.Errorf("stable pointer: version %q is not X.Y.Z", p.Version)
	}
	p.Version = v.String()
	return p, nil
}

// Candidate is a published release as the releases API describes it, the part a promotion reads.
type Candidate struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

// Promotion is what a promotion is decided by.
type Promotion struct {
	// Now is the time of the promotion.
	Now time.Time
	// Window is how long the release must have been out; zero means DefaultWindow.
	Window time.Duration
	// Current is the pointer in place, nil when there is none yet.
	Current *Pointer
	// AllowOlder lets the pointer move back to an older release (taking a bad stable one away).
	AllowOlder bool
}

// ErrRefused wraps every reason a release may not become stable.
var ErrRefused = errors.New("not promoted")

func refuse(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, a...))
}

// Promote decides whether the release may become the stable one and answers with the pointer that says so. It is
// refused (ErrRefused) when the release is a draft, a pre-release or withdrawn, has been out for less than the window,
// or is older than the stable one in place.
func Promote(rel Candidate, p Promotion) (Pointer, error) {
	v, ok := Parse(rel.TagName)
	if !ok || v.Pre != "" {
		return Pointer{}, refuse("%q is not a release version X.Y.Z", rel.TagName)
	}
	if rel.Draft || rel.Prerelease {
		return Pointer{}, refuse("%s is a draft or a pre-release", v)
	}
	if withdrawn(rel.Name) || withdrawn(rel.Body) {
		return Pointer{}, refuse("%s is withdrawn (%s in its title or notes)", v, WithdrawnMark)
	}
	published, err := time.Parse(time.RFC3339, rel.PublishedAt)
	if err != nil {
		return Pointer{}, refuse("%s has no publication time (%q)", v, rel.PublishedAt)
	}
	window := p.Window
	if window <= 0 {
		window = DefaultWindow
	}
	if age := p.Now.Sub(published); age < window {
		return Pointer{}, refuse("%s has been out for %s, the window is %s: it may be promoted from %s",
			v, age.Round(time.Minute), window, published.Add(window).UTC().Format(time.RFC3339))
	}
	if p.Current != nil && !p.AllowOlder {
		if cur, ok := Parse(p.Current.Version); ok && Compare(v, cur) < 0 {
			return Pointer{}, refuse("%s is older than the stable %s", v, cur)
		}
	}
	return Pointer{Schema: PointerSchema, Channel: ChannelStable, Version: v.String(), URL: safeURL(rel.HTMLURL),
		ReleasedAt: published.UTC().Format(time.RFC3339), PromotedAt: p.Now.UTC().Format(time.RFC3339)}, nil
}

func withdrawn(text string) bool {
	return strings.Contains(strings.ToLower(text), WithdrawnMark)
}
