package connect

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// The harnessly shim: the agent's commands on the device call `harnessly …` (open, deliver, tab create, secret run),
// which is the Connect binary itself under that name. It reaches the device's Connect on a loopback port, which asks
// the runtime on its link; the runtime runs the command for the chat whose command is running.
const (
	shimURLEnv  = "HARNESSLY_CONNECT_SHIM"
	shimKeyEnv  = "HARNESSLY_CONNECT_KEY"
	shimExecEnv = "HARNESSLY_CONNECT_EXEC"
)

type shimReq struct {
	Kind  string   `json:"kind"` // cli or secrets
	Exec  string   `json:"exec"`
	Args  []string `json:"args"`
	Stdin []byte   `json:"stdin,omitempty"`
}

type shimWait struct {
	next atomic.Uint64
	mu   sync.Mutex
	wait map[uint64]chan Msg
}

// startShim serves the shim on a loopback port and puts `harnessly` in a folder of its own for the commands' PATH.
func (d *Device) startShim() {
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	d.shimKey = hex.EncodeToString(key)
	d.shimWait = &shimWait{wait: map[uint64]chan Msg{}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		d.logf("shim: %v", err)
		return
	}
	d.shimURL = "http://" + ln.Addr().String()
	go func() { _ = http.Serve(ln, http.HandlerFunc(d.serveShim)) }()

	self, err := os.Executable()
	if err != nil {
		return
	}
	bin := filepath.Join(d.tmp, "bin")
	_ = os.MkdirAll(bin, 0o700)
	name := "harnessly"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(bin, name)
	_ = os.Remove(dst)
	if os.Symlink(self, dst) != nil {
		if raw, err := os.ReadFile(self); err == nil {
			_ = os.WriteFile(dst, raw, 0o755)
		}
	}
	d.shimBin = bin
}

// shimEnv is what a command needs to find the shim.
func (d *Device) shimEnv(sid string) map[string]string {
	if d.shimURL == "" {
		return nil
	}
	return map[string]string{shimURLEnv: d.shimURL, shimKeyEnv: d.shimKey, shimExecEnv: sid}
}

// withShimPath puts the shim's folder first on PATH.
func (d *Device) withShimPath(env []string) []string {
	if d.shimBin == "" {
		return env
	}
	for i, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			env[i] = k + "=" + d.shimBin + string(os.PathListSeparator) + v
			return env
		}
	}
	return append(env, "PATH="+d.shimBin)
}

func (d *Device) serveShim(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Connect-Key") != d.shimKey || r.Method != http.MethodPost {
		http.Error(w, "no", http.StatusForbidden)
		return
	}
	var req shimReq
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req) != nil {
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	l := d.link.Load()
	if l == nil {
		writeJSON(w, http.StatusOK, Msg{Code: 1, Stderr: []byte("harnessly: нет связи с runtime\n")})
		return
	}
	id := d.shimWait.next.Add(1)
	ch := make(chan Msg, 1)
	d.shimWait.mu.Lock()
	d.shimWait.wait[id] = ch
	d.shimWait.mu.Unlock()
	defer func() {
		d.shimWait.mu.Lock()
		delete(d.shimWait.wait, id)
		d.shimWait.mu.Unlock()
	}()
	l.sendMsg(Msg{T: req.Kind, SID: strconv.FormatUint(id, 10), Exec: req.Exec, Args: req.Args, Data: req.Stdin})
	select {
	case m := <-ch:
		writeJSON(w, http.StatusOK, m)
	case <-l.done:
		writeJSON(w, http.StatusOK, Msg{Code: 1, Stderr: []byte("harnessly: связь с runtime оборвалась\n")})
	case <-r.Context().Done():
	}
}

// shimAnswer takes the runtime's answer to the shim.
func (d *Device) shimAnswer(m Msg) {
	id, _ := strconv.ParseUint(m.SID, 10, 64)
	if d.shimWait == nil {
		return
	}
	d.shimWait.mu.Lock()
	ch := d.shimWait.wait[id]
	d.shimWait.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

func shimCall(req shimReq) (Msg, error) {
	url, key := os.Getenv(shimURLEnv), os.Getenv(shimKeyEnv)
	if url == "" {
		return Msg{}, errors.New("harnessly здесь работает только в командах агента harnessly (Harnessly Connect)")
	}
	req.Exec = os.Getenv(shimExecEnv)
	body, _ := json.Marshal(req)
	hr, _ := http.NewRequest(http.MethodPost, url+"/shim", bytes.NewReader(body))
	hr.Header.Set("X-Connect-Key", key)
	resp, err := http.DefaultClient.Do(hr)
	if err != nil {
		return Msg{}, fmt.Errorf("Harnessly Connect не отвечает: %w", err)
	}
	defer resp.Body.Close()
	var m Msg
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&m) != nil {
		return Msg{}, fmt.Errorf("Harnessly Connect: %s", resp.Status)
	}
	return m, nil
}

// RunShim is `harnessly …` on the device.
func RunShim(args []string, stdin io.Reader, stdout, stderr io.Writer, stdinIsTerminal bool) int {
	if len(args) >= 2 && args[0] == "secret" && args[1] == "run" {
		return shimSecretRun(args[2:], stdout, stderr)
	}
	req := shimReq{Kind: "cli", Args: args}
	if !stdinIsTerminal && stdin != nil {
		req.Stdin, _ = io.ReadAll(io.LimitReader(stdin, 16<<20))
	}
	m, err := shimCall(req)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, _ = stdout.Write(m.Stdout)
	_, _ = stderr.Write(m.Stderr)
	return m.Code
}

// shimSecretRun runs the command here with the secrets in its environment only, their values masked in its output.
// --env VAR=NAME and --file VAR=NAME as on the server; --code is not on the device.
func shimSecretRun(args []string, stdout, stderr io.Writer) int {
	vars := map[string]string{}  // VAR → secret name
	files := map[string]string{} // VAR → secret name, given as a file path
	var names []string
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		flag, val, hasVal := strings.Cut(a, "=")
		if (a == "--env" || a == "--file" || a == "--why") && i+1 < len(args) {
			flag, val, hasVal = a, args[i+1], true
			i++
		}
		if !hasVal {
			continue
		}
		switch flag {
		case "--env", "--file":
			v, n, ok := strings.Cut(val, "=")
			if !ok {
				fmt.Fprintf(stderr, "harnessly secret run: %s VAR=NAME\n", flag)
				return 2
			}
			if flag == "--env" {
				vars[v] = n
			} else {
				files[v] = n
			}
			names = append(names, n)
		case "--code":
			fmt.Fprintln(stderr, "harnessly secret run --code на устройстве не работает: запустите эту команду на сервере")
			return 2
		}
	}
	cmdArgs := args[i:]
	if len(cmdArgs) == 0 || len(names) == 0 {
		fmt.Fprintln(stderr, "usage: harnessly secret run --env VAR=NAME [--file VAR=NAME] -- COMMAND [ARGS…]")
		return 2
	}
	m, err := shimCall(shimReq{Kind: "secrets", Args: names})
	if err == nil && m.Error != "" {
		err = errors.New(m.Error)
	}
	if err != nil {
		fmt.Fprintln(stderr, "harnessly secret run:", err)
		return 1
	}
	env := os.Environ()
	var secrets []string
	var cleanup []string
	for v, n := range vars {
		env = append(env, v+"="+m.Env[n])
		secrets = append(secrets, m.Env[n])
	}
	for v, n := range files {
		f, err := os.CreateTemp("", "harnessly-secret-*")
		if err != nil {
			fmt.Fprintln(stderr, "harnessly secret run:", err)
			return 1
		}
		_ = f.Chmod(0o600)
		_, _ = f.WriteString(m.Env[n])
		f.Close()
		cleanup = append(cleanup, f.Name())
		env = append(env, v+"="+f.Name())
		secrets = append(secrets, m.Env[n])
	}
	defer func() {
		for _, p := range cleanup {
			os.Remove(p)
		}
	}()
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = &masker{w: stdout, secrets: secrets}
	cmd.Stderr = &masker{w: stderr, secrets: secrets}
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		fmt.Fprintln(stderr, "harnessly secret run:", err)
		return 127
	}
	return 0
}

// masker hides the secrets' values in what passes through it.
type masker struct {
	w       io.Writer
	secrets []string
}

func (m *masker) Write(p []byte) (int, error) {
	s := string(p)
	for _, v := range m.secrets {
		if len(v) >= 4 {
			s = strings.ReplaceAll(s, v, "********")
		}
	}
	_, err := m.w.Write([]byte(s))
	return len(p), err
}
