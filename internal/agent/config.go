package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	RuntimeCookie    string
	// CLITokenHeaders carry the runtime's cli-token when the agent tells it a state file changed.
	CLITokenHeaders []string

	// LocalTokenHeader carries the token from agent.json on the loopback routes the CLIs call.
	LocalTokenHeader string
	// ReportPortEnv is the variable a launched automation conversation finds this agent's port in.
	ReportPortEnv string
	// ExtraStateFiles are files of the data directory's root the platform also keeps a copy of.
	ExtraStateFiles []string
}

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
	c.ConfigFile = env("AGENT_CONFIG", filepath.Join(c.DataDir, "config.json"))
	c.SecretFile = env("AGENT_SECRET_FILE", "")
	c.ClusterURL = env("AGENT_CLUSTER_URL", "")
	c.RuntimeID = env("AGENT_RUNTIME_ID", "")
	c.ListenAudience = env("AGENT_AUDIENCE", "agent")
	c.DialAudience = env("AGENT_DIAL_AUDIENCE", "ingest")
	c.RuntimeAPI = strings.TrimRight(env("AGENT_RUNTIME_API", "/api/agent"), "/")
	c.StateChangedPath = env("AGENT_RUNTIME_STATE_CHANGED", c.RuntimeAPI+"/state-changed")
	c.StatusPath = env("AGENT_RUNTIME_STATUS", "/api/status")
	c.SyncPath = env("AGENT_RUNTIME_SYNC", "/api/sync")
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
