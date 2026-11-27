package connect

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func readTrim(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

// Endpoint finds the runtime of this machine the way the harnessly CLI does: its base URL, cli-token and header.
func Endpoint(getenv func(string) string) (base, token, header string, err error) {
	dir := getenv("HARNESSLY_DATA_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".harnessly")
	}
	base = strings.TrimRight(getenv("HARNESSLY_URL"), "/")
	if base == "" {
		port := readTrim(filepath.Join(dir, "port"))
		if port == "" {
			return "", "", "", errors.New("не найден runtime: нет " + filepath.Join(dir, "port"))
		}
		base = "http://127.0.0.1:" + port
	}
	token = getenv("HARNESSLY_CLI_TOKEN")
	if f := getenv("HARNESSLY_CLI_TOKEN_FILE"); token == "" && f != "" {
		token = readTrim(f)
	}
	if token == "" {
		token = readTrim(filepath.Join(dir, "cli-token"))
	}
	if token == "" {
		return "", "", "", errors.New("нет cli-token runtime")
	}
	header = getenv("HARNESSLY_CLI_HEADER")
	if header == "" {
		header = "x-cli-token"
	}
	return base, token, header, nil
}
