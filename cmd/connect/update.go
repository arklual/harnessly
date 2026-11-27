package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Set by the release build (-X): the GitHub repository whose connect-v* releases Connect updates from, and the public
// key their checksums.txt is signed with. Without a key (a build of one's own) Connect never updates itself.
var (
	updateRepo = "arklual/harnessly"
	updateKey  = ""
)

const updateEvery = 6 * time.Hour

type release struct {
	Tag    string `json:"tag_name"`
	Draft  bool   `json:"draft"`
	Pre    bool   `json:"prerelease"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// newer says tag (connect-v1.2.3) is a later version than have (1.2.0).
func newer(tag, have string) bool {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimPrefix(v, "connect-"), "v")
		v, _, _ = strings.Cut(v, "-")
		var out []int
		for _, p := range strings.Split(v, ".") {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil
			}
			out = append(out, n)
		}
		return out
	}
	a, b := parse(tag), parse(have)
	if a == nil || b == nil {
		return false
	}
	for i := 0; i < max(len(a), len(b)); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func assetName() string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("harnessly-connect_%s_%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

// updater checks for a newer Connect now and then and puts it in place when no agent command is running; the new
// one starts in place of this one.
func (a *app) updater(ctx context.Context) {
	if updateKey == "" || version == "dev" {
		return
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := a.updateOnce(ctx); err != nil {
			a.log.Printf("обновление: %v", err)
		}
		t.Reset(updateEvery)
	}
}

func (a *app) updateOnce(ctx context.Context) error {
	rel, err := latest(ctx)
	if err != nil || rel == nil {
		return err
	}
	if _, _, _, _, _, _, n := a.state(); n > 0 {
		return nil // the next check: no agent command is cut off by a restart
	}
	bin, err := download(ctx, rel)
	if err != nil {
		return err
	}
	a.log.Printf("обновление до %s", rel.Tag)
	if err := replaceSelf(bin); err != nil {
		return err
	}
	a.stop()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

func get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "harnessly-connect/"+version)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func latest(ctx context.Context) (*release, error) {
	raw, err := get(ctx, "https://api.github.com/repos/"+updateRepo+"/releases?per_page=30", 4<<20)
	if err != nil {
		return nil, err
	}
	var list []release
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var best *release
	for i := range list {
		r := &list[i]
		if r.Draft || r.Pre || !strings.HasPrefix(r.Tag, "connect-v") || !newer(r.Tag, version) {
			continue
		}
		if best == nil || newer(r.Tag, best.Tag) {
			best = r
		}
	}
	return best, nil
}

// download fetches the release's archive for this system, checks it against the signed checksums and gives the
// binary inside.
func download(ctx context.Context, rel *release) ([]byte, error) {
	urls := map[string]string{}
	for _, as := range rel.Assets {
		urls[as.Name] = as.URL
	}
	name := assetName()
	if urls[name] == "" || urls["checksums.txt"] == "" || urls["checksums.txt.sig"] == "" {
		return nil, fmt.Errorf("%s: нет %s, checksums.txt или подписи", rel.Tag, name)
	}
	sums, err := get(ctx, urls["checksums.txt"], 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := get(ctx, urls["checksums.txt.sig"], 4096)
	if err != nil {
		return nil, err
	}
	if err := verify(sums, sig, updateKey); err != nil {
		return nil, err
	}
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == name {
			want = f[0]
		}
	}
	if want == "" {
		return nil, fmt.Errorf("%s нет в checksums.txt", name)
	}
	arc, err := get(ctx, urls[name], 200<<20)
	if err != nil {
		return nil, err
	}
	if h := sha256.Sum256(arc); hex.EncodeToString(h[:]) != want {
		return nil, errors.New("архив не совпадает с checksums.txt")
	}
	return extract(arc, name)
}

// verify checks checksums.txt against its signature by the release key.
func verify(sums, sig []byte, key string) error {
	pub, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("ключ обновлений испорчен")
	}
	s, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(pub, sums, s) {
		return errors.New("подпись checksums.txt не сходится: обновление отклонено")
	}
	return nil
}

func extract(arc []byte, name string) ([]byte, error) {
	want := "connect"
	if runtime.GOOS == "windows" {
		want = "connect.exe"
	}
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(arc), int64(len(arc)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, 200<<20))
			}
		}
		return nil, errors.New("в архиве нет " + want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(arc))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, errors.New("в архиве нет " + want)
		}
		if filepath.Base(h.Name) == want && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}

// replaceSelf puts the new binary where this one is. A running file is renamed away first (Windows allows renaming it).
func replaceSelf(bin []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	return nil
}
