package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// stall is how long a download may go without a byte before it is given up.
const stall = 45 * time.Second

// ErrChecksum is a download that is not the file that was pinned.
var ErrChecksum = errors.New("checksum mismatch")

// fileSum is the SHA-256 of a file, "" when it cannot be read.
func fileSum(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// fetchBytes reads a small text file (a checksum list).
func fetchBytes(ctx context.Context, c *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// download fetches url to dest through a .part file, checks the SHA-256 (want, hex) and moves it into place with
// mode. progress gets the bytes so far and the total (0 when unknown).
func download(ctx context.Context, c *http.Client, url, dest, want string, mode os.FileMode, progress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	part := dest + ".part"
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer os.Remove(part)
	h := sha256.New()
	timer := time.AfterFunc(stall, cancel)
	defer timer.Stop()
	buf := make([]byte, 128<<10)
	var done int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			timer.Reset(stall)
			h.Write(buf[:n])
			if _, werr := out.Write(buf[:n]); werr != nil {
				_ = out.Close()
				return werr
			}
			done += int64(n)
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = out.Close()
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("%s: %w", url, ErrChecksum)
	}
	return os.Rename(part, dest)
}

// sumOf finds the hex SHA-256 of name in a `sha256sum` list.
func sumOf(list []byte, name string) string {
	for _, line := range strings.Split(string(list), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0])
		}
	}
	return ""
}
