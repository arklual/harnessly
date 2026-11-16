package kitadd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/confine"
)

// A kit that an HTTP request names is somebody's code: its modules are loaded to read the props, its stylesheet is
// compiled by a Tailwind that follows @plugin and @config into it. So the programs of a sandboxed run (npm to install,
// node to analyse and build) run under Landlock and seccomp (internal/confine):
//   - files: read only what a program needs (system folders, the node install), write only the build folder; the
//     home, the safe, the tokens, the projects and the other processes' /proc are not there;
//   - network: npm may connect to TCP 443 (and the registry's own port) and nothing else; node has no sockets at all;
//   - environment: PATH, HOME and TMPDIR inside the build folder; nothing else of the runtime's environment.
//
// The helper that applies the rules is this binary run again with SandboxHelperArg: the runtime, the harnessly CLI and
// the tests call RunSandboxHelper for it before anything else.

// SandboxHelperArg is the first argument of the helper process.
const SandboxHelperArg = "__kit-confine"

// SandboxAvailable says whether a sandboxed run can be made on this machine.
func SandboxAvailable() bool { return confine.Available() > 0 }

// RunSandboxHelper is the helper's main: `--net none|ports [--port N]… [--rw P]… [--ro P]… -- command args`. It does
// not return when it succeeds.
func RunSandboxHelper(args []string) error {
	r := confine.Rules{}
	i := 0
	for ; i < len(args) && args[i] != "--"; i += 2 {
		if i+1 >= len(args) {
			return errors.New("sandbox helper: a flag without a value")
		}
		v := args[i+1]
		switch args[i] {
		case "--rw":
			r.RW = append(r.RW, v)
		case "--ro":
			r.RO = append(r.RO, v)
		case "--port":
			n, err := strconv.ParseUint(v, 10, 16)
			if err != nil {
				return fmt.Errorf("sandbox helper: port %q", v)
			}
			r.Ports = append(r.Ports, uint16(n))
		case "--net":
			switch v {
			case "none":
				r.Net = confine.NetNone
			case "ports":
				r.Net = confine.NetPorts
			case "open":
				r.Net = confine.NetOpen
			default:
				return fmt.Errorf("sandbox helper: net %q", v)
			}
		default:
			return fmt.Errorf("sandbox helper: flag %q", args[i])
		}
	}
	if i >= len(args) {
		return errors.New("sandbox helper: no command")
	}
	return confine.Exec(r, args[i+1:])
}

// What every sandboxed program may read: programs, libraries, the few settings of /etc, devices and the machine's
// numbers. Not /run (the sockets of daemons), not /proc as a whole (the environment of the runtime), not the home.
var sandboxSystemRO = []string{
	"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/snap",
	"/etc/ssl", "/etc/ca-certificates", "/etc/ca-certificates.conf", "/etc/ld.so.cache", "/etc/ld.so.conf", "/etc/ld.so.conf.d",
	"/etc/alternatives", "/etc/passwd", "/etc/group", "/etc/nsswitch.conf", "/etc/resolv.conf", "/etc/hosts", "/etc/host.conf",
	"/etc/gai.conf", "/etc/localtime", "/etc/os-release", "/etc/mime.types", "/etc/services", "/etc/protocols",
	"/var/lib/ca-certificates", "/run/systemd/resolve",
	"/proc/self", "/proc/cpuinfo", "/proc/meminfo", "/proc/stat", "/proc/version", "/proc/filesystems", "/proc/sys/vm",
	"/sys/devices/system/cpu", "/sys/fs/cgroup",
	"/dev/urandom", "/dev/random", "/dev/zero",
}

// sandboxPrefixes are the folders a program's own install lives in (a node under a version manager is not in /usr).
func sandboxPrefixes(name string) []string {
	var out []string
	for _, n := range []string{name, "node"} {
		p, err := exec.LookPath(n)
		if err != nil {
			continue
		}
		for _, q := range []string{p, realPath(p)} {
			out = append(out, filepath.Dir(q))
			if up := filepath.Dir(filepath.Dir(q)); up != "/" && up != "." && !coversHome(up) {
				out = append(out, up)
			}
		}
	}
	return out
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// runCmd runs a program of the build; in a sandbox it runs it under the rules above.
func (o *Options) runCmd(dir, name string, args ...string) ([]byte, error) {
	if !o.Sandbox {
		return runCmd(dir, nil, name, args...)
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	helper := o.SandboxHelper
	if len(helper) == 0 {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		helper = []string{self, SandboxHelperArg}
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	home := filepath.Join(dir, ".kit-home")
	if err := os.MkdirAll(filepath.Join(home, "tmp"), 0o755); err != nil {
		return nil, err
	}
	cmdline := append([]string{}, helper[1:]...)
	if name == "npm" {
		// npm installs: TCP to the registry's port only (where the kernel can say so). The hosts are not limited by the
		// kernel; the lock file is, before anything is fetched from it (checkLock).
		if confine.Available() >= 4 {
			cmdline = append(cmdline, "--net", "ports")
			for _, p := range o.registryPorts() {
				cmdline = append(cmdline, "--port", strconv.Itoa(int(p)))
			}
		}
	} else {
		cmdline = append(cmdline, "--net", "none")
	}
	cmdline = append(cmdline, "--rw", dir, "--rw", "/dev/null")
	ro := append([]string{}, sandboxSystemRO...)
	ro = append(ro, sandboxPrefixes(name)...)
	for _, p := range ro {
		cmdline = append(cmdline, "--ro", p)
	}
	cmdline = append(cmdline, "--", bin)
	cmdline = append(cmdline, args...)
	cmd := exec.Command(helper[0], cmdline...)
	cmd.Dir = dir
	path := filepath.Dir(bin)
	if nodeBin, err := exec.LookPath("node"); err == nil {
		path += ":" + filepath.Dir(nodeBin)
	}
	cmd.Env = []string{
		"PATH=" + path + ":/usr/bin:/bin",
		"HOME=" + home, "TMPDIR=" + filepath.Join(home, "tmp"),
		"LANG=C.UTF-8", "CI=1", "NO_COLOR=1",
		"npm_config_cache=" + filepath.Join(home, "npm-cache"),
		"npm_config_userconfig=" + filepath.Join(home, "npmrc"), "npm_config_globalconfig=" + filepath.Join(home, "npmglobalrc"),
		"npm_config_update_notifier=false", "npm_config_fund=false", "npm_config_audit=false",
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(errb.String())
		if len(tail) > 2000 {
			tail = "…" + tail[len(tail)-2000:]
		}
		return out.Bytes(), fmt.Errorf("%s %s (sandbox): %v\n%s", name, strings.Join(args, " "), err, tail)
	}
	return out.Bytes(), nil
}

// registryPorts are the TCP ports npm may connect to: 443, and the registry's own when the person named one.
func (o *Options) registryPorts() []uint16 {
	ports := []uint16{443}
	if o.Registry != "" {
		if u, err := url.Parse(o.Registry); err == nil && u.Port() != "" {
			if n, err := strconv.ParseUint(u.Port(), 10, 16); err == nil && n != 443 {
				ports = append(ports, uint16(n))
			}
		}
	}
	return ports
}

// allowedHosts are the hosts a package may be fetched from: the registry npm uses, the person's named one, the
// machine's list (AllowedHosts).
func (o *Options) allowedHosts() map[string]bool {
	m := map[string]bool{"registry.npmjs.org": true}
	for _, h := range o.AllowedHosts {
		if h = strings.TrimSpace(h); h != "" {
			m[strings.ToLower(h)] = true
		}
	}
	if o.Registry != "" {
		if u, err := url.Parse(o.Registry); err == nil {
			m[strings.ToLower(u.Hostname())] = true
		}
	}
	return m
}

// checkLock reads the package-lock.json npm wrote with --package-lock-only and refuses what would be fetched from
// anywhere but an allowed registry: a git or file dependency, a tarball address on another host, a link, a plain-http
// address (a loopback one only when it is listed). Nothing has been downloaded yet when it runs.
func checkLock(dir string, hosts map[string]bool) error {
	raw, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		return fmt.Errorf("the dependency lock was not written: %w", err)
	}
	var lock struct {
		Packages map[string]struct {
			Resolved string `json:"resolved"`
			Link     bool   `json:"link"`
			InBundle bool   `json:"inBundle"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(raw, &lock); err != nil {
		return fmt.Errorf("the dependency lock is not readable: %w", err)
	}
	for key, p := range lock.Packages {
		if key == "" {
			continue
		}
		name := strings.TrimPrefix(key, "node_modules/")
		if p.Link {
			return fmt.Errorf("dependency %s is a link to a folder: refused", name)
		}
		if p.Resolved == "" {
			if p.InBundle {
				continue
			}
			return fmt.Errorf("dependency %s has no registry address: refused", name)
		}
		u, err := url.Parse(p.Resolved)
		if err != nil || u.User != nil {
			return fmt.Errorf("dependency %s comes from %q: refused", name, p.Resolved)
		}
		host := strings.ToLower(u.Hostname())
		if host == "" || !strings.HasPrefix(p.Resolved, u.Scheme+"://"+u.Host) {
			return fmt.Errorf("dependency %s comes from %q (no registry host): refused", name, p.Resolved)
		}
		ip := net.ParseIP(host)
		loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
		switch {
		case u.Scheme == "https":
		case u.Scheme == "http" && loopback:
		default:
			return fmt.Errorf("dependency %s comes from %q (only https from a registry is installed): refused", name, p.Resolved)
		}
		if !hosts[host] {
			return fmt.Errorf("dependency %s comes from %s, which is not an allowed registry: refused", name, host)
		}
	}
	return nil
}

// Verified says whether a source is one the team's recipes know by name (MUI, Ant Design, Chakra, HeroUI, PrimeNG, a
// shadcn registry): the command trusts these without the sandbox. Any other package runs sandboxed where it can.
func Verified(src Source) bool {
	switch src.Kind {
	case "npm":
		if _, ok := rootRecipes[src.Package]; ok {
			return true
		}
		if _, ok := sampleRecipes[src.Package]; ok {
			return true
		}
		_, ok := ngPresets[src.Package]
		return ok
	case "shadcn":
		return src.Base == ""
	}
	return false
}

// coversHome says whether a folder is the person's home, above it or a folder directly in it (~/.local): too wide to
// hand a package read access to, whatever a node under it needs.
func coversHome(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	for _, h := range []string{home, realPath(home)} {
		if dir == h || strings.HasPrefix(h, dir+string(filepath.Separator)) || filepath.Dir(dir) == h {
			return true
		}
	}
	return false
}
