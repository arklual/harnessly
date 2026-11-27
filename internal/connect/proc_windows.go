//go:build windows

package connect

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// signalGroup: Windows has no signals for a group; every signal ends the command's whole tree.
func signalGroup(cmd *exec.Cmd, sig string) {
	if cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if kill.Run() != nil {
		_ = cmd.Process.Kill()
	}
}

func exitCode(ee *exec.ExitError) int { return ee.ExitCode() }

// findShell finds Git Bash, the shell Claude Code itself uses on Windows; never WSL's bash.exe in System32.
func findShell() string {
	var cands []string
	if p := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); p != "" {
		cands = append(cands, p)
	}
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if base := os.Getenv(env); base != "" {
			cands = append(cands, filepath.Join(base, "Git", "bin", "bash.exe"), filepath.Join(base, "Programs", "Git", "bin", "bash.exe"))
		}
	}
	if git, err := exec.LookPath("git"); err == nil {
		// …\Git\cmd\git.exe → …\Git\bin\bash.exe
		cands = append(cands, filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe"))
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func shellMissing() string {
	return "на устройстве нет Git Bash: установите Git for Windows (https://git-scm.com/downloads/win) или укажите CLAUDE_CODE_GIT_BASH_PATH"
}

// toShellPath: C:\Users\m → /c/Users/m, the form Git Bash and the agent see.
func toShellPath(p string) string {
	if len(p) >= 2 && p[1] == ':' {
		rest := strings.ReplaceAll(p[2:], `\`, "/")
		return "/" + strings.ToLower(p[:1]) + rest
	}
	return strings.ReplaceAll(p, `\`, "/")
}

// fromShellPath: /c/Users/m → C:\Users\m.
func fromShellPath(p string) string {
	if len(p) >= 2 && p[0] == '/' && (len(p) == 2 || p[2] == '/') {
		c := p[1]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			return strings.ToUpper(string(c)) + ":" + filepath.FromSlash(p[2:]+map[bool]string{true: "/", false: ""}[len(p) == 2])
		}
	}
	return filepath.FromSlash(p)
}
