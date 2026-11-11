package envsetup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Secrets is the person's safe as setup sees it. The values never leave a step's environment (and the masked log).
type Secrets interface {
	// Has says whether the safe holds a secret of this name.
	Has(name string) bool
	// Reveal gives the values of names; an error if any is missing.
	Reveal(names []string) (map[string]string, error)
	// Ask opens the safe's ask on the person's page: «положите <name> в сейф», with the reason. One open ask per name.
	Ask(name, why string) error
}

// Cmd is one program to run.
type Cmd struct {
	Name string
	Args []string
	Dir  string
	// Env is the whole environment of the program; nothing is inherited.
	Env   []string
	Stdin string
	// Out gets stdout and stderr, interleaved as written.
	Out     io.Writer
	Timeout time.Duration
	// Bind is a folder of this machine a sandboxed program must still see and write (its working folder under /tmp).
	Bind string
}

// ErrTimeout: the program did not end in time; it was asked to stop, then killed.
var ErrTimeout = errors.New("timeout")

// ExecFunc runs c and answers its exit code. An error is a program that did not run to its end: not found (exec.ErrNotFound),
// ErrTimeout, or the context's.
type ExecFunc func(ctx context.Context, c Cmd) (exit int, err error)

// Runner does the setup for one link folder.
type Runner struct {
	// LinkDir is the folder of a hub link (the agent's data folder, or <data>/hub for an observe link).
	LinkDir string
	// Home is the person's home: where repositories go and what scripts get as HOME.
	Home    string
	Secrets Secrets
	// Apps installs the environment's apps (items of kind app) in the runtime's app registry; nil: this runtime does
	// not, and such a step fails saying so.
	Apps Apps

	// The rest is for tests; the zero value is the real thing.
	Exec   ExecFunc
	Fetch  func(ctx context.Context, url string) ([]byte, error)
	Now    func() time.Time
	Getenv func(string) string
	// Version is the runtime's, for the heartbeat.
	Version string
	// Free says how many bytes are free on the disk of path; nil asks the system.
	Free func(path string) (uint64, error)
	// Poll is how often the link folder is looked at (3 s).
	Poll time.Duration
	// Log is the runtime's log: level, message, key/value pairs.
	Log func(level, msg string, kv ...string)
	// OnUpdate is told when the hub asks for an update run («Применить на машинах»): the runtime asks the base package
	// for a look of its own then. nil: nothing.
	OnUpdate func()

	// secretsWaited: the secrets a finished run waits for, and the list's hash it was for.
	waited     []string
	waitedHash string
	lastPrune  time.Time
	// beatMu keeps the heartbeat's own goroutine (Run) and Tick from writing it twice at once.
	beatMu   sync.Mutex
	lastBeat time.Time
}

const (
	scriptTimeout = 15 * time.Minute
	checkTimeout  = 2 * time.Minute
	gitTimeout    = 15 * time.Minute
	loginTimeout  = time.Minute
	fetchTimeout  = 30 * time.Second
	fetchMax      = 1 << 20
	defaultPoll   = 3 * time.Second
	pruneEvery    = time.Hour
	// retryEvery: a machine whose last apply left something undone is tried again this often, and only such a machine.
	retryEvery = 30 * time.Minute
	// requestTTL: a request nobody took up in this time is answered with an error rather than run late.
	requestTTL = 10 * time.Minute
	// minFree is what a clone or a sandbox run needs on the disk.
	minFree = 2 << 30
)

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) getenv(k string) string {
	if r.Getenv != nil {
		return r.Getenv(k)
	}
	return os.Getenv(k)
}

func (r *Runner) logf(level, msg string, kv ...string) {
	if r.Log != nil {
		r.Log(level, msg, kv...)
	}
}

func (r *Runner) exec(ctx context.Context, c Cmd) (int, error) {
	if r.Exec != nil {
		return r.Exec(ctx, c)
	}
	return DefaultExec(ctx, c)
}

// source is what any setup kind's source may hold (docs/hub/environment.md).
// Apps is the runtime's app registry as the setup sees it.
type Apps interface {
	// Install puts the app in dir on the machine as the environment item's, with the installation's values the hub
	// gave (${install:<name>}); changed is false when the same version from the same files is already there. missing
	// are the values the app asks for and the hub has none of: the app is there but not configured.
	Install(dir, itemID string, locked bool, install map[string]string) (id string, changed bool, missing []string, err error)
	// Has: the item's app is installed.
	Has(itemID string) bool
	// Prune takes off the environment's apps whose item is not among keep.
	Prune(keep []string) ([]string, error)
}

type source struct {
	// repo
	URL    string `json:"url"`
	Branch string `json:"branch"`
	Dir    string `json:"dir"`
	// access
	Service string `json:"service"`
	Host    string `json:"host"`
	Secret  string `json:"secret"`
	// script
	From     string   `json:"from"`
	Body     string   `json:"body"`
	FileName string   `json:"fileName"`
	Repo     string   `json:"repo"`
	RepoURL  string   `json:"repoUrl"`
	Path     string   `json:"path"`
	Run      string   `json:"run"`
	Rerun    string   `json:"rerun"`
	Check    string   `json:"check"`
	Secrets  []string `json:"secrets"`
	// app: the installation's values for ${install:<name>} of the package, which the hub adds
	Install map[string]string `json:"install"`
	// plugin
	Marketplace string `json:"marketplace"`
	Plugin      string `json:"plugin"`
	// claude-settings
	Always   map[string]json.RawMessage `json:"always"`
	IfAbsent map[string]json.RawMessage `json:"ifAbsent"`
	ClaudeMd string                     `json:"claudeMd"`
}

// shown maps a step's status to the one an item reports to the hub (ok, error, pending).
func shown(status string) string {
	switch status {
	case hubproto.EnvSkipped:
		return hubproto.EnvOK
	case hubproto.EnvRunning:
		return hubproto.EnvPending
	}
	return status
}
