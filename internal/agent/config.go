package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// Config is everything the agent takes from its environment. Names a particular platform gives its routes, headers
// and directories are settings with neutral defaults, so one binary serves any deployment.
type Config struct {
	Home    string
	Host    string
	Port    int
	Version string
	// ScanInterval is the full transcript scan: a safety net for events the watcher missed.
	ScanInterval time.Duration

	// DataDir is the runtime's data directory: state files, agent.json, eval runs.
	DataDir string
	// ConfigFile holds the runtime's authSecret when SecretFile is not given.
	ConfigFile string
	SecretFile string

	// RuntimePort is the runtime's own HTTP API on loopback; FilesPort the disk service next to it. Zero: not here.
	RuntimePort int
	FilesPort   int

	// UsageFeed builds what fills the usage outbox for a hub. It is a function handed in by cmd/agent because
	// the usage report that reads the transcripts imports this package. nil: the machine reports no spend.
	UsageFeed func(UsageDeps) (UsageFeed, error)

	// ClusterURL and RuntimeID make the agent dial the cluster itself; without both it only listens.
	ClusterURL string
	RuntimeID  string

	// ListenAudience is the audience of a token the cluster presents on /v1; DialAudience the one this agent signs
	// when it dials. They differ so a token for one direction is never accepted in the other.
	ListenAudience string
	DialAudience   string

	// RuntimeAPI is the prefix of the runtime routes the agent calls: <prefix>/machines, <prefix>/automations/start…
	RuntimeAPI       string
	StateChangedPath string
	StatusPath       string
	SyncPath         string
	// V2Path is the runtime's v2 API: <prefix>/replica, <prefix>/conversations/<id>/turns.
	V2Path string
	// PeersPath is the runtime's API of messages between machines: <prefix>/inbox, <prefix>/directory.
	PeersPath     string
	RuntimeCookie string
	// CLITokenHeaders carry the runtime's cli-token when the agent tells it a state file changed.
	CLITokenHeaders []string

	// LocalTokenHeader carries the token from agent.json on the loopback routes the CLIs call.
	LocalTokenHeader string
	// ReportPortEnv is the variable a launched automation conversation finds this agent's port in.
	ReportPortEnv string
	// ExtraStateFiles are files of the data directory's root the platform also keeps a copy of.
	ExtraStateFiles []string

	// Auth is AGENT_AUTH: "auto" (a hub when machine.json exists, else the closed cluster), "ed25519" or "hs256".
	Auth string
	// Machine is machine.json when the agent dials a hub (nil: it does not).
	Machine     *MachineFile
	MachineFile string
	// ObserveMachine is the machine.json of a hub that only sees this machine while the agent keeps its closed cluster's
	// link (AGENT_HUB_OBSERVE_FILE); nil when there is none, or when the agent is a hub's machine already.
	ObserveMachine     *MachineFile
	ObserveMachineFile string
	// PolicyFile is AGENT_POLICY_FILE ("": /etc/harnessly/policy.toml if there is one, else the account's).
	PolicyFile string
	// EnvDir holds the env files the agents' launches source; WorkDir is the folder whose disk the preflight reports.
	EnvDir  string
	WorkDir string
}

// Authentication of the dial.
const (
	AuthAuto    = "auto"
	AuthEd25519 = "ed25519"
	AuthHS256   = "hs256"
)

const (
	ListenPath = "/v1"
	DialPath   = "/agent/v1"
	Protocol   = 1
)

// BuildVersion is set at link time: -ldflags "-X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=1.2.3".
var BuildVersion = "dev"

func env(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) (int, error) {
	v := env(name, "")
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s is not a number: %q", name, v)
	}
	return n, nil
}

// LoadConfig reads the environment.
func LoadConfig() (Config, error) {
	home := env("HOME", "")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return Config{}, err
		}
		home = h
	}
	c := Config{Home: home}
	var err error
	if c.Port, err = envInt("AGENT_PORT", 0); err != nil {
		return c, err
	}
	if c.Port == 0 {
		return c, fmt.Errorf("AGENT_PORT is not set")
	}
	scanMs, err := envInt("AGENT_SCAN_MS", 30_000)
	if err != nil {
		return c, err
	}
	c.ScanInterval = time.Duration(scanMs) * time.Millisecond
	if c.RuntimePort, err = envInt("RUNTIME_PORT", 0); err != nil {
		return c, err
	}
	if c.FilesPort, err = envInt("FILES_PORT", 0); err != nil {
		return c, err
	}
	c.Host = env("AGENT_HOST", "127.0.0.1")
	c.Version = env("AGENT_VERSION", BuildVersion)
	c.DataDir = env("AGENT_DATA_DIR", filepath.Join(home, ".harnessly"))
	// Once, at the start: a link on the way there lies in the home folder, where an agent could repoint it; the wall can
	// pin a folder but not a link.
	if real, err := filepath.EvalSymlinks(c.DataDir); err == nil {
		c.DataDir = real
	}
	c.ConfigFile = env("AGENT_CONFIG", filepath.Join(c.DataDir, "config.json"))
	c.SecretFile = env("AGENT_SECRET_FILE", "")
	c.ClusterURL = env("AGENT_CLUSTER_URL", "")
	c.RuntimeID = env("AGENT_RUNTIME_ID", "")
	c.ListenAudience = env("AGENT_AUDIENCE", "agent")
	c.DialAudience = env("AGENT_DIAL_AUDIENCE", "ingest")
	c.Auth = env("AGENT_AUTH", AuthAuto)
	c.MachineFile = env("AGENT_MACHINE_FILE", DefaultMachineFile(c.DataDir))
	switch c.Auth {
	case AuthAuto:
		m, err := LoadMachine(c.MachineFile)
		switch {
		case err == nil:
			c.Machine = m
		case !errors.Is(err, os.ErrNotExist):
			return c, err
		}
	case AuthEd25519:
		m, err := LoadMachine(c.MachineFile)
		if err != nil {
			return c, fmt.Errorf("AGENT_AUTH=ed25519 needs a registered machine: %w", err)
		}
		c.Machine = m
	case AuthHS256:
	default:
		return c, fmt.Errorf("AGENT_AUTH is %q, expected auto, ed25519 or hs256", c.Auth)
	}
	// The observe link: registered into its own folder (AGENT_MACHINE_FILE=<that file> harnessly-agent register …), so
	// the cluster's agent never finds a machine.json of its own and stays what it is. No file yet is not an error: the
	// setting may come before the registration.
	c.ObserveMachineFile = env("AGENT_HUB_OBSERVE_FILE", "")
	if c.ObserveMachineFile != "" && c.Machine == nil {
		m, err := LoadMachine(c.ObserveMachineFile)
		switch {
		case err == nil:
			c.ObserveMachine = m
		case !errors.Is(err, os.ErrNotExist):
			return c, fmt.Errorf("AGENT_HUB_OBSERVE_FILE: %w", err)
		}
	}
	// A hub's runtime answers its API under its base path, so the routes the agent calls itself start with it.
	base := ""
	if c.Machine != nil {
		base = c.Machine.RuntimeBasePath
	}
	c.RuntimeAPI = strings.TrimRight(env("AGENT_RUNTIME_API", base+"/api/agent"), "/")
	c.StateChangedPath = env("AGENT_RUNTIME_STATE_CHANGED", c.RuntimeAPI+"/state-changed")
	c.StatusPath = env("AGENT_RUNTIME_STATUS", base+"/api/status")
	c.SyncPath = env("AGENT_RUNTIME_SYNC", base+"/api/sync")
	c.PolicyFile = env("AGENT_POLICY_FILE", "")
	c.EnvDir = env("AGENT_ENV_DIR", filepath.Join(c.DataDir, agents.EnvDirName))
	c.WorkDir = env("AGENT_WORK_DIR", env("RUNTIME_WORKSPACE_DIR", home))
	c.V2Path = strings.TrimRight(env("AGENT_RUNTIME_V2", base+"/api/v2"), "/")
	c.PeersPath = strings.TrimRight(env("AGENT_RUNTIME_PEERS", base+"/api/peers"), "/")
	c.RuntimeCookie = env("AGENT_RUNTIME_COOKIE", "session-token")
	for _, h := range strings.Split(env("AGENT_RUNTIME_CLI_HEADERS", "x-cli-token"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.CLITokenHeaders = append(c.CLITokenHeaders, h)
		}
	}
	c.LocalTokenHeader = env("AGENT_LOCAL_TOKEN_HEADER", "x-agent-local-token")
	c.ReportPortEnv = env("AGENT_REPORT_PORT_ENV", "AGENT_REPORT_PORT")
	for _, f := range strings.Split(env("AGENT_EXTRA_STATE_FILES", ""), ",") {
		if f = strings.TrimSpace(f); f != "" && !strings.ContainsAny(f, "/\\") {
			c.ExtraStateFiles = append(c.ExtraStateFiles, f)
		}
	}
	return c, nil
}

// LoadSecret is the pair's session secret: an explicit file wins, else the runtime config's authSecret.
func (c Config) LoadSecret() ([]byte, error) {
	if c.SecretFile != "" {
		raw, err := os.ReadFile(c.SecretFile)
		if err != nil {
			return nil, err
		}
		return []byte(strings.TrimSpace(string(raw))), nil
	}
	raw, err := os.ReadFile(c.ConfigFile)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		AuthSecret any `json:"authSecret"`
	}
	if err := jsonUnmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	s, ok := cfg.AuthSecret.(string)
	if !ok || len([]rune(s)) < 32 {
		return nil, fmt.Errorf("runtime config has no usable authSecret")
	}
	return []byte(s), nil
}
