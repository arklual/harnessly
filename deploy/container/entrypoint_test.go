//go:build unix

package container

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// rig is a home folder with a fake agent and a fake runtime that record how they were started.
type rig struct {
	t    *testing.T
	home string
	bin  string
}

const fakeAgent = `#!/bin/sh
here=$(dirname "$0")
if [ "$1" = register ]; then
	echo "$*" >> "$here/register.log"
	[ -f "$here/register.fail" ] && [ "$(wc -l < "$here/register.log")" -le "$(cat "$here/register.fail")" ] && exit 1
	while [ $# -gt 0 ]; do
		[ "$1" = --code-file ] && cat "$2" >> "$here/code.log"
		shift
	done
	mkdir -p "$AGENT_DATA_DIR"
	printf '{\n  "machineId": "abcdefghijklmnopqrst",\n  "hubUrl": "https://hub.example.com",\n  "runtimeBasePath": "/m/abcdefghijklmnopqrst"\n}\n' > "$AGENT_DATA_DIR/machine.json"
	exit 0
fi
echo "$$" > "$here/agent.pid"
echo "AGENT_PORT=$AGENT_PORT RUNTIME_PORT=$RUNTIME_PORT DATA=$AGENT_DATA_DIR" > "$here/agent.env.out"
exec sleep 300
`

const fakeRuntime = `#!/bin/sh
here=$(dirname "$0")
echo "$$" > "$here/runtime.pid"
{
	echo "BASE=$RUNTIME_APP_BASE_PATH"
	echo "HUB=$RUNTIME_HUB_URL"
	echo "LISTEN=$RUNTIME_GATE_LISTEN UPSTREAM=[$RUNTIME_GATE_UPSTREAM] PREVIEW=$RUNTIME_PREVIEW_LISTEN"
	echo "WORK=$RUNTIME_WORKSPACE_DIR"
} > "$here/runtime.env.out"
if [ -f "$here/runtime.exit" ]; then
	sleep 1
	exit "$(cat "$here/runtime.exit")"
fi
exec sleep 300
`

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, home: t.TempDir(), bin: t.TempDir()}
	for name, body := range map[string]string{"agent": fakeAgent, "runtime": fakeRuntime} {
		if err := os.WriteFile(filepath.Join(r.bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *rig) file(name string) string {
	raw, _ := os.ReadFile(filepath.Join(r.bin, name))
	return string(raw)
}

func (r *rig) write(name, body string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.bin, name), []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) start(extra ...string) (*exec.Cmd, *bytes.Buffer) {
	r.t.Helper()
	cmd := exec.Command("/bin/sh", "entrypoint.sh")
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + r.home,
		"HARNESSLY_AGENT_BIN=" + filepath.Join(r.bin, "agent"), "HARNESSLY_RUNTIME_BIN=" + filepath.Join(r.bin, "runtime")}, extra...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
		}
	})
	return cmd, &out
}

// wait returns the exit status of the entrypoint, or fails after the timeout.
func (r *rig) wait(cmd *exec.Cmd, within time.Duration) int {
	r.t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		r.t.Fatal(err)
	case <-time.After(within):
		r.t.Fatal("the entrypoint did not end")
	}
	return -1
}

func (r *rig) waitFor(name string) string {
	r.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if s := r.file(name); s != "" {
			return s
		}
	}
	r.t.Fatalf("%s never appeared", name)
	return ""
}

func (r *rig) alive(pidFile string) bool {
	pid, err := strconv.Atoi(strings.TrimSpace(r.file(pidFile)))
	if err != nil {
		r.t.Fatalf("%s: %v", pidFile, err)
	}
	return syscall.Kill(pid, 0) == nil
}

func (r *rig) registered() {
	r.t.Helper()
	dir := filepath.Join(r.home, ".harnessly")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	body := `{"machineId":"abcdefghijklmnopqrst","hubUrl":"https://hub.example.com","runtimeBasePath":"/m/abcdefghijklmnopqrst"}`
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func TestEntrypointRunsBothAndEndsWithTheOneThatEnded(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.registered()
	r.write("runtime.exit", "7")
	cmd, out := r.start()
	if code := r.wait(cmd, 20*time.Second); code != 7 {
		t.Fatalf("status %d\n%s", code, out)
	}
	if !strings.Contains(out.String(), "the runtime ended (status 7)") {
		t.Errorf("%s", out)
	}
	if r.alive("agent.pid") {
		t.Error("the agent was left running")
	}
	rt := r.file("runtime.env.out")
	for _, want := range []string{
		"BASE=/m/abcdefghijklmnopqrst", "HUB=https://hub.example.com", "LISTEN=127.0.0.1:18022 UPSTREAM=[] PREVIEW=off",
		"WORK=" + r.home + "/work",
	} {
		if !strings.Contains(rt, want) {
			t.Errorf("runtime env lacks %q:\n%s", want, rt)
		}
	}
	if got := r.file("agent.env.out"); !strings.Contains(got, "AGENT_PORT=18023 RUNTIME_PORT=18022 DATA="+r.home+"/.harnessly") {
		t.Errorf("agent env: %s", got)
	}
	if _, err := os.Stat(filepath.Join(r.home, "work")); err != nil {
		t.Errorf("no work folder: %v", err)
	}
	if r.file("register.log") != "" {
		t.Errorf("a registered machine registered again: %s", r.file("register.log"))
	}
}

func TestEntrypointRegistersFromACodeFileThatAppearsLate(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.write("runtime.exit", "0")
	code := filepath.Join(t.TempDir(), "code")
	cmd, out := r.start("AGENT_HUB_URL=https://hub.example.com", "AGENT_REGISTER_CODE_FILE="+code, "AGENT_REGISTER_NAME=Ada box",
		"MACHINE_RUNTIME_PORT=18123", "MACHINE_AGENT_PORT=18124")
	time.Sleep(400 * time.Millisecond)
	if err := os.WriteFile(code, []byte("hx_localcode\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := r.wait(cmd, 30*time.Second); got != 0 {
		t.Fatalf("status %d\n%s", got, out)
	}
	if !strings.Contains(out.String(), "waiting for the registration code file") {
		t.Errorf("%s", out)
	}
	log := r.file("register.log")
	if want := "register --hub https://hub.example.com --profile container --via container --name Ada box --code-file " + code; strings.TrimSpace(log) != want {
		t.Errorf("register was called as %q, want %q", strings.TrimSpace(log), want)
	}
	if strings.TrimSpace(r.file("code.log")) != "hx_localcode" {
		t.Errorf("the code the agent read: %q", r.file("code.log"))
	}
	if strings.Contains(out.String(), "hx_localcode") {
		t.Errorf("the code is in the output: %s", out)
	}
	if rt := r.file("runtime.env.out"); !strings.Contains(rt, "BASE=/m/abcdefghijklmnopqrst") || !strings.Contains(rt, "LISTEN=127.0.0.1:18123") {
		t.Errorf("%s", rt)
	}
	if !strings.Contains(r.file("agent.env.out"), "AGENT_PORT=18124 RUNTIME_PORT=18123") {
		t.Errorf("%s", r.file("agent.env.out"))
	}
}

func TestEntrypointRegistersFromACodeAndRetries(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.write("runtime.exit", "0")
	r.write("register.fail", "1") // the first try fails, the hub is not up yet
	cmd, out := r.start("AGENT_HUB_URL=https://hub.example.com", "AGENT_REGISTER_CODE=hx_abc", "AGENT_REGISTER_PROFILE=dir", "AGENT_REGISTER_VIA=command")
	if got := r.wait(cmd, 30*time.Second); got != 0 {
		t.Fatalf("status %d\n%s", got, out)
	}
	lines := strings.Split(strings.TrimSpace(r.file("register.log")), "\n")
	if len(lines) != 2 || lines[1] != "register --hub https://hub.example.com --profile dir --via command --token hx_abc" {
		t.Errorf("%q", lines)
	}
}

func TestEntrypointGivesUpWhenItCannotRegister(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.write("register.fail", "1000")
	cmd, out := r.start("AGENT_HUB_URL=https://hub.example.com", "AGENT_REGISTER_CODE=hx_abc", "AGENT_REGISTER_WAIT=1")
	if got := r.wait(cmd, 30*time.Second); got != 1 || !strings.Contains(out.String(), "could not register with https://hub.example.com") {
		t.Fatalf("status %d\n%s", got, out)
	}
	if r.file("runtime.pid") != "" {
		t.Error("the runtime started without a registration")
	}
}

func TestEntrypointRefusesToRunUnregisteredWithoutACode(t *testing.T) {
	t.Parallel()
	for name, env := range map[string][]string{
		"no hub":   {"AGENT_REGISTER_CODE=hx_abc"},
		"no code":  {"AGENT_HUB_URL=https://hub.example.com"},
		"bad wait": {"AGENT_HUB_URL=https://hub.example.com", "AGENT_REGISTER_CODE=hx_abc", "AGENT_REGISTER_WAIT=soon"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			cmd, out := r.start(env...)
			if got := r.wait(cmd, 10*time.Second); got != 1 || !strings.Contains(out.String(), "harnessly-machine:") {
				t.Fatalf("status %d\n%s", got, out)
			}
			if r.file("runtime.pid") != "" || r.file("register.log") != "" {
				t.Error("something ran")
			}
		})
	}
}

func TestEntrypointInTheClosedClusterWayDoesNotRegister(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.write("runtime.exit", "0")
	cmd, out := r.start("AGENT_AUTH=hs256")
	if got := r.wait(cmd, 20*time.Second); got != 0 {
		t.Fatalf("status %d\n%s", got, out)
	}
	if r.file("register.log") != "" {
		t.Error("registered")
	}
	if rt := r.file("runtime.env.out"); !strings.Contains(rt, "BASE=\n") || !strings.Contains(rt, "HUB=\n") {
		t.Errorf("a base path or hub URL without a hub:\n%s", rt)
	}
}

func TestEntrypointStopsBothOnTerm(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.registered()
	cmd, out := r.start()
	r.waitFor("runtime.env.out")
	r.waitFor("agent.env.out")
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if got := r.wait(cmd, 20*time.Second); got != 143 {
		t.Fatalf("status %d\n%s", got, out)
	}
	if r.alive("runtime.pid") || r.alive("agent.pid") {
		t.Error("a child outlived the entrypoint")
	}
}

func TestEntrypointOddBasePathIsRefused(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	dir := filepath.Join(r.home, ".harnessly")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "machine.json"), []byte(`{"hubUrl":"https://h","runtimeBasePath":"/m/x y;rm"}`), 0o600)
	cmd, out := r.start()
	if got := r.wait(cmd, 10*time.Second); got != 1 || !strings.Contains(out.String(), "odd runtimeBasePath") {
		t.Fatalf("status %d\n%s", got, out)
	}
}
