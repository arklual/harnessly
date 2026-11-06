// Package execkv keeps the safe in any store a command can reach, without a new build of harnessly: the runtime runs
// the command once per call, writes one request as JSON to its stdin and reads one answer from its stdout
// (docs/safe.md, "Any other store"). The protocol, harnessly.secrets-provider/v1:
//
//	→ {"protocol":"harnessly.secrets-provider/v1","op":"read","path":"harnessly/gh-token"}
//	← {"data":{"value":"…"}}
//	→ {"protocol":…,"op":"write","path":"harnessly/gh-token","data":{"value":"…"}}
//	← {}
//	→ {"protocol":…,"op":"delete","path":"harnessly/gh-token"}
//	← {}
//	→ {"protocol":…,"op":"list","path":"harnessly"}
//	← {"keys":["gh-token","team/"]}        a folder ends in "/"
//	← {"error":{"code":"not_found","message":"…"}}   not_found, denied, unavailable, too_large or failed
//
// A path is a folder of the store and a name, as on a Vault-compatible server; what the store makes of it is the
// command's business. The command's stderr is never shown: it may hold what the store said about a value.
package execkv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// Protocol is the name and version of the protocol.
const Protocol = "harnessly.secrets-provider/v1"

// The operations.
const (
	OpRead   = "read"
	OpWrite  = "write"
	OpDelete = "delete"
	OpList   = "list"
)

// maxAnswer bounds what a command may print: the largest value with room for its JSON, or a long list.
const maxAnswer = 1 << 20

// Request is what the command reads from stdin.
type Request struct {
	Protocol string            `json:"protocol"`
	Op       string            `json:"op"`
	Path     string            `json:"path"`
	Data     map[string]string `json:"data,omitempty"`
}

// Answer is what the command prints on stdout.
type Answer struct {
	Data  map[string]string `json:"data,omitempty"`
	Keys  []string          `json:"keys,omitempty"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

// KV is a secrets.KV over a command.
type KV struct {
	// Argv is the command and its arguments; it is run as is, without a shell.
	Argv []string
	// Env is added to the runtime's environment for the command (nil: the runtime's).
	Env     []string
	Timeout time.Duration
}

var _ secrets.KV = (*KV)(nil)

// New is a KV over the command line cmd, split at spaces (no shell, no quotes).
func New(cmd string) (*KV, error) {
	argv := strings.Fields(cmd)
	if len(argv) == 0 {
		return nil, errors.New("no command")
	}
	if !filepath.IsAbs(argv[0]) {
		if _, err := exec.LookPath(argv[0]); err != nil {
			return nil, fmt.Errorf("the command %s is not found", argv[0])
		}
	}
	return &KV{Argv: argv, Timeout: 20 * time.Second}, nil
}

// Name is the command's name, for the page: never its arguments, which may name more than the store.
func (k *KV) Name() string { return filepath.Base(k.Argv[0]) }

var reasons = map[string]string{
	"not_found":   secrets.ReasonNotFound,
	"denied":      secrets.ReasonDenied,
	"unavailable": secrets.ReasonUnavailable,
	"too_large":   secrets.ReasonTooLarge,
	"failed":      secrets.ReasonFailed,
}

func (k *KV) call(ctx context.Context, req Request) (Answer, error) {
	req.Protocol = Protocol
	in, err := json.Marshal(req)
	if err != nil {
		return Answer{}, err
	}
	timeout := k.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, k.Argv[0], k.Argv[1:]...)
	if k.Env != nil {
		cmd.Env = append(cmd.Environ(), k.Env...)
	}
	cmd.Stdin = bytes.NewReader(in)
	var out limited
	cmd.Stdout = &out
	runErr := cmd.Run()
	name := k.Name()
	if ctx.Err() != nil {
		return Answer{}, &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: fmt.Errorf("%s did not answer in %s", name, timeout)}
	}
	var ans Answer
	if err := json.Unmarshal(out.Bytes(), &ans); err != nil {
		if runErr != nil {
			return Answer{}, &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: fmt.Errorf("%s: %v", name, exitOf(runErr))}
		}
		return Answer{}, &secrets.Refusal{Reason: secrets.ReasonFailed, Err: fmt.Errorf("%s did not answer in %s", name, Protocol)}
	}
	if ans.Error != nil {
		reason, ok := reasons[ans.Error.Code]
		if !ok {
			reason = secrets.ReasonFailed
		}
		// The message is the command's; it is shown, so a provider must not put a value in it (docs/safe.md).
		msg := ans.Error.Message
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return Answer{}, &secrets.Refusal{Reason: reason, Err: fmt.Errorf("%s: %s", name, strings.TrimSpace(ans.Error.Code+" "+msg))}
	}
	if runErr != nil {
		return Answer{}, &secrets.Refusal{Reason: secrets.ReasonFailed, Err: fmt.Errorf("%s: %v", name, exitOf(runErr))}
	}
	return ans, nil
}

// exitOf is how the command ended, without what it printed.
func exitOf(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Errorf("exited with %d", ee.ExitCode())
	}
	return errors.New("could not be run")
}

func (k *KV) Read(ctx context.Context, path string) (map[string]string, error) {
	ans, err := k.call(ctx, Request{Op: OpRead, Path: path})
	if err == nil && ans.Data == nil {
		return nil, &secrets.Refusal{Reason: secrets.ReasonNotFound, Err: errors.New("no data")}
	}
	return ans.Data, err
}

func (k *KV) Write(ctx context.Context, path string, data map[string]string) error {
	_, err := k.call(ctx, Request{Op: OpWrite, Path: path, Data: data})
	return err
}

func (k *KV) Delete(ctx context.Context, path string) error {
	_, err := k.call(ctx, Request{Op: OpDelete, Path: path})
	return err
}

func (k *KV) List(ctx context.Context, path string) ([]string, error) {
	ans, err := k.call(ctx, Request{Op: OpList, Path: path})
	if err == nil && ans.Keys == nil {
		ans.Keys = []string{}
	}
	return ans.Keys, err
}

// limited is a buffer that keeps the first maxAnswer bytes; more makes the answer unreadable, so it is refused.
type limited struct{ bytes.Buffer }

func (l *limited) Write(p []byte) (int, error) {
	if l.Len()+len(p) > maxAnswer {
		return 0, errors.New("the answer is too long")
	}
	return l.Buffer.Write(p)
}
