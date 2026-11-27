package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/connect"
)

type route struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Dir    string `json:"dir"`
	Cwd    string `json:"cwd"`
}

// dispatch is the Claude launcher: the chat of this tmux session runs on its device, or here as plain claude.
func dispatch(args []string) int {
	self, _ := os.Executable()
	real := realClaude(self)
	if real == "" {
		return fail(fmt.Errorf("claude не найден в PATH"))
	}
	r, err := lookup()
	if err != nil {
		// The runtime cannot say: the chat starts here, as it would without Connect.
		fmt.Fprintln(os.Stderr, "harnessly connect:", err)
	}
	if err != nil || r.Device == "" {
		err := syscall.Exec(real, append([]string{"claude"}, args...), os.Environ())
		return fail(err)
	}
	if hint := hintFile(r); hint != "" {
		args = append([]string{"--append-system-prompt-file", hint}, args...)
	}
	// The process keeps the name claude: the runtime tells a pane's agent by its process's name.
	argv := append([]string{"claude", "--device", r.Device, "--dir", r.Dir, "--cwd", r.Cwd, "--", real}, args...)
	return fail(syscall.Exec(self, argv, os.Environ()))
}

// realClaude is the claude on PATH that is not this launcher.
func realClaude(self string) string {
	me, _ := os.Stat(self)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, "claude")
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || st.Mode()&0o111 == 0 || me != nil && os.SameFile(me, st) {
			continue
		}
		if t, err := filepath.EvalSymlinks(p); err == nil && filepath.Base(t) == "connect-chat" {
			continue
		}
		return p
	}
	return ""
}

func tmuxSession() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func lookup() (route, error) {
	var r route
	q := url.Values{}
	if s := tmuxSession(); s != "" {
		q.Set("session", s)
	} else if wd, err := os.Getwd(); err == nil {
		q.Set("cwd", wd)
	}
	base, token, header, err := connect.Endpoint(os.Getenv)
	if err != nil {
		return r, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/cli/connect/route?"+q.Encode(), nil)
	req.Header.Set(header, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("runtime: %s", resp.Status)
	}
	err = json.NewDecoder(resp.Body).Decode(&r)
	if r.Cwd == "" {
		r.Cwd = r.Dir
	}
	return r, err
}

// hintFile tells the agent where it works; on Windows also how the paths look. The file is the device's own.
func hintFile(r route) string {
	text := fmt.Sprintf("You work on the device «%s» through Harnessly Connect: the files of %s and every Bash "+
		"command are on that device, not on the server this session runs on. Tools you install with Bash are "+
		"installed there.\n", r.Name, r.Dir)
	if r.OS == "windows" {
		text += "The device runs Windows; your shell is Git Bash there. Paths are POSIX: /c/... is C:\\..., and every " +
			"path you read or write keeps that form. Run .exe/.cmd/.bat programs directly; PowerShell through " +
			"`powershell -NoProfile -Command \"...\"`. Paths a Windows program prints as C:\\... are rewritten to " +
			"/c/... for you. Files may have CRLF line endings: keep a file's own.\n"
	}
	data := os.Getenv("HARNESSLY_DATA_DIR")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".harnessly")
	}
	p := filepath.Join(data, "connect", "prompts", r.Device+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return ""
	}
	if os.WriteFile(p, []byte(text), 0o600) != nil {
		return ""
	}
	return p
}
