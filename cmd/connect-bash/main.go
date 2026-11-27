// connect-bash is the agent's shell when its chat runs on a Harnessly Connect device: Claude Code is started with
// CLAUDE_CODE_SHELL pointing here (the name must hold "bash") and HARNESSLY_CONNECT_DEVICE naming the device. Without
// a device it is plain bash.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/connect"
)

func main() {
	args := os.Args[1:]
	// Claude Code checks a shell with --version before taking it.
	if len(args) == 1 && args[0] == "--version" && os.Getenv(connect.DeviceEnv) != "" {
		fmt.Println("GNU bash, version 5 (harnessly connect)")
		return
	}
	cwd, _ := os.Getwd()
	os.Exit(connect.RunBash(args, connect.BashEnv{
		Getenv: os.Getenv, Environ: os.Environ(), Cwd: cwd,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Endpoint: endpoint,
		Fallback: func(args []string) int {
			bash := "/bin/bash"
			err := syscall.Exec(bash, append([]string{"bash"}, args...), os.Environ())
			fmt.Fprintf(os.Stderr, "connect-bash: %v\n", err)
			return 127
		},
	}))
}

func readTrim(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

// endpoint finds the runtime the way the harnessly CLI does.
func endpoint() (base, token, header string, err error) {
	dir := os.Getenv("HARNESSLY_DATA_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".harnessly")
	}
	base = strings.TrimRight(os.Getenv("HARNESSLY_URL"), "/")
	if base == "" {
		port := readTrim(filepath.Join(dir, "port"))
		if port == "" {
			return "", "", "", errors.New("не найден runtime: нет " + filepath.Join(dir, "port"))
		}
		base = "http://127.0.0.1:" + port
	}
	token = os.Getenv("HARNESSLY_CLI_TOKEN")
	if f := os.Getenv("HARNESSLY_CLI_TOKEN_FILE"); token == "" && f != "" {
		token = readTrim(f)
	}
	if token == "" {
		token = readTrim(filepath.Join(dir, "cli-token"))
	}
	if token == "" {
		return "", "", "", errors.New("нет cli-token runtime")
	}
	header = os.Getenv("HARNESSLY_CLI_HEADER")
	if header == "" {
		header = "x-cli-token"
	}
	return base, token, header, nil
}
