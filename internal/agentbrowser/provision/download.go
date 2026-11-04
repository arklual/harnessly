package provision

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// Version is pinned with the hashes below; tools/update-browser-checksums.sh downloads every archive before hashing.
const Version = "157.0.8103.38"

type archive struct{ platform, sha256, binary string }

func platform(cfg Config) (archive, error) {
	if cfg.archive != nil {
		return *cfg.archive, nil
	}
	switch cfg.GOOS + "/" + cfg.GOARCH {
	case "linux/amd64":
		return archive{"linux64", "8cdeb344e9b2cefe59f4aea8d1d93d5d93054c03cde217665c16fc1f62e72a66", "chrome-linux64/chrome"}, nil
	case "linux/arm64":
		return archive{"linux-arm64", "5517510e61657ff43ee9ab804ecd1a499b3d5bd3caf72f097516bd6565ba5a17",
			"chrome-linux-arm64/chrome"}, nil
	case "darwin/arm64":
		return archive{"mac-arm64", "3406b38b52b7d341d74450eb7b369be038c9fec9894c8b8a9e9e9f97333caf2f",
			"chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"}, nil
	case "darwin/amd64":
		return archive{"mac-x64", "c6fcc2869b0d5a421e880d7bcdabe96e4261c4100e125fc4e4daf6346474efa6",
			"chrome-mac-x64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"}, nil
	default:
		return archive{}, fmt.Errorf("no pinned Chrome for Testing build for %s/%s; install Chromium: %s; "+
			"then set RUNTIME_BROWSER_CHROME to its executable", cfg.GOOS, cfg.GOARCH, installCommand(osRelease(), cfg.GOARCH))
	}
}

type progressWriter struct {
	written, total int64
	last           int
	notify         func(Progress)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.written += int64(len(b))
	n := 0
	if p.total > 0 {
		n = int(min(90, p.written*90/p.total))
	}
	if n != p.last {
		p.last = n
		p.notify(Progress{n, fmt.Sprintf("Downloading Chrome (%d MiB); retry in 20 seconds", p.written>>20)})
	}
	return len(b), nil
}

func download(ctx context.Context, cfg Config, a archive, root string, notify func(Progress)) error {
	// A fresh staging directory prevents a partial archive from ever becoming a cached installation.
	stage, err := os.MkdirTemp(root, ".download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	downloadURL := cfg.URL
	if downloadURL == "" {
		downloadURL = "https://storage.googleapis.com/chrome-for-testing-public/" + Version + "/" + a.platform + "/chrome-" + a.platform + ".zip"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	notify(Progress{0, "Downloading Chrome; retry in 20 seconds"})
	resp, err := (&http.Client{Timeout: 15 * time.Minute}).Do(req)
	if err != nil {
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		return fmt.Errorf("download Chrome: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Chrome: HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(filepath.Join(stage, "chrome.zip"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	p := &progressWriter{total: resp.ContentLength, last: -1, notify: notify}
	const maxArchive = 1 << 30
	size, err := io.Copy(io.MultiWriter(f, hash, p), io.LimitReader(resp.Body, maxArchive+1))
	if err != nil {
		return fmt.Errorf("download Chrome interrupted: %w", err)
	}
	if size > maxArchive {
		return errors.New("Chrome archive exceeds 1 GiB")
	}
	if hex.EncodeToString(hash.Sum(nil)) != a.sha256 {
		return errors.New("Chrome archive SHA-256 mismatch; discarded download")
	}
	notify(Progress{92, "Verified Chrome SHA-256; unpacking"})
	dest := filepath.Join(stage, "unpacked")
	if err = unpack(f, size, dest); err != nil {
		return err
	}
	if !executable(filepath.Join(dest, a.binary)) {
		return errors.New("Chrome archive has no executable")
	}
	final := filepath.Join(root, Version)
	// An incomplete destination can only come from a previous implementation or manual changes.
	if err = os.RemoveAll(final); err != nil {
		return err
	}
	if err = os.Rename(dest, final); err != nil {
		return err
	}
	notify(Progress{95, "Chrome installed; checking libraries and sandbox"})
	return nil
}

func unpack(f *os.File, size int64, dest string) error {
	z, err := zip.NewReader(f, size)
	if err != nil {
		return err
	}
	var expanded uint64
	links := map[string]string{}
	for _, entry := range z.File {
		clean := filepath.Clean(filepath.FromSlash(entry.Name))
		if !filepath.IsLocal(clean) {
			return errors.New("unsafe Chrome archive path")
		}
		expanded += entry.UncompressedSize64
		if expanded > 3<<30 {
			return errors.New("Chrome archive expands beyond 3 GiB")
		}
		path := filepath.Join(dest, clean)
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(path, 0755); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		// Create framework symlinks only after all regular files, so extraction cannot traverse one.
		if entry.Mode()&os.ModeSymlink != 0 {
			in, e := entry.Open()
			if e != nil {
				return e
			}
			target, e := io.ReadAll(io.LimitReader(in, 4097))
			_ = in.Close()
			if e != nil {
				return e
			}
			value := string(target)
			if len(target) > 4096 || filepath.IsAbs(value) || !filepath.IsLocal(filepath.Join(filepath.Dir(clean), value)) {
				return errors.New("unsafe Chrome archive symlink")
			}
			links[path] = value
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("unsupported Chrome archive entry: %s", entry.Name)
		}
		if err = unpackFile(entry, path); err != nil {
			return err
		}
	}
	for path, target := range links {
		if err = os.Symlink(target, path); err != nil {
			return err
		}
	}
	return nil
}

func unpackFile(entry *zip.File, path string) error {
	in, err := entry.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	// Drop setuid/setgid bits from downloaded files; privilege changes are an administrator's explicit choice.
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode().Perm()&0777)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(in, int64(entry.UncompressedSize64)+1))
	return errors.Join(copyErr, out.Close())
}

func osRelease() string { b, _ := os.ReadFile("/etc/os-release"); return strings.TrimSpace(string(b)) }
