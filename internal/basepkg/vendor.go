package basepkg

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// IsSnap says the binary at path is a snap: a link into /snap, snap itself, or a script that only starts one (Ubuntu's
// chromium-browser package puts such a script in /usr/bin). The same rule as internal/agentbrowser/provision.
func IsSnap(path string) bool {
	resolved, _ := filepath.EvalSymlinks(path)
	if strings.HasPrefix(path, "/snap/") || strings.HasPrefix(resolved, "/snap/") || resolved == "/usr/bin/snap" {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head, _ := io.ReadAll(io.LimitReader(f, 4096))
	return snapScript(head)
}

func snapScript(head []byte) bool {
	return bytes.HasPrefix(head, []byte("#!")) && (bytes.Contains(head, []byte("/snap/bin/")) ||
		bytes.Contains(head, []byte("snap run ")))
}

// OSRelease reads the ID of /etc/os-release ("ubuntu", "debian", …); empty when there is none.
func OSRelease(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "ID="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

// DebArch is the Debian name of a Go architecture.
func DebArch(goarch string) string {
	switch goarch {
	case "386":
		return "i386"
	case "arm":
		return "armhf"
	}
	return goarch
}

// AddAptSource puts the vendor's key in /usr/share/keyrings and its deb822 file in /etc/apt/sources.list.d. The key
// comes over https from the vendor's own address, the way the vendor's install instructions take it; apt then checks
// every package by it.
func AddAptSource(ctx context.Context, src AptSource, proxy string) error {
	armored, err := fetch(ctx, src.KeyURL, "")
	if err != nil && proxy != "" {
		armored, err = fetch(ctx, src.KeyURL, proxy)
	}
	if err != nil {
		return err
	}
	key, err := Dearmor(armored)
	if err != nil {
		return fmt.Errorf("ключ %s: %w", src.KeyURL, err)
	}
	if err := writeFile(src.KeyFile(), key); err != nil {
		return err
	}
	return writeFile(src.SourcesFile(), []byte(src.Sources()))
}

func fetch(ctx context.Context, raw, proxy string) ([]byte, error) {
	tr := &http.Transport{}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(u)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", raw, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Dearmor turns an ASCII-armored OpenPGP key into the binary keyring apt reads (what `gpg --dearmor` does). Several
// armored blocks are joined.
func Dearmor(armored []byte) ([]byte, error) {
	var out []byte
	in, body, headers := false, strings.Builder{}, false
	for _, line := range strings.Split(strings.ReplaceAll(string(armored), "\r", ""), "\n") {
		switch {
		case strings.HasPrefix(line, "-----BEGIN PGP PUBLIC KEY BLOCK-----"):
			in, headers = true, true
			body.Reset()
		case strings.HasPrefix(line, "-----END PGP PUBLIC KEY BLOCK-----"):
			if !in {
				return nil, errors.New("конец блока без начала")
			}
			b, err := base64.StdEncoding.DecodeString(body.String())
			if err != nil {
				return nil, err
			}
			out = append(out, b...)
			in = false
		case !in:
		case headers:
			// Armor headers ("Version: …") end at the first empty line; a block without headers starts at once.
			if strings.TrimSpace(line) == "" {
				headers = false
			} else if !strings.Contains(line, ": ") {
				headers = false
				body.WriteString(strings.TrimSpace(line))
			}
		case strings.HasPrefix(line, "="):
			// The CRC24 line.
		default:
			body.WriteString(strings.TrimSpace(line))
		}
	}
	if len(out) == 0 {
		return nil, errors.New("в ответе нет ключа OpenPGP")
	}
	return out, nil
}

// writeFile writes 0644 through a temporary file, so apt never reads half a file.
func writeFile(path string, data []byte) error {
	tmp := path + ".harnessly.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
