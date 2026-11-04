package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

// Diagnostic separates a copyable repair command from the human explanation.
type Diagnostic struct{ Message, Command string }

// Error keeps the repair detail available to both tools and the panel.
func (d *Diagnostic) Error() string { return d.Message }

func releaseValue(release, key string) string {
	for _, line := range strings.Split(release, "\n") {
		if value, ok := strings.CutPrefix(line, key+"="); ok {
			return strings.Trim(value, "\"'")
		}
	}
	return ""
}

// InstallCommand suggests a native browser, including its libraries, for the detected distribution.
func InstallCommand(release string) string {
	id := releaseValue(release, "ID")
	switch id {
	case "ubuntu":
		return "sudo apt-get update && sudo apt-get install -y chromium-browser xvfb fonts-liberation fonts-noto-color-emoji"
	case "alpine":
		return "sudo apk add chromium xvfb font-liberation font-noto-emoji"
	case "arch", "manjaro":
		return "sudo pacman -S --needed chromium xorg-server-xvfb ttf-liberation noto-fonts-emoji"
	case "fedora", "rhel", "rocky", "almalinux", "centos":
		return "sudo dnf install -y chromium xorg-x11-server-Xvfb liberation-fonts google-noto-color-emoji-fonts"
	default:
		return "sudo apt-get update && sudo apt-get install -y chromium xvfb fonts-liberation fonts-noto-color-emoji"
	}
}

// LibrariesCommand avoids Ubuntu's chromium snap wrapper when only shared libraries are missing.
func LibrariesCommand(release string) string {
	id := releaseValue(release, "ID")
	if id == "ubuntu" || id == "debian" {
		suffix := ""
		major, _ := strconv.Atoi(strings.Split(releaseValue(release, "VERSION_ID"), ".")[0])
		if id == "ubuntu" && major >= 24 || id == "debian" && major >= 13 {
			suffix = "t64"
		}
		return "sudo apt-get update && sudo apt-get install -y libnss3 libnspr4 libatk1.0-0" + suffix +
			" libatk-bridge2.0-0" + suffix + " libcups2" + suffix +
			" libdrm2 libxkbcommon0 libxcomposite1 libxdamage1 libxfixes3 libxrandr2 libgbm1 libasound2" + suffix
	}
	return InstallCommand(release)
}

func libraryError() error {
	command := LibrariesCommand(osRelease())
	return &Diagnostic{Message: "Chrome has missing shared libraries. Run: " + command, Command: command}
}

func sandboxCommand(chrome string) string {
	path, _ := filepath.Abs(chrome)
	return "sudo tee /etc/apparmor.d/harnessly-browser <<'EOF'\nabi <abi/4.0>,\ninclude <tunables/global>\n" +
		"profile harnessly-browser " + fmt.Sprintf("%q", path) + " flags=(unconfined) {\n  userns,\n}\nEOF\n" +
		"sudo apparmor_parser -r /etc/apparmor.d/harnessly-browser"
}

// SandboxHelp preserves the sandbox by making every privilege-changing remedy an explicit choice.
func SandboxHelp(chrome string) string {
	path, _ := filepath.Abs(chrome)
	helper := filepath.Join(filepath.Dir(path), "chrome-sandbox")
	return "Chrome sandbox is unavailable. Choose one remedy: install system Google Chrome and set RUNTIME_BROWSER_CHROME; " +
		"or ask an administrator to install an AppArmor profile for this exact executable:\n" +
		sandboxCommand(chrome) + "\n" +
		"Or install the trusted sandbox helper: sudo chown root:root " + shellQuote(helper) + " && sudo chmod 4755 " + shellQuote(helper) +
		" (requires a filesystem allowing setuid and a service without NoNewPrivileges). " +
		"Last resort: explicitly set RUNTIME_BROWSER_CHROME_ARGS=--no-sandbox. WARNING: this disables Chrome's security sandbox; " +
		"do not use it for untrusted sites."
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// Mode reports the display the host will choose; lack of Xvfb is a supported headless configuration.
func Mode(requested string) string {
	if requested == "headless" {
		return "headless"
	}
	if _, err := exec.LookPath("Xvfb"); err == nil {
		return "xvfb"
	}
	return "headless"
}

// Inspection is safe to display in a machine check; no browsing data or Chrome log is returned.
type Inspection struct{ Chrome, Version, Mode, Sandbox string }

type probePipe struct {
	io.ReadCloser
	io.WriteCloser
}

func (p probePipe) Close() error { return errors.Join(p.ReadCloser.Close(), p.WriteCloser.Close()) }

// Probe verifies the real debugging pipe and creates a renderer, so sandbox failures are not mistaken for readiness.
func Probe(ctx context.Context, chrome, data string) (Inspection, error) {
	result := Inspection{Chrome: chrome, Mode: Mode(os.Getenv("RUNTIME_BROWSER_MODE")), Sandbox: "not yet checked"}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if os.Getenv("RUNTIME_BROWSER_MODE") == "xvfb" && result.Mode != "xvfb" {
		return result, errors.New("RUNTIME_BROWSER_MODE=xvfb requires Xvfb; install Xvfb or set RUNTIME_BROWSER_MODE=headless")
	}
	if _, err := exec.LookPath("ldd"); err == nil {
		output, _ := exec.CommandContext(ctx, "ldd", chrome).CombinedOutput()
		if strings.Contains(string(output), "not found") || strings.Contains(string(output), "Error loading shared library") {
			return result, libraryError()
		}
	}
	versionCtx, stopVersion := context.WithTimeout(ctx, 3*time.Second)
	versionOutput, versionErr := exec.CommandContext(versionCtx, chrome, "--version").Output()
	stopVersion()
	if versionErr == nil {
		result.Version = strings.TrimSpace(string(versionOutput))
	}
	root := filepath.Join(data, "agent-browser")
	if err := os.MkdirAll(root, 0700); err != nil {
		return result, err
	}
	profile, err := os.MkdirTemp(root, "probe-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(profile)
	rd, wr, err := os.Pipe()
	if err != nil {
		return result, err
	}
	defer rd.Close()
	defer wr.Close()
	reply, replyWriter, err := os.Pipe()
	if err != nil {
		return result, err
	}
	defer reply.Close()
	defer replyWriter.Close()
	args := []string{"--headless=new", "--remote-debugging-pipe", "--user-data-dir=" + profile,
		"--no-first-run", "--no-default-browser-check", "--disable-dev-shm-usage", "--no-startup-window"}
	extra := strings.Fields(os.Getenv("RUNTIME_BROWSER_CHROME_ARGS"))
	for _, arg := range extra {
		if arg == "--no-sandbox" || arg == "--no-sandbox=true" {
			result.Sandbox = "DISABLED explicitly by RUNTIME_BROWSER_CHROME_ARGS"
		}
	}
	args = append(args, extra...)
	cmd := exec.CommandContext(ctx, chrome, args...)
	cmd.ExtraFiles = []*os.File{rd, replyWriter}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "TMPDIR=/tmp")
	var output bytes.Buffer
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		return result, err
	}
	_ = rd.Close()
	_ = replyWriter.Close()
	conn := cdp.New(cdp.NewStream(probePipe{reply, wr}), nil)
	done := make(chan struct{})
	go func() { defer close(done); _ = conn.Run(ctx) }()
	raw, callErr := conn.Call(ctx, "Browser.getVersion", nil, "")
	if callErr == nil {
		var version struct {
			Product string `json:"product"`
		}
		_ = json.Unmarshal(raw, &version)
		result.Version = version.Product
		var target struct {
			ID string `json:"targetId"`
		}
		raw, callErr = conn.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank"}, "")
		if callErr == nil {
			callErr = json.Unmarshal(raw, &target)
		}
		var session struct {
			ID string `json:"sessionId"`
		}
		if callErr == nil {
			raw, callErr = conn.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": target.ID, "flatten": true}, "")
		}
		if callErr == nil {
			callErr = json.Unmarshal(raw, &session)
		}
		if callErr == nil {
			_, callErr = conn.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "1+1"}, session.ID)
		}
	}
	if callErr == nil {
		_, _ = conn.Call(ctx, "Browser.close", nil, "")
	}
	_ = conn.Close()
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Wait()
	<-done
	if callErr != nil {
		result.Sandbox = "unavailable or unverified"
		log := output.String()
		if strings.Contains(log, "sandbox") || strings.Contains(log, "Operation not permitted") || strings.Contains(log, "namespace") {
			return result, &Diagnostic{Message: SandboxHelp(chrome), Command: sandboxCommand(chrome)}
		}
		if strings.Contains(log, "shared libraries") {
			return result, libraryError()
		}
		for _, reason := range []string{"Socket path too long", "Failed to create a ProcessSingleton",
			"Permission denied", "No such file or directory"} {
			if strings.Contains(log, reason) {
				return result, fmt.Errorf("Chrome startup: %s", reason)
			}
		}
		return result, fmt.Errorf("Chrome debugging pipe/renderer probe failed (%v). "+
			"Check executable permissions and snap confinement; use system Chrome or downloaded Chrome for Testing", callErr)
	}
	if result.Sandbox == "not yet checked" {
		result.Sandbox = "enabled (renderer probe passed)"
	}
	return result, nil
}
