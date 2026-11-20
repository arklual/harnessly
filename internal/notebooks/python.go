package notebooks

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/proc"
)

//go:embed helper.py
var pythonHelper string

const dependencyMessage = "Notebook execution needs Python with jupyter_client and ipykernel. Install them with python3 -m pip install jupyter_client ipykernel, or create a virtualenv, install them there, and set RUNTIME_NOTEBOOK_PYTHON to its absolute bin/python path."

type reply struct {
	ID      uint64 `json:"id"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Result  Result `json:"result"`
	PID     int    `json:"pid,omitempty"`
	Ready   bool   `json:"ready,omitempty"`
}

type python struct {
	cmd       *exec.Cmd
	in        io.WriteCloser
	mu        sync.Mutex
	pending   map[uint64]chan reply
	writeMu   sync.Mutex
	seq       atomic.Uint64
	kernelPID atomic.Int64
	done      chan struct{}
	once      sync.Once
}

// runtime tokens, connector credentials, proxy authentication, PYTHONPATH and
// startup hooks are deliberately absent. IPython/Jupyter use fresh config dirs.
func cleanEnv(dir string) []string {
	out := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "PYTHONNOUSERSITE=1", "IPYTHONDIR=" + dir, "JUPYTER_CONFIG_DIR=" + dir, "JUPYTER_DATA_DIR=" + dir, "JUPYTER_RUNTIME_DIR=" + dir}
	for _, name := range []string{"HOME", "TZ"} {
		if v := os.Getenv(name); v != "" {
			out = append(out, name+"="+v)
		}
	}
	return out
}

func startPython(life context.Context, path string) (kernel, error) {
	interpreter := os.Getenv("RUNTIME_NOTEBOOK_PYTHON")
	if interpreter == "" {
		// Search a fixed machine path, not a conversation's executable directory.
		for _, candidate := range []string{"/usr/local/bin/python3", "/usr/bin/python3", "/bin/python3"} {
			if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
				interpreter = candidate
				break
			}
		}
	}
	if !validInterpreter(interpreter) {
		return nil, failure("dependencies", dependencyMessage)
	}
	dir, err := os.MkdirTemp("", "notebook-kernel-*")
	if err != nil {
		return nil, failure("internal", "Cannot create a private notebook runtime directory.")
	}
	cmd := exec.Command(interpreter, "-I", "-u", "-c", pythonHelper, path, strconv.Itoa(MaxOutputBytes), strconv.Itoa(MaxOutputs))
	cmd.Dir = dir
	cmd.Env = cleanEnv(dir)
	// Jupyter creates a separate kernel process group. Track that pid as well as
	// the helper's group so timeout/shutdown can stop both. This is not a sandbox.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	p := &python{cmd: cmd, pending: make(map[uint64]chan reply), done: make(chan struct{})}
	p.pending[0] = make(chan reply, 1)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	p.in = stdin
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	// Kernel/user stdout never shares this protocol, and stderr must not grow an
	// unbounded buffer or leak machine paths/credentials into an HTTP error.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		os.RemoveAll(dir)
		return nil, failure("dependencies", dependencyMessage)
	}
	ready := p.pending[0]
	go func() {
		defer close(p.done)
		defer os.RemoveAll(dir)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64<<10), MaxOutputBytes+(64<<10))
		for scanner.Scan() {
			var v reply
			if json.Unmarshal(scanner.Bytes(), &v) != nil {
				break
			}
			if v.ID == 0 && v.PID > 0 {
				p.kernelPID.Store(int64(v.PID))
			}
			if v.ID == 0 && !v.Ready && v.Error == "" {
				continue
			}
			p.mu.Lock()
			ch := p.pending[v.ID]
			p.mu.Unlock()
			if ch != nil {
				select {
				case ch <- v:
				default:
				}
			}
		}
		// EOF or an over-limit/invalid protocol frame means this session is dead.
		p.kill()
		_ = cmd.Wait()
	}()
	go func() {
		select {
		case <-life.Done():
			p.Close()
		case <-p.done:
		}
	}()
	ctx, cancel := context.WithTimeout(life, StartupTimeout)
	defer cancel()
	select {
	case v := <-ready:
		p.mu.Lock()
		delete(p.pending, 0)
		p.mu.Unlock()
		if v.Error != "" {
			p.Close()
			return nil, failure(v.Error, v.Message)
		}
		return p, nil
	case <-p.done:
		return nil, failure("dependencies", dependencyMessage)
	case <-ctx.Done():
		p.Close()
		return nil, contextError(ctx)
	}
}

func (p *python) Alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *python) call(ctx context.Context, action, code string) (reply, error) {
	id := p.seq.Add(1)
	ch := make(chan reply, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
	b, err := json.Marshal(struct {
		ID     uint64 `json:"id"`
		Action string `json:"action"`
		Code   string `json:"code,omitempty"`
	}{id, action, code})
	if err != nil {
		return reply{}, err
	}
	// A kernel process may stop reading. A deadline must still kill it instead
	// of leaving a goroutine stuck writing a code-sized request to its pipe.
	stop := context.AfterFunc(ctx, func() { p.Close() })
	defer stop()
	p.writeMu.Lock()
	_, err = p.in.Write(append(b, '\n'))
	p.writeMu.Unlock()
	if err != nil {
		return reply{}, failure("kernel", "The notebook kernel stopped. Run the cell again to start a fresh kernel.")
	}
	select {
	case v := <-ch:
		if v.Error != "" {
			return v, failure(v.Error, v.Message)
		}
		return v, nil
	case <-ctx.Done():
		return reply{}, contextError(ctx)
	case <-p.done:
		if ctx.Err() != nil {
			return reply{}, contextError(ctx)
		}
		return reply{}, failure("kernel", "The notebook kernel stopped. Run the cell again to start a fresh kernel.")
	}
}

func (p *python) Execute(ctx context.Context, code string) (Result, error) {
	v, err := p.call(ctx, "execute", code)
	if err != nil {
		return Result{}, err
	}
	b, err := json.Marshal(v.Result)
	if err != nil || len(b) > MaxOutputBytes || len(v.Result.Outputs) > MaxOutputs || (v.Result.Status != "ok" && v.Result.Status != "error") {
		return Result{}, failure("output-limit", fmt.Sprintf("Notebook output exceeded %d MiB; the kernel was stopped.", MaxOutputBytes>>20))
	}
	if v.Result.Outputs == nil {
		v.Result.Outputs = []json.RawMessage{}
	}
	return v.Result, nil
}

func (p *python) Interrupt(ctx context.Context) error {
	_, err := p.call(ctx, "interrupt", "")
	return err
}

func (p *python) kill() {
	// Covers the small startup window before Jupyter reports the kernel pid,
	// and children which moved to their own process group on Linux.
	children := proc.Descendants(p.cmd.Process.Pid)
	for i := len(children) - 1; i >= 0; i-- {
		_ = syscall.Kill(children[i], syscall.SIGKILL)
	}
	if pid := int(p.kernelPID.Load()); pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

func (p *python) Close() {
	p.once.Do(func() {
		// Give Jupyter a short opportunity to reap its child and unlink its files;
		// a stuck helper/kernel is still bounded by the force-kill fallback.
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(time.Second):
			p.kill()
		}
		_ = p.in.Close()
	})
	<-p.done
}

// Avoid accepting a command line where configuration promised one executable.
func validInterpreter(path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsRune(path, 0)
}
