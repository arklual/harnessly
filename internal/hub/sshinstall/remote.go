package sshinstall

import (
	"fmt"
	"strconv"
	"strings"
)

// shellQuote quotes a value for sh in single quotes.
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }

// inSh wraps a script so that the server's login shell (fish and csh included) hands it to sh.
func inSh(script string) string { return "sh -c " + shellQuote(script) }

// preflightScript looks at the server and prints key=value lines. space is the folder whose filesystem will hold the
// installation.
func preflightScript(space string) string {
	return strings.Join([]string{
		`echo "os=$(uname -s)"`,
		`echo "arch=$(uname -m)"`,
		`echo "uid=$(id -u)"`,
		`if command -v curl >/dev/null 2>&1; then echo curl=1; else echo curl=0; fi`,
		`if [ "$(id -u)" = 0 ]; then echo sudo=1; elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then echo sudo=1; else echo sudo=0; fi`,
		`echo "free_kb=$(df -Pk ` + space + ` 2>/dev/null | awk 'NR==2{print $4}')"`,
		`if [ -r /proc/meminfo ]; then echo "mem_kb=$(awk '/^MemTotal:/{print $2}' /proc/meminfo)"; else echo "mem_bytes=$(sysctl -n hw.memsize 2>/dev/null)"; fi`,
	}, "\n")
}

// facts is what preflightScript printed.
type facts struct {
	OS, Arch      string
	UID           int
	Curl, Sudo    bool
	FreeBytes     int64
	MemTotalBytes int64
}

func parseFacts(out string) facts {
	kv := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	num := func(k string) int64 { n, _ := strconv.ParseInt(kv[k], 10, 64); return n }
	f := facts{OS: strings.ToLower(kv["os"]), UID: int(num("uid")), Curl: kv["curl"] == "1", Sudo: kv["sudo"] == "1", FreeBytes: num("free_kb") << 10}
	switch strings.ToLower(kv["arch"]) {
	case "x86_64", "amd64":
		f.Arch = "amd64"
	case "aarch64", "arm64":
		f.Arch = "arm64"
	default:
		f.Arch = strings.ToLower(kv["arch"])
	}
	if n := num("mem_kb"); n > 0 {
		f.MemTotalBytes = n << 10
	} else {
		f.MemTotalBytes = num("mem_bytes")
	}
	return f
}

const (
	gib = 1 << 30
	// The thresholds of the machine's own preflight (api.md): below these the installation is refused.
	minFree = 3 * gib
	minMem  = 2 * gib
)

// problem says why a server cannot take the install, in words for the person; "" when it can.
func (f facts) problem(profile string) string {
	switch {
	case f.OS != "linux" && f.OS != "darwin":
		return "Поддерживаются Linux и macOS, а на сервере " + orUnknown(f.OS)
	case f.Arch != "amd64" && f.Arch != "arm64":
		return "Поддерживаются процессоры amd64 и arm64, а на сервере " + orUnknown(f.Arch)
	case f.OS == "darwin" && profile != "full":
		return "На macOS работает только режим «С вашим доступом»"
	case !f.Curl:
		return "На сервере нет curl: поставьте его и попробуйте снова"
	case profile != "full" && !f.Sudo:
		return "Для режима «В отдельной папке» нужен root или sudo без пароля: войдите под root или выберите «С вашим доступом»"
	case f.FreeBytes > 0 && f.FreeBytes < minFree:
		return fmt.Sprintf("Свободно %s, нужно хотя бы 3 ГБ", gb(f.FreeBytes))
	case f.MemTotalBytes > 0 && f.MemTotalBytes < minMem:
		return fmt.Sprintf("Памяти %s, нужно хотя бы 2 ГБ", gb(f.MemTotalBytes))
	}
	return ""
}

func orUnknown(v string) string {
	if v == "" {
		return "не удалось определить систему"
	}
	return v
}

func gb(n int64) string {
	return strconv.FormatFloat(float64(n)/gib, 'f', 1, 64) + " ГБ"
}

// summary is the detail of the preflight step: «412 ГБ свободно · 64 ГБ памяти».
func (f facts) summary() string {
	var parts []string
	if f.FreeBytes > 0 {
		parts = append(parts, fmt.Sprintf("%d ГБ свободно", f.FreeBytes/gib))
	}
	if f.MemTotalBytes > 0 {
		parts = append(parts, fmt.Sprintf("%d ГБ памяти", (f.MemTotalBytes+gib/2)/gib))
	}
	return strings.Join(parts, " · ")
}

// installScript is the install command of the connect screen as one shell script. The installer goes through a file,
// not a pipe, so that a failed download is a failed install (sh would run an empty script and succeed). Outside root
// it runs under sudo -n for the profiles that need it.
func installScript(hubURL, code, profile string, uid int) string {
	run := "sh \"$f\" --token " + shellQuote(code) + " --profile " + shellQuote(profile)
	if uid != 0 && profile != "full" {
		run = "sudo -n " + run
	}
	// A non-interactive SSH session has no XDG_RUNTIME_DIR, and the user units of the full profile need it.
	return `export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"; ` +
		`f=$(mktemp) || exit 1; trap 'rm -f "$f"' EXIT; ` +
		"curl -fsSL " + shellQuote(hubURL+"/install") + ` -o "$f" || exit 1; ` + run
}
