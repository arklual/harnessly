package wire

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// Backend profiles: another vendor's models (Kimi, GLM, Grok) behind one of the agent CLIs the machine has. A profile is
// no Provider: a Kimi or GLM chat is `provider: claude` and a Grok chat `provider: codex`, and `profile` names the
// vendor setup it runs on. Everything here is optional and goes only to a page that names the `profiles` cap.
//
// The key of a profile stays on the machine. The wire and the page see a masked tail («…3456») and whether a key is
// saved, never the key: it travels once, in the body of PUT .../key, and is not echoed.

// ProfileID names a profile (a preset of the runtime: «kimi», «glm», «xai»…). The runtime owns the list; the wire only
// fixes the shape of the name.
var profileID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidProfileID says whether s has the shape of a profile id.
func ValidProfileID(s string) bool { return profileID.MatchString(s) }

// ProfileVendor is whose models a profile offers; it picks the mark the page draws.
type ProfileVendor string

const (
	VendorKimi ProfileVendor = "kimi"
	VendorGLM  ProfileVendor = "glm"
	VendorGrok ProfileVendor = "grok"
)

// ProfileVendors is every ProfileVendor, in the schema's order.
var ProfileVendors = []ProfileVendor{VendorKimi, VendorGLM, VendorGrok}

// ProfileOffer is a profile a machine can start a conversation on, as MachineAgent.Profiles lists it under the agent
// CLI the profile runs through. Models are the profile's own (ids of the vendor), and a conversation of the profile
// takes only these. A page draws a profile that is not Configured as not offered.
type ProfileOffer struct {
	ID         string        `json:"id"`
	Label      string        `json:"label"`
	Vendor     ProfileVendor `json:"vendor"`
	Configured bool          `json:"configured"`
	Models     []AgentModel  `json:"models"`
}

func (p ProfileOffer) Validate() error {
	errs := []error{profileIDErr(p.ID), need("label", p.Label), in("vendor", p.Vendor, ProfileVendors)}
	for i, m := range p.Models {
		errs = append(errs, under("models["+strconv.Itoa(i)+"]", m.Validate()))
	}
	return errors.Join(errs...)
}

func profileIDErr(id string) error {
	if !ValidProfileID(id) {
		return errors.New("id: not a profile id")
	}
	return nil
}

// ProfileKey is whether a key is saved for a profile: Tail is its last four characters after an ellipsis, null while
// none is saved. It is the whole of what the runtime ever says about a key.
type ProfileKey struct {
	Configured bool    `json:"configured"`
	Tail       *string `json:"tail"`
}

func (k ProfileKey) Validate() error {
	switch {
	case k.Configured && (k.Tail == nil || !validTail(*k.Tail)):
		return errors.New("tail: a saved key shows its last four characters after «…»")
	case !k.Configured && k.Tail != nil:
		return errors.New("tail: no key, no tail")
	}
	return nil
}

func validTail(s string) bool {
	rest, ok := strings.CutPrefix(s, "…")
	return ok && utf8.RuneCountInString(rest) == 4
}

// ProfileInfo is one profile of the Settings screen: GET /api/v2/providers lists every preset of the runtime, a key
// saved or not. Via is the agent CLI it runs through. Host is the vendor's host the requests go to (shown, never
// typed: the person cannot point a key at another address). Hint is one line on the plan, in the runtime's wording,
// "" when there is none.
type ProfileInfo struct {
	ID     string        `json:"id"`
	Label  string        `json:"label"`
	Vendor ProfileVendor `json:"vendor"`
	Via    Provider      `json:"via"`
	Host   string        `json:"host"`
	Hint   string        `json:"hint"`
	Key    ProfileKey    `json:"key"`
	Models []AgentModel  `json:"models"`
	// Login is the vendor CLI's own sign-in the profile runs on while no key is saved (the Grok CLI's for xai); absent
	// while the CLI is not signed in, and from a runtime that does not read it.
	Login *ProfileLogin `json:"login,omitempty"`
}

// ProfileLogin is a sign-in of the vendor's CLI on the machine: whom it is of (an email; "" when the CLI does not say).
// The credential stays with the CLI.
type ProfileLogin struct {
	Account string `json:"account"`
}

func (p ProfileInfo) Validate() error {
	errs := []error{profileIDErr(p.ID), need("label", p.Label), in("vendor", p.Vendor, ProfileVendors), in("via", p.Via, Providers),
		need("host", p.Host), under("key", p.Key.Validate())}
	for i, m := range p.Models {
		errs = append(errs, under("models["+strconv.Itoa(i)+"]", m.Validate()))
	}
	return errors.Join(errs...)
}

// ProfileList is the answer of GET /api/v2/providers.
type ProfileList struct {
	Profiles []ProfileInfo `json:"profiles"`
}

func (l ProfileList) Validate() error {
	if l.Profiles == nil {
		return errors.New("profiles: missing")
	}
	seen := map[string]bool{}
	var errs []error
	for _, p := range l.Profiles {
		if seen[p.ID] {
			errs = append(errs, errors.New("profiles: "+p.ID+" twice"))
		}
		seen[p.ID] = true
	}
	return errors.Join(append(errs, each("profiles", l.Profiles))...)
}

// ProfileKeyEdit is the body of PUT /api/v2/providers/<id>/key. The key is a token as a vendor issues it: printable
// characters without a space, at least ProfileKeyMin and at most ProfileKeyMax of them.
type ProfileKeyEdit struct {
	Key string `json:"key"`
}

// The bounds of a key a page may save.
const (
	ProfileKeyMin = 8
	ProfileKeyMax = 512
	// MaxProfileKeyBody is the largest PUT .../key body.
	MaxProfileKeyBody = 2 << 10
)

func (e ProfileKeyEdit) Validate() error {
	if n := len(e.Key); n < ProfileKeyMin || n > ProfileKeyMax {
		return errors.New("key: not a key")
	}
	for _, c := range []byte(e.Key) {
		if c <= 0x20 || c >= 0x7f {
			return errors.New("key: not a key")
		}
	}
	return nil
}

// ProfileTestReason is why a test of a connection did not pass.
type ProfileTestReason string

const (
	// TestNoKey: no key is saved for the profile.
	TestNoKey ProfileTestReason = "no-key"
	// TestUnauthorized: the vendor refused the key (401).
	TestUnauthorized ProfileTestReason = "unauthorized"
	// TestForbidden: the key is known but the plan does not allow the model (403), or has no balance.
	TestForbidden ProfileTestReason = "forbidden"
	// TestRateLimited: the vendor answered 429.
	TestRateLimited ProfileTestReason = "rate-limited"
	// TestUnreachable: the vendor's host did not answer.
	TestUnreachable ProfileTestReason = "unreachable"
	// TestTimeout: the vendor did not answer in time.
	TestTimeout ProfileTestReason = "timeout"
	// TestRejected: the vendor answered another error; Status is its code.
	TestRejected ProfileTestReason = "rejected"
)

// ProfileTestReasons is every ProfileTestReason, in the schema's order.
var ProfileTestReasons = []ProfileTestReason{TestNoKey, TestUnauthorized, TestForbidden, TestRateLimited, TestUnreachable, TestTimeout, TestRejected}

// ProfileTest is the answer of POST /api/v2/providers/<id>/test: one minimal request to the vendor with the saved key.
// A pass has no Reason; a failure has one. Status is the vendor's HTTP code, null where it did not answer. Ms is how
// long the request took. The answer never holds the key or the vendor's body.
type ProfileTest struct {
	OK     bool               `json:"ok"`
	Reason *ProfileTestReason `json:"reason"`
	Status *int               `json:"status"`
	Ms     int64              `json:"ms"`
}

func (t ProfileTest) Validate() error {
	errs := []error{nonNegative("ms", t.Ms)}
	switch {
	case t.OK && (t.Reason != nil):
		errs = append(errs, errors.New("reason: a pass has none"))
	case !t.OK && t.Reason == nil:
		errs = append(errs, errors.New("reason: a failure has one"))
	case t.Reason != nil:
		errs = append(errs, in("reason", *t.Reason, ProfileTestReasons))
	}
	if t.Status != nil && (*t.Status < 100 || *t.Status > 599) {
		errs = append(errs, errors.New("status: not an HTTP code"))
	}
	return errors.Join(errs...)
}

// ErrProfileUnknown: the runtime has no profile of that id.
var ErrProfileUnknown = errors.New("wire: no such profile")

// ProfilesSource keeps the profiles of this machine and their keys.
type ProfilesSource interface {
	Profiles(ctx context.Context) (ProfileList, error)
	// SetProfileKey saves the key of a profile, replacing a saved one.
	SetProfileKey(ctx context.Context, id, key string) (ProfileKey, error)
	// RemoveProfileKey deletes the saved key; a profile with none stays as it is.
	RemoveProfileKey(ctx context.Context, id string) (ProfileKey, error)
	// TestProfile makes one minimal request to the vendor with the saved key.
	TestProfile(ctx context.Context, id string) (ProfileTest, error)
}

// profiles serves /api/v2/providers: GET lists the profiles, PUT and DELETE .../<id>/key save and remove a key, POST
// .../<id>/test tries the connection. Saving a key changes a file of the person's, so every write is refused to other
// origins (origin.go). The body of a PUT is not logged and the key is not echoed.
func (s *Server) profiles(w http.ResponseWriter, r *http.Request, rest string) {
	if s.Profiles == nil {
		refuse(w, RefuseNotFound)
		return
	}
	if rest == "" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		got, err := s.Profiles.Profiles(r.Context())
		if err != nil {
			s.profilesFailed(w, r, err)
			return
		}
		s.writeChecked(w, r, got)
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || !ValidProfileID(parts[0]) || (parts[1] != "key" && parts[1] != "test") {
		refuse(w, RefuseNotFound)
		return
	}
	id := parts[0]
	switch {
	case parts[1] == "key" && r.Method == http.MethodPut:
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxProfileKeyBody))
		var edit ProfileKeyEdit
		if err == nil {
			err = decodeStrict(raw, &edit)
		}
		if err == nil {
			err = edit.Validate()
		}
		if err != nil {
			refuse(w, RefuseBadRequest)
			return
		}
		got, err := s.Profiles.SetProfileKey(r.Context(), id, edit.Key)
		s.profileAnswer(w, r, got, err)
	case parts[1] == "key" && r.Method == http.MethodDelete:
		got, err := s.Profiles.RemoveProfileKey(r.Context(), id)
		s.profileAnswer(w, r, got, err)
	case parts[1] == "test" && r.Method == http.MethodPost:
		got, err := s.Profiles.TestProfile(r.Context(), id)
		s.profileAnswer(w, r, got, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) profileAnswer(w http.ResponseWriter, r *http.Request, v interface{ Validate() error }, err error) {
	if err != nil {
		s.profilesFailed(w, r, err)
		return
	}
	s.writeChecked(w, r, v)
}

func (s *Server) profilesFailed(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrProfileUnknown):
		refuse(w, RefuseNotFound)
	case r.Context().Err() != nil || errors.Is(err, context.DeadlineExceeded):
		refuse(w, RefuseTimeout)
	default:
		jsonlog.Log("warn", "v2: the profiles failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	}
}

// agentsWithoutProfiles is as as a page that does not read profiles gets them, and whether it differs from as.
func agentsWithoutProfiles(as []MachineAgent) ([]MachineAgent, bool) {
	if !slices.ContainsFunc(as, func(a MachineAgent) bool { return len(a.Profiles) > 0 }) {
		return as, false
	}
	out := slices.Clone(as)
	for i := range out {
		out[i].Profiles = nil
	}
	return out, true
}

// modelWithoutProfile is m as a page that does not read profiles gets it.
func modelWithoutProfile(m *Model) (*Model, bool) {
	if m == nil || m.Profile == "" {
		return m, false
	}
	c := *m
	c.Profile = ""
	return &c, true
}
