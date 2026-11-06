package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The app's agent.validators: checks of the agent's turn. A validator is a script of the package that runs on
// PostToolUse (after a tool call; match narrows it to some tools) or on Stop (before the agent ends its turn). The
// script gets the hook's JSON on stdin and answers by its exit code: 0 is fine, 2 is a failure and its stdout (up to
// ValidatorReasonBytes) is the remark the agent reads. The hook (harnessly hook app-validate) answers
// {"decision":"block","reason":…}, so Claude Code shows the remark to the agent, which fixes the work. Failures in a row
// are counted per chat and validator; past maxRetries the hook lets go (a Stop ends, a PostToolUse stops blocking) so
// the agent does not spin. A pass resets the count. Any other exit code, a timeout or a script that cannot start
// blocks nothing: the validator is broken, not the work.

const (
	ValidatorPostToolUse = "PostToolUse"
	ValidatorStop        = "Stop"
	// ValidatorDefaultRetries is how many times a validator may block in a row when it does not say; ValidatorMaxRetries
	// is the most it may ask for.
	ValidatorDefaultRetries = 3
	ValidatorMaxRetries     = 10
	// ValidatorTimeoutSeconds is how long a script may run.
	ValidatorTimeoutSeconds = 30
	// ValidatorReasonBytes bounds the remark the agent gets.
	ValidatorReasonBytes = 4 << 10
	// ValidatorHookTimeoutSeconds is how long Claude waits for the hook: a script's time plus the hook's own.
	ValidatorHookTimeoutSeconds = ValidatorTimeoutSeconds + 10
)

// ValidatorsOf are the app's validators with the default retries filled in.
func ValidatorsOf(m *Manifest) []Validator {
	if m == nil || m.Agent == nil {
		return nil
	}
	out := make([]Validator, 0, len(m.Agent.Validators))
	for _, v := range m.Agent.Validators {
		if v.MaxRetries == 0 {
			v.MaxRetries = ValidatorDefaultRetries
		}
		out = append(out, v)
	}
	return out
}

// HasValidators says whether any app of the snapshot has a validator.
func (s Snapshot) HasValidators() bool {
	for _, a := range s.Apps {
		if len(a.Validators) > 0 {
			return true
		}
	}
	return false
}

// Applies says whether a validator runs for the event; tool is the call's tool name and input its arguments (PostToolUse).
func (v Validator) Applies(event, appID, tool string, input map[string]any) bool {
	if v.On != event {
		return false
	}
	if event != ValidatorPostToolUse || v.Match == "" {
		return true
	}
	score, _ := match(v.Match, DecideAllow, PluginName(appID), Call{Tool: tool, Input: input})
	return score >= 0
}

// ValidatorResult is how one run of a script ended.
type ValidatorResult struct {
	// Failed is true on exit code 2; Remark is then what the script said, for the agent.
	Failed bool
	Remark string
	// Err is a script that did not work (cannot start, timeout, another exit code): nothing is blocked.
	Err error
}

// RunValidator runs the script of v from the app's folder with the hook's input on stdin.
func RunValidator(ctx context.Context, appDir string, v Validator, input []byte) ValidatorResult {
	pkg := &Package{Dir: appDir}
	full, err := pkg.inside(v.Run)
	if err != nil {
		return ValidatorResult{Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, ValidatorTimeoutSeconds*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if strings.HasSuffix(strings.ToLower(full), ".py") {
		cmd = exec.CommandContext(ctx, "python3", "-I", full)
	} else {
		cmd = exec.CommandContext(ctx, full)
	}
	cmd.Dir = appDir
	cmd.Env = append(ValidatorEnv(os.Environ()), "HARNESSLY_APP_DIR="+appDir, "HARNESSLY_VALIDATOR="+v.ID)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, max: ValidatorReasonBytes}
	cmd.Stderr = &limitWriter{w: &stderr, max: 1 << 10}
	err = cmd.Run()
	if err == nil {
		return ValidatorResult{}
	}
	if ctx.Err() != nil {
		return ValidatorResult{Err: fmt.Errorf("%s did not finish in %d s", filepath.Base(full), ValidatorTimeoutSeconds)}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 2 {
		remark := strings.ToValidUTF8(strings.TrimSpace(stdout.String()), "")
		if remark == "" {
			remark = fmt.Sprintf("The check %s of the app did not pass.", v.ID)
		}
		return ValidatorResult{Failed: true, Remark: remark}
	}
	return ValidatorResult{Err: fmt.Errorf("%s: %v %s", filepath.Base(full), err, strings.TrimSpace(stderr.String()))}
}

// ValidatorEnv is the part of env a validator gets: PATH, HOME, LANG, LC_*, TMPDIR and the variables of harnessly the CLI
// needs. Nothing else (tokens and keys of the person's shell stay out).
func ValidatorEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case name == "PATH", name == "HOME", name == "LANG", name == "TMPDIR", strings.HasPrefix(name, "LC_"), strings.HasPrefix(name, "HARNESSLY_"):
			out = append(out, kv)
		}
	}
	return out
}

// limitWriter keeps the first max bytes and drops the rest without failing the writer.
type limitWriter struct {
	w   *bytes.Buffer
	max int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if room := l.max - l.w.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		l.w.Write(p[:room])
	}
	return len(p), nil
}

// ValidatorBlockAnswer is the hook's answer that shows the agent a remark.
func ValidatorBlockAnswer(reason string) []byte {
	out, _ := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	return append(out, '\n')
}
