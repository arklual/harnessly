package basepkg

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Service is `harnessly-agent basepkg install`: the root service's one job. It installs the system part of its own
// list and nothing else; from the inbox it takes only the skipped ids and a proxy address (ReadInbox).
func Service(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("basepkg", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state", "/var/lib/harnessly/basepkg", "the service's own folder (root's)")
	inbox := fs.String("inbox", "/var/lib/harnessly/.harnessly/basepkg", "the folder the runtime writes")
	if len(args) == 0 || args[0] != "install" {
		fmt.Fprintln(stderr, "usage: harnessly-agent basepkg install [--state DIR] [--inbox DIR]")
		return 2
	}
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(stderr, "harnessly-agent basepkg: only root installs system packages")
		return 1
	}
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "harnessly-agent basepkg:", err)
		return 1
	}
	lock, err := os.OpenFile(filepath.Join(*stateDir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintln(stderr, "harnessly-agent basepkg:", err)
		return 1
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(stdout, "basepkg: an install is already going")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	in := &Installer{
		Manager:  Detect(exec.LookPath, runtime.GOOS),
		Run:      runCmd,
		LookPath: exec.LookPath,
		Free:     func() (uint64, error) { return freeAt("/") },
		Busy:     busy,
		Now:      time.Now,
		Save: func(st State) {
			if err := WriteState(*stateDir, st); err != nil {
				fmt.Fprintln(stderr, "basepkg: state:", err)
			}
		},
	}
	st := in.Install(ctx, ReadInbox(*inbox))
	for _, s := range st.Items {
		if s.Status != OK {
			fmt.Fprintf(stdout, "basepkg: %s %s %s\n", s.ID, s.Status, s.Message)
		}
	}
	return 0
}

func runCmd(ctx context.Context, env, args []string) (string, error) {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Env = append(os.Environ(), env...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func freeAt(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// busy: the 1-minute load is above the cores × 2 (no /proc: not busy).
func busy() bool {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return false
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return false
	}
	l, err := strconv.ParseFloat(f[0], 64)
	return err == nil && l > float64(runtime.NumCPU()*2)
}
