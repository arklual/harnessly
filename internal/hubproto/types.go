package hubproto

import (
	"fmt"
	"strings"
)

// Version is protocol 2: protocol 1 plus the operations and events below.
const Version = 2

// Close codes of the hub on an agent link.
const (
	CloseSwitchedOff   = 4003 // the pair is switched off (v1): the agent waits a minute
	CloseRevoked       = 4010 // the machine was revoked: the agent stops dialing until restart
	CloseReplaced      = 4011 // a newer link of the same machine: the agent redials normally
	CloseProtocolOld   = 4012 // the agent's protocol is too old: the agent waits an hour
	StatusUpgradeAgent = 426  // the HTTP answer to a dial of an agent older than the hub's minimum
)

// Profile is the isolation the machine was connected with.
type Profile string

const (
	ProfileDir       Profile = "dir"
	ProfileContainer Profile = "container"
	ProfileFull      Profile = "full"
)

// Valid is one of the three profiles.
func (p Profile) Valid() bool { return p == ProfileDir || p == ProfileContainer || p == ProfileFull }

// Via is how the machine was connected.
type Via string

const (
	ViaCommand   Via = "command"
	ViaSSH       Via = "ssh"
	ViaLocal     Via = "local"
	ViaContainer Via = "container"
)

// Valid is one of the four ways.
func (v Via) Valid() bool {
	return v == ViaCommand || v == ViaSSH || v == ViaLocal || v == ViaContainer
}

// Providers the agent knows. Gemini is a card in the UI and nothing here.
const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
	ProviderGemini = "gemini"
)

// Operations the hub asks of the agent (the `op` of a request).
const (
	OpHello         = "hello"
	OpLinkSubscribe = "link.subscribe"
	OpPreflightRun  = "preflight.run"
	OpAgentsStatus  = "agents.status"
	OpAgentsInstall = "agents.install"
	OpProviderCheck = "provider.check"
	OpProxyGet      = "proxy.get"
	OpProxySet      = "proxy.set"
	OpProxyClear    = "proxy.clear"
	OpLoginStart    = "login.start"
	OpLoginCode     = "login.code"
	OpLoginCancel   = "login.cancel"
	OpKeyDeliver    = "key.deliver"
	OpKeyVerify     = "key.verify"
	OpEnvDeliver    = "env.deliver"
	OpEnvRemove     = "env.remove"
	OpGitkeyEnsure  = "gitkey.ensure"
	OpRuntimeSess   = "runtime.session"
	OpTunnelOpen    = "tunnel.open"
)

// Events the agent sends (the `event` of a frame).
const (
	EventJob       = "job"
	EventLogin     = "login"
	EventAgents    = "agents"
	EventPreflight = "preflight"
	EventPolicy    = "policy"
)

// Error codes at the start of an error string of protocol 2 ("policy_denied: terminal is off here").
const (
	CodePolicyDenied = "policy_denied"
	CodeUnsupported  = "unsupported"
	CodeBusy         = "busy"
	CodeNotFound     = "not_found"
	CodeInvalid      = "invalid"
	CodeFailed       = "failed"
)

// Error is an operation's failure: a code and a text, written "code: text".
type Error struct {
	Code string
	Text string
}

func (e *Error) Error() string { return e.Code + ": " + e.Text }

// Errf is an Error with a formatted text.
func Errf(code, format string, args ...any) *Error {
	return &Error{Code: code, Text: fmt.Sprintf(format, args...)}
}

// ParseError splits an error string of protocol 2; a string without a known code is `failed`.
func ParseError(s string) *Error {
	code, text, ok := strings.Cut(s, ": ")
	switch code {
	case CodePolicyDenied, CodeUnsupported, CodeBusy, CodeNotFound, CodeInvalid, CodeFailed:
		if ok {
			return &Error{Code: code, Text: text}
		}
	}
	return &Error{Code: CodeFailed, Text: s}
}

// RegisterRequest is the body of POST /agent/register.
type RegisterRequest struct {
	Code         string  `json:"code"`
	PublicKey    string  `json:"publicKey"`
	EncKey       string  `json:"encKey"`
	Hostname     string  `json:"hostname"`
	OS           string  `json:"os"`
	Arch         string  `json:"arch"`
	UnixUser     string  `json:"unixUser"`
	Profile      Profile `json:"profile"`
	Via          Via     `json:"via"`
	AgentVersion string  `json:"agentVersion"`
}

// RegisterResponse is the hub's 201.
type RegisterResponse struct {
	MachineID       string  `json:"machineId"`
	DialURL         string  `json:"dialUrl"`
	RuntimeBasePath string  `json:"runtimeBasePath"`
	HubURL          string  `json:"hubUrl"`
	SpaceName       *string `json:"spaceName"`
}

// Hello is the answer to `hello` on a protocol 2 link.
type Hello struct {
	Protocol     int      `json:"protocol"`
	AgentVersion string   `json:"agentVersion"`
	User         string   `json:"user"`
	Home         string   `json:"home"`
	Hostname     string   `json:"hostname"`
	Features     []string `json:"features"`
	MachineID    string   `json:"machineId"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Profile      Profile  `json:"profile"`
	Policy       Policy   `json:"policy"`
}

// Policy is what the machine's own policy file allows. The hub reads it and cannot change it.
type Policy struct {
	Terminal       bool     `json:"terminal"`
	AgentLogin     bool     `json:"agentLogin"`
	Keys           bool     `json:"keys"`
	Env            bool     `json:"env"`
	Install        bool     `json:"install"`
	RuntimeSession bool     `json:"runtimeSession"`
	DenyRoutes     []string `json:"denyRoutes"`
	Source         string   `json:"source"`
}

// Check is one line of a preflight report.
type Check struct {
	ID      string `json:"id"`
	Level   string `json:"level"` // ok, warn, fail
	Message string `json:"message"`
}

// Check levels.
const (
	LevelOK   = "ok"
	LevelWarn = "warn"
	LevelFail = "fail"
)

// Disk is the work folder's filesystem.
type Disk struct {
	Path  string `json:"path"`
	Total int64  `json:"total"`
	Free  int64  `json:"free"`
}

// ToolState is one tool in a report. Compatible is only set for tmux.
type ToolState struct {
	Installed  bool    `json:"installed"`
	Version    *string `json:"version"`
	Compatible *bool   `json:"compatible,omitempty"`
}

// Tools are the tools a report looks for.
type Tools struct {
	Tmux   ToolState `json:"tmux"`
	Git    ToolState `json:"git"`
	Node   ToolState `json:"node"`
	Claude ToolState `json:"claude"`
	Codex  ToolState `json:"codex"`
}

// PreflightReport is what `preflight.run` answers and the `preflight` event carries.
type PreflightReport struct {
	At           int64   `json:"at"`
	OSName       string  `json:"osName"`
	OS           string  `json:"os"`
	Arch         string  `json:"arch"`
	CPUs         int     `json:"cpus"`
	MemTotal     int64   `json:"memTotal"`
	MemAvailable int64   `json:"memAvailable"`
	Disk         Disk    `json:"disk"`
	Tools        Tools   `json:"tools"`
	Checks       []Check `json:"checks"`
}

// Login states of an AgentStatus.
const (
	LoggedIn     = "logged_in"
	LoggedOut    = "logged_out"
	LoginUnknown = "unknown"
)

// Auth modes of an AgentStatus.
const (
	AuthSubscription = "subscription"
	AuthAPIKey       = "api_key"
	AuthCloud        = "cloud"
)

// AgentStatus is how one agent stands on the machine.
type AgentStatus struct {
	Provider     string         `json:"provider"`
	Soon         bool           `json:"soon,omitempty"`
	Installed    bool           `json:"installed"`
	Version      *string        `json:"version"`
	Login        string         `json:"login"`
	AuthMode     *string        `json:"authMode"`
	OverriddenBy *string        `json:"overriddenBy"`
	Access       *ProviderCheck `json:"access"`
}

// Verdicts of a provider check.
const (
	VerdictOK       = "ok"
	VerdictNetwork  = "network"
	VerdictRejected = "rejected"
)

// HostCheck is DNS, TCP and TLS to one host.
type HostCheck struct {
	Host  string  `json:"host"`
	DNS   bool    `json:"dns"`
	TCP   bool    `json:"tcp"`
	TLS   bool    `json:"tls"`
	MS    int64   `json:"ms"`
	Error *string `json:"error"`
}

// ProviderCheck is the verdict on the network to a provider's servers, made on the machine.
type ProviderCheck struct {
	Provider string      `json:"provider"`
	Verdict  string      `json:"verdict"`
	At       int64       `json:"at"`
	Via      string      `json:"via"` // direct, proxy
	Host     string      `json:"host"`
	Status   *int        `json:"status"`
	Detail   *string     `json:"detail"`
	Hosts    []HostCheck `json:"hosts"`
}

// ProxyEntry is a proxy as the hub may see it: never with credentials.
type ProxyEntry struct {
	Scheme string  `json:"scheme"` // http, https, socks5
	Host   string  `json:"host"`
	Bridge *string `json:"bridge"`
}

// ProxySummary is the proxies by scope.
type ProxySummary struct {
	All    *ProxyEntry `json:"all"`
	Claude *ProxyEntry `json:"claude"`
	Codex  *ProxyEntry `json:"codex"`
}

// Job states and kinds.
const (
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"

	JobAgentInstall = "agent-install"
)

// Step states of a job.
const (
	StepWait = "wait"
	StepRun  = "run"
	StepOK   = "ok"
	StepFail = "fail"
)

// JobStep is one step of a job.
type JobStep struct {
	ID     string  `json:"id"`
	State  string  `json:"state"`
	Detail *string `json:"detail"`
}

// JobError says why a job failed.
type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Job is an install on the machine.
type Job struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Provider *string        `json:"provider"`
	State    string         `json:"state"`
	Steps    []JobStep      `json:"steps"`
	Error    *JobError      `json:"error"`
	Check    *ProviderCheck `json:"check"`
}

// Login states and methods.
const (
	LoginStarting  = "starting"
	LoginURL       = "url"
	LoginWaiting   = "waiting"
	LoginVerifying = "verifying"
	LoginDone      = "done"
	LoginFailed    = "failed"
	LoginExpired   = "expired"
	LoginCancelled = "cancelled"

	MethodLink    = "link"
	MethodConsole = "console"
)

// Login is an agent login in progress or finished.
type Login struct {
	ID         string       `json:"id"`
	Provider   string       `json:"provider"`
	Method     string       `json:"method"`
	State      string       `json:"state"`
	URL        *string      `json:"url"`
	UserCode   *string      `json:"userCode"`
	NeedsCode  bool         `json:"needsCode"`
	ConsoleURL *string      `json:"consoleUrl"`
	ExpiresAt  *int64       `json:"expiresAt"`
	Error      *string      `json:"error"`
	Status     *AgentStatus `json:"status"`
}

// Key check verdicts.
const (
	KeyAccepted = "accepted"
	KeyRejected = "rejected"
	KeyNetwork  = "network"
	KeyAbsent   = "absent"
)

// KeyCheck is what the machine made of an API key.
type KeyCheck struct {
	Provider string `json:"provider"`
	Present  bool   `json:"present"`
	Verdict  string `json:"verdict"`
	Status   *int   `json:"status"`
	Saved    bool   `json:"saved,omitempty"`
}

// SessionGrant is the answer to `runtime.session`.
type SessionGrant struct {
	CookieName string `json:"cookieName"`
	Token      string `json:"token"`
	ExpiresAt  int64  `json:"expiresAt"`
}

// FeaturesV2 are the features protocol 2 adds to the v1 list in `hello`.
var FeaturesV2 = []string{"preflight", "agents", "login", "provider-check", "proxy", "keys", "env", "gitkey", "runtime-session"}
