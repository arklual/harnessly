package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/coder/websocket"
)

// DeviceEnv names the device a chat's commands run on; without it the wrapper is plain bash.
const DeviceEnv = "HARNESSLY_CONNECT_DEVICE"

// The agent's own files that its shell commands name, kept on the runtime's machine: the shell snapshot Claude Code
// builds once and sources before every command, and the file each command's `pwd -P` goes to. They are carried to
// the device and back; the device keeps them in its own folder.
var mirrored = regexp.MustCompile(`/[^\s'"]*/shell-snapshots/snapshot-[A-Za-z0-9._-]+\.sh|/[^\s'"]*/claude-[0-9a-f]+-cwd`)

// forwarded says which of the agent's environment goes with a command: the agent's own settings for its commands,
// never the runtime machine's paths, home or secrets. The rest is the device's login environment.
func forwarded(k string) bool {
	if k == DeviceEnv || k == "HARNESSLY_DATA_DIR" || k == "HARNESSLY_CLI_TOKEN" || k == "HARNESSLY_CLI_TOKEN_FILE" {
		return false
	}
	for _, p := range []string{"CLAUDECODE", "CLAUDE_CODE_", "__CLAUDE", "HARNESSLY_", "GIT_EDITOR", "GIT_PAGER", "PAGER",
		"NO_COLOR", "FORCE_COLOR", "TERM", "COLORTERM"} {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// BashEnv is what RunBash reads from its world.
type BashEnv struct {
	Getenv  func(string) string
	Environ []string
	Cwd     string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	// Endpoint gives the runtime's base URL, its cli-token and the header it goes in.
	Endpoint func() (base, token, header string, err error)
	// Fallback runs a plain local bash when no device is chosen.
	Fallback func(args []string) int
}

// RunBash is the agent's shell (CLAUDE_CODE_SHELL): the command runs on the chat's device, its output and exit code
// come back as if it ran here.
func RunBash(args []string, env BashEnv) int {
	device := strings.TrimSpace(env.Getenv(DeviceEnv))
	if device == "" {
		return env.Fallback(args)
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(env.Stderr, "harnessly connect: "+format+"\n", a...)
		return 1
	}
	base, token, header, err := env.Endpoint()
	if err != nil {
		return fail("%v", err)
	}

	// The mirrored files: each path becomes the device's own (TmpToken/<name>), what exists here goes along, and
	// whatever the device has under that name after the command comes back.
	local := map[string]string{} // device path → path here
	var in []File
	var out []string
	rewritten := make([]string, len(args))
	for i, a := range args {
		rewritten[i] = mirrored.ReplaceAllStringFunc(a, func(p string) string {
			dp := TmpToken + "/" + filepath.Base(p)
			if _, seen := local[dp]; !seen {
				local[dp] = p
				out = append(out, dp)
				if raw, err := os.ReadFile(p); err == nil && len(raw) <= maxFileBytes {
					in = append(in, File{Path: dp, Data: raw})
				}
			}
			return dp
		})
	}
	vars := map[string]string{}
	for _, kv := range env.Environ {
		if k, v, ok := strings.Cut(kv, "="); ok && forwarded(k) {
			vars[k] = v
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws, _, err := websocket.Dial(ctx, strings.Replace(base, "http", "ws", 1)+"/api/cli/connect/exec", &websocket.DialOptions{
		HTTPHeader:      http.Header{header: {token}},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return fail("нет связи с runtime: %v", err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(64 << 20)
	send := func(m Msg) error {
		b, _ := json.Marshal(m)
		return ws.Write(ctx, websocket.MessageText, b)
	}
	if err := send(Msg{T: "exec", Device: device, Args: rewritten, Cwd: env.Cwd, Env: vars, FilesIn: in, FilesOut: out}); err != nil {
		return fail("нет связи с runtime: %v", err)
	}

	// Signals the agent sends its shell (a timeout, an interrupt) go to the command on the device.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	go func() {
		names := map[os.Signal]string{syscall.SIGINT: "INT", syscall.SIGTERM: "TERM", syscall.SIGHUP: "HUP"}
		for sg := range sigs {
			_ = send(Msg{T: "signal", Sig: names[sg]})
		}
	}()
	if env.Stdin != nil {
		go func() {
			buf := make([]byte, 32<<10)
			for {
				n, err := env.Stdin.Read(buf)
				if n > 0 {
					if send(Msg{T: "stdin", Data: append([]byte(nil), buf[:n]...)}) != nil {
						return
					}
				}
				if err != nil {
					_ = send(Msg{T: "stdin", EOF: true})
					return
				}
			}
		}()
	}

	for {
		typ, b, err := ws.Read(ctx)
		if err != nil {
			return fail("связь с runtime оборвалась: %v", err)
		}
		if typ == websocket.MessageBinary {
			if len(b) == 0 {
				continue
			}
			w := env.Stdout
			if b[0] == KindStderr {
				w = env.Stderr
			}
			_, _ = w.Write(b[1:])
			continue
		}
		var m Msg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		switch m.T {
		case "error":
			return fail("%s", m.Error)
		case "exit":
			for _, f := range m.Files {
				if p, ok := local[f.Path]; ok {
					if old, err := os.ReadFile(p); err != nil || string(old) != string(f.Data) {
						_ = os.MkdirAll(filepath.Dir(p), 0o700)
						_ = os.WriteFile(p, f.Data, 0o600)
					}
				}
			}
			ws.Close(websocket.StatusNormalClosure, "")
			return m.Code
		}
	}
}
