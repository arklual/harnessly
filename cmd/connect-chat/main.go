// connect-chat starts an agent whose chat lives on a Harnessly Connect device. The device's folder is mounted here
// (FUSE) and shown at its own path (/Users/masha/proj, /c/Users/m/proj) in a bubblewrap view of this machine where
// everything else stays in place; the agent starts there with its Bash going to the device (connect-bash), and the
// mount goes when the agent ends.
//
//	connect-chat --device NAME --dir /Users/masha/proj [--cwd /Users/masha/proj/sub] -- claude [args…]
//
// As the runtime's Claude launcher (RUNTIME_CLAUDE_LAUNCHER pointing at a link named claude-connect) it decides for
// every Claude the runtime starts: the runtime says whether the chat of its tmux session lives in a device folder;
// if so it runs as above, else it becomes the real claude.
//
// Mounting: the runtime runs NoNewPrivileges, so the setuid fusermount does not work from a chat, and Ubuntu's
// AppArmor gives bubblewrap's namespaces no capabilities to mount by themselves. The mount is served by a transient
// unit of the person's own systemd (systemd-run --user), outside that restriction, as the same user; bwrap then only
// binds the mounted folder, which needs no privilege.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/connect"
	"github.com/ly-solutions/harnessly/internal/connect/mount"
)

func main() {
	if filepath.Base(os.Args[0]) == "claude-connect" {
		os.Exit(dispatch(os.Args[1:]))
	}
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("connect-chat", flag.ContinueOnError)
	device := fs.String("device", os.Getenv(connect.DeviceEnv), "the device")
	dir := fs.String("dir", "", "the folder on the device, its shell's form (/c/… on Windows)")
	cwd := fs.String("cwd", "", "where the agent starts, in dir (dir when empty)")
	serve := fs.String("serve-mount", "", "internal: serve the mount at this folder")
	parent := fs.Int("parent", 0, "internal: unmount when this process ends")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	cmd := fs.Args()
	if *device == "" || *dir == "" || (*serve == "" && len(cmd) == 0) {
		fmt.Fprintln(os.Stderr, "usage: connect-chat --device NAME --dir PATH -- COMMAND [ARGS…]")
		return 2
	}
	base, token, header, err := connect.Endpoint(os.Getenv)
	if err != nil {
		return fail(err)
	}
	c := &mount.Client{Base: base, Token: token, Header: header, Device: *device}
	if *serve != "" {
		return serveMount(c, *dir, *serve, *parent)
	}

	st := c.Do(&connect.FSReq{Op: "stat", Path: *dir})
	switch {
	case st.Errno == "ENOTCONN":
		return fail(fmt.Errorf("устройство «%s» офлайн: откройте Harnessly Connect на нём", *device))
	case st.Errno != "":
		return fail(fmt.Errorf("на устройстве «%s» нет папки %s (%s)", *device, *dir, st.Errno))
	case st.Stat.Mode&connect.SIFMT != connect.SIFDIR:
		return fail(fmt.Errorf("%s на устройстве не папка", *dir))
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return fail(errors.New("нужен bubblewrap (bwrap) на этой машине"))
	}
	self, _ := os.Executable()
	shell := filepath.Join(filepath.Dir(self), "connect-bash")
	if _, err := os.Stat(shell); err != nil {
		return fail(fmt.Errorf("нет %s рядом с connect-chat", shell))
	}

	data := os.Getenv("HARNESSLY_DATA_DIR")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".harnessly")
	}
	sum := sha256.Sum256([]byte(*device + "\x00" + *dir))
	name := fmt.Sprintf("%s-%d", hex.EncodeToString(sum[:6]), os.Getpid())
	mnt := filepath.Join(data, "connect", "mnt", name)
	if err := os.MkdirAll(mnt, 0o700); err != nil {
		return fail(err)
	}
	defer os.Remove(mnt)
	stop, err := startMount(self, name, *device, *dir, mnt, data, token)
	if err != nil {
		return fail(fmt.Errorf("не смонтировать папку устройства: %w", err))
	}
	defer stop()

	start := *dir
	if *cwd != "" && (*cwd == *dir || strings.HasPrefix(*cwd, strings.TrimSuffix(*dir, "/")+"/")) {
		start = *cwd
	}
	args := append(mount.BwrapArgs(mnt, *dir), "--chdir", start,
		"--setenv", "CLAUDE_CODE_SHELL", shell, "--setenv", connect.DeviceEnv, *device, "--setenv", "PWD", start, "--")
	return relay(exec.Command(bwrap, append(args, cmd...)...))
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "harnessly connect:", err)
	return 1
}

// startMount mounts the device's folder at mnt and gives the way to unmount it. Through systemd-run --user when it
// is there (a chat's process cannot use fusermount), else in this process.
func startMount(self, name, device, dir, mnt, data, token string) (func(), error) {
	if run, err := exec.LookPath("systemd-run"); err == nil {
		// The unit reads the token from a file only this user reads, not from its own command line or properties.
		tokFile := filepath.Join(filepath.Dir(mnt), "."+name+".token")
		if err := os.WriteFile(tokFile, []byte(token), 0o600); err != nil {
			return nil, err
		}
		unit := "harnessly-connect-mount-" + name
		args := []string{"--user", "--quiet", "--collect", "--unit", unit, "-p", "NoNewPrivileges=no",
			"--setenv", "HARNESSLY_DATA_DIR=" + data, "--setenv", "HARNESSLY_CLI_TOKEN_FILE=" + tokFile}
		if u := os.Getenv("HARNESSLY_URL"); u != "" {
			args = append(args, "--setenv", "HARNESSLY_URL="+u)
		}
		if h := os.Getenv("HARNESSLY_CLI_HEADER"); h != "" {
			args = append(args, "--setenv", "HARNESSLY_CLI_HEADER="+h)
		}
		args = append(args, self, "--device", device, "--dir", dir, "--serve-mount", mnt, "--parent", fmt.Sprint(os.Getpid()))
		if out, err := exec.Command(run, args...).CombinedOutput(); err != nil {
			os.Remove(tokFile)
			return nil, fmt.Errorf("systemd-run: %v %s", err, strings.TrimSpace(string(out)))
		}
		stop := func() {
			_ = exec.Command("systemctl", "--user", "stop", unit+".service").Run()
			os.Remove(tokFile)
		}
		if err := waitMounted(mnt, 15*time.Second); err != nil {
			stop()
			return nil, err
		}
		return stop, nil
	}
	c := &mount.Client{Device: device}
	c.Base, c.Token, c.Header, _ = connect.Endpoint(os.Getenv)
	srv, err := mount.Mount(c, dir, mnt, false)
	if err != nil {
		return nil, err
	}
	return func() { _ = srv.Unmount() }, nil
}

// serveMount is the unit's side: the mount lives until the unit is stopped or the chat's process is gone.
func serveMount(c *mount.Client, dir, mnt string, parent int) int {
	srv, err := mount.Mount(c, dir, mnt, false)
	if err != nil {
		return fail(err)
	}
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-sigs:
			_ = srv.Unmount()
			return 0
		case <-tick.C:
			if parent > 0 && syscall.Kill(parent, 0) != nil {
				_ = srv.Unmount()
				return 0
			}
		}
	}
}

func waitMounted(mnt string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if mounted(mnt) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("папка устройства не смонтировалась за " + d.String())
}

func mounted(mnt string) bool {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) > 4 && fields[4] == mnt {
			return true
		}
	}
	return false
}

// relay runs a child with this process's terminal, passes signals on and returns its exit code.
func relay(child *exec.Cmd) int {
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		return fail(err)
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH)
	go func() {
		for s := range sigs {
			_ = child.Process.Signal(s)
		}
	}()
	err := child.Wait()
	signal.Stop(sigs)
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	if err != nil {
		return fail(err)
	}
	return 0
}
