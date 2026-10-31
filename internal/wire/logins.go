package wire

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// Signing an agent in from the page. The CLI does the signing in (the runtime drives it through a terminal of its own);
// the page only shows the link, takes the code the person got at the vendor's site, and says how it went. Nothing the
// CLI prints beyond the link and the one-time code reaches the page, and the code the person types goes to the CLI and
// is not kept. An API key goes to the CLI's standard input and nowhere else.

// LoginMode is how the person signs in: with the subscription (a link and a code to paste back), with the console account
// (the same for API billing), or with a device code (Codex: a link and a code to type at the vendor, nothing to paste).
type LoginMode string

const (
	LoginSubscription LoginMode = "subscription"
	LoginConsole      LoginMode = "console"
	LoginDevice       LoginMode = "device"
)

// LoginModes is every LoginMode, in the schema's order.
var LoginModes = []LoginMode{LoginSubscription, LoginConsole, LoginDevice}

// LoginModesOf are the modes a CLI takes: Claude the first two, Codex the device code.
func LoginModesOf(p Provider) []LoginMode {
	switch p {
	case ProviderClaude:
		return []LoginMode{LoginSubscription, LoginConsole}
	case ProviderCodex:
		return []LoginMode{LoginDevice}
	}
	return nil
}

// LoginState is where a sign-in is. `starting`: the CLI is starting and has no link yet. `awaiting`: the link is out
// (and `input` says whether a code is to be pasted back). `checking`: the code went to the CLI, which is checking it.
// The last four end the flow.
type LoginState string

const (
	LoginStarting  LoginState = "starting"
	LoginAwaiting  LoginState = "awaiting"
	LoginChecking  LoginState = "checking"
	LoginDone      LoginState = "done"
	LoginFailed    LoginState = "failed"
	LoginCancelled LoginState = "cancelled"
	LoginExpired   LoginState = "expired"
)

// LoginStates is every LoginState, in the schema's order.
var LoginStates = []LoginState{LoginStarting, LoginAwaiting, LoginChecking, LoginDone, LoginFailed, LoginCancelled, LoginExpired}

// LoginInput is what the flow wants from the page: a code to paste back, or nothing (the person finishes at the vendor).
type LoginInput string

const (
	LoginInputCode LoginInput = "code"
	LoginInputNone LoginInput = "none"
)

// LoginInputs is every LoginInput, in the schema's order.
var LoginInputs = []LoginInput{LoginInputCode, LoginInputNone}

// LoginReason is why a flow failed.
type LoginReason string

const (
	// LoginNotInstalled: the CLI is not on the machine.
	LoginNotInstalled LoginReason = "not-installed"
	// LoginStartFailed: the CLI did not start or did not give a link.
	LoginStartFailed LoginReason = "start-failed"
	// LoginCodeRejected: the CLI did not take the code.
	LoginCodeRejected LoginReason = "code-rejected"
	// LoginRefused: the CLI ended without a sign-in (the person said no at the vendor, or the code was never entered).
	LoginRefused LoginReason = "refused"
	// LoginTimeout: nobody finished in time.
	LoginTimeout LoginReason = "timeout"
)

// LoginReasons is every LoginReason, in the schema's order.
var LoginReasons = []LoginReason{LoginNotInstalled, LoginStartFailed, LoginCodeRejected, LoginRefused, LoginTimeout}

// LoginFlow is one sign-in. `url` is the vendor's link to open; `userCode` is the one-time code to type there (device
// mode only, it is no secret: it opens nothing without the person's own sign-in). `expiresAt` is when the runtime gives
// the flow up.
type LoginFlow struct {
	ID        string       `json:"id"`
	Provider  Provider     `json:"provider"`
	Mode      LoginMode    `json:"mode"`
	State     LoginState   `json:"state"`
	Input     LoginInput   `json:"input"`
	URL       *string      `json:"url"`
	UserCode  *string      `json:"userCode"`
	StartedAt Ms           `json:"startedAt"`
	ExpiresAt Ms           `json:"expiresAt"`
	Reason    *LoginReason `json:"reason"`
}

var loginID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (f LoginFlow) Validate() error {
	errs := []error{in("provider", f.Provider, Providers), in("mode", f.Mode, LoginModes), in("state", f.State, LoginStates),
		in("input", f.Input, LoginInputs), nonNegative("startedAt", int64(f.StartedAt)), nonNegative("expiresAt", int64(f.ExpiresAt))}
	if !loginID.MatchString(f.ID) {
		errs = append(errs, errors.New("id: not a flow id"))
	}
	if !slices.Contains(LoginModesOf(f.Provider), f.Mode) {
		errs = append(errs, errors.New("mode: the CLI does not take it"))
	}
	if f.URL != nil && !strings.HasPrefix(*f.URL, "https://") {
		errs = append(errs, errors.New("url: not an https link"))
	}
	if f.State == LoginAwaiting && f.URL == nil {
		errs = append(errs, errors.New("url: an awaiting flow has a link"))
	}
	if (f.State == LoginFailed) != (f.Reason != nil) {
		errs = append(errs, errors.New("reason: a failed flow has one, and only a failed one"))
	}
	if f.Reason != nil {
		errs = append(errs, in("reason", *f.Reason, LoginReasons))
	}
	if f.UserCode != nil && f.Mode != LoginDevice {
		errs = append(errs, errors.New("userCode: device mode only"))
	}
	return errors.Join(errs...)
}

// LoginStatus is the answer of the sign-in routes: the flow, or null when none was started since the runtime began.
type LoginStatus struct {
	Flow *LoginFlow `json:"flow"`
}

func (s LoginStatus) Validate() error {
	if s.Flow == nil {
		return nil
	}
	return under("flow", s.Flow.Validate())
}

// LoginStart is the body of POST /api/v2/agents/<provider>/login.
type LoginStart struct {
	Mode LoginMode `json:"mode"`
}

// LoginCode is the body of POST /api/v2/agents/<provider>/login/code: the code the vendor's site showed, for the flow
// the page was shown (a code never goes to another flow than the one it was asked for).
type LoginCode struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

// The bounds of a code a page may submit: what the vendors' codes are made of (letters, digits and `-_#.~`).
const (
	LoginCodeMin = 8
	LoginCodeMax = 512
	// MaxLoginBody is the largest body of a sign-in route.
	MaxLoginBody = 2 << 10
)

var loginCode = regexp.MustCompile(`^[A-Za-z0-9_#.~/+=-]+$`)

func (c LoginCode) Validate() error {
	errs := []error{}
	if !loginID.MatchString(c.ID) {
		errs = append(errs, errors.New("id: not a flow id"))
	}
	if n := len(c.Code); n < LoginCodeMin || n > LoginCodeMax || !loginCode.MatchString(c.Code) {
		errs = append(errs, errors.New("code: not a code"))
	}
	return errors.Join(errs...)
}

func (s LoginStart) Validate() error { return in("mode", s.Mode, LoginModes) }

// LoginDoneEv is the answer of the routes that end in a state of the CLI's account and nothing else (an API key saved,
// a sign-out): the page asks the agent's settings for what the account is now.
type LoginOK struct {
	OK bool `json:"ok"`
}

func (LoginOK) Validate() error { return nil }

// Errors a LoginSource answers with.
var (
	// ErrLoginBusy: a flow of that CLI is on.
	ErrLoginBusy = errors.New("wire: a sign-in is on")
	// ErrLoginRate: too many sign-ins were started lately.
	ErrLoginRate = errors.New("wire: too many sign-ins")
	// ErrLoginUnsupported: the CLI does not take that mode or that way of signing in.
	ErrLoginUnsupported = errors.New("wire: the CLI does not sign in that way")
	// ErrLoginStale: the flow is not the one asked for, or is not at a state that takes the code.
	ErrLoginStale = errors.New("wire: not that sign-in")
	// ErrLoginInput: the CLI rejected the input (a key it did not take).
	ErrLoginInput = errors.New("wire: the CLI did not take it")
	// ErrLoginNotInstalled: the CLI is not on the machine.
	ErrLoginNotInstalled = errors.New("wire: the CLI is not installed")
)

// LoginSource signs the machine's agents in and out.
type LoginSource interface {
	// Login is the last flow of the CLI (or the one on).
	Login(ctx context.Context, p Provider) (LoginStatus, error)
	StartLogin(ctx context.Context, p Provider, mode LoginMode) (LoginStatus, error)
	SubmitLoginCode(ctx context.Context, p Provider, id, code string) (LoginStatus, error)
	CancelLogin(ctx context.Context, p Provider) (LoginStatus, error)
	// SaveAPIKey gives the CLI an API key on its standard input.
	SaveAPIKey(ctx context.Context, p Provider, key string) error
	// Logout signs the CLI out.
	Logout(ctx context.Context, p Provider) error
}

// logins serves /api/v2/agents/<provider>/...: GET login, POST login (start), POST login/code, DELETE login (cancel),
// PUT key, POST logout. Every write is for the same origin only (origin.go). Bodies are not logged and answers never
// carry a code or a key.
func (s *Server) logins(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	p := Provider(parts[0])
	if s.Logins == nil || !slices.Contains(Providers, p) || len(parts) < 2 {
		refuse(w, RefuseNotFound)
		return
	}
	route := strings.Join(parts[1:], "/")
	var status LoginStatus
	var err error
	switch {
	case route == "login" && r.Method == http.MethodGet:
		status, err = s.Logins.Login(r.Context(), p)
	case route == "login" && r.Method == http.MethodPost:
		var body LoginStart
		if !s.readLoginBody(w, r, &body) {
			return
		}
		status, err = s.Logins.StartLogin(r.Context(), p, body.Mode)
	case route == "login/code" && r.Method == http.MethodPost:
		var body LoginCode
		if !s.readLoginBody(w, r, &body) {
			return
		}
		status, err = s.Logins.SubmitLoginCode(r.Context(), p, body.ID, body.Code)
	case route == "login" && r.Method == http.MethodDelete:
		status, err = s.Logins.CancelLogin(r.Context(), p)
	case route == "key" && r.Method == http.MethodPut:
		var body ProfileKeyEdit
		if !s.readLoginBody(w, r, &body) {
			return
		}
		s.loginAnswer(w, r, LoginOK{OK: true}, s.Logins.SaveAPIKey(r.Context(), p, body.Key))
		return
	case route == "logout" && r.Method == http.MethodPost:
		s.loginAnswer(w, r, LoginOK{OK: true}, s.Logins.Logout(r.Context(), p))
		return
	case route == "login" || route == "login/code" || route == "key" || route == "logout":
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	default:
		refuse(w, RefuseNotFound)
		return
	}
	s.loginAnswer(w, r, status, err)
}

// readLoginBody reads and checks a body; false when it answered.
func (s *Server) readLoginBody(w http.ResponseWriter, r *http.Request, v interface{ Validate() error }) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxLoginBody))
	if err == nil {
		err = decodeStrict(raw, v)
	}
	if err == nil {
		err = v.Validate()
	}
	if err != nil {
		refuse(w, RefuseBadRequest)
		return false
	}
	return true
}

func (s *Server) loginAnswer(w http.ResponseWriter, r *http.Request, v interface{ Validate() error }, err error) {
	switch {
	case err == nil:
		s.writeChecked(w, r, v)
	case errors.Is(err, ErrLoginBusy), errors.Is(err, ErrLoginStale):
		refuse(w, RefuseConflict)
	case errors.Is(err, ErrLoginRate):
		refuse(w, RefuseRateLimited)
	case errors.Is(err, ErrLoginUnsupported), errors.Is(err, ErrLoginNotInstalled):
		refuse(w, RefuseUnsupported)
	case errors.Is(err, ErrLoginInput):
		refuse(w, RefuseBadRequest)
	case r.Context().Err() != nil || errors.Is(err, context.DeadlineExceeded):
		refuse(w, RefuseTimeout)
	default:
		jsonlog.Log("warn", "v2: the sign-in failed", jsonlog.F{K: "path", V: r.URL.Path})
		refuse(w, RefuseInternal)
	}
}
