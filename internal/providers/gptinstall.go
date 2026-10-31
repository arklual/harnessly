package providers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// CLIProxyAPI (github.com/router-for-me/CLIProxyAPI, MIT) is the proxy a GPT chat of Claude Code runs on: it takes
// Anthropic's Messages API and sends the ChatGPT backend's Responses requests. The runtime downloads one release of it,
// pinned here with the digest of each archive, the first time the switch goes on; a new release is a change of this
// file. Linux takes the builds without plugins: static, and the proxy's plugins stay off anyway.
const (
	ProxyVersion = "8.0.48"
	proxyRelease = "https://github.com/router-for-me/CLIProxyAPI/releases/download/v" + ProxyVersion + "/"
	proxyBinName = "cli-proxy-api"
)

// proxyAsset is the archive of one system and its sha256.
type proxyAsset struct{ name, sha256 string }

var proxyAssets = map[string]proxyAsset{
	"linux/amd64":  {"CLIProxyAPI_8.0.48_linux_amd64_no-plugin.tar.gz", "fb3bada1cdd05a8ef84b9f3b777c1019092c87def6405d057fbe1b2be9def22f"},
	"linux/arm64":  {"CLIProxyAPI_8.0.48_linux_aarch64_no-plugin.tar.gz", "17685b2a186faa7c8726c6ac90150a08a91d60d936ce8fafe5e8a11c1055e9c0"},
	"darwin/amd64": {"CLIProxyAPI_8.0.48_darwin_amd64.tar.gz", "97f662d40bc3da1bd6bb0f9188ba803d11e0cb29dfc037376bb655bf3f437781"},
	"darwin/arm64": {"CLIProxyAPI_8.0.48_darwin_aarch64.tar.gz", "543bdbc6f8384fecd033bf7398236e46279962a80dbc909cf8d8e6ee94ccfc04"},
}

// proxyArchiveMax bounds a download: the archives are about 25 MB.
const proxyArchiveMax = 128 << 20

// ErrNoProxyBuild: CLIProxyAPI has no build for this system.
var ErrNoProxyBuild = errors.New("providers: no CLIProxyAPI build for this system")

// ProxyBin is where the pinned release's binary is kept under dir.
func ProxyBin(dir string) string {
	return filepath.Join(dir, "bin", proxyBinName+"-"+ProxyVersion)
}

// InstallProxy puts the pinned release's binary under dir unless it is there: the archive of this system is
// downloaded with client (nil is one of ProxyFromEnvironment that gives up after two minutes), checked against its
// digest and unpacked. A failed download leaves nothing behind.
func InstallProxy(ctx context.Context, dir string, client *http.Client) (string, error) {
	bin := ProxyBin(dir)
	if fi, err := os.Stat(bin); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o100 != 0 {
		return bin, nil
	}
	asset, ok := proxyAssets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", ErrNoProxyBuild
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		return "", err
	}
	return bin, fetchProxy(ctx, client, proxyRelease+asset.name, asset.sha256, bin)
}

func fetchProxy(ctx context.Context, client *http.Client, url, sum, bin string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("providers: download of CLIProxyAPI answered %d", res.StatusCode)
	}
	archive, err := os.CreateTemp(filepath.Dir(bin), "download-*")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(archive, h), io.LimitReader(res.Body, proxyArchiveMax+1))
	if err != nil {
		return err
	}
	if n > proxyArchiveMax {
		return errors.New("providers: the CLIProxyAPI archive is too large")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return fmt.Errorf("providers: the CLIProxyAPI archive has digest %s, not the pinned one", got)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return unpackProxy(archive, bin)
}

// unpackProxy writes the archive's binary to bin, under a temporary name first.
func unpackProxy(r io.Reader, bin string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("providers: no " + proxyBinName + " in the CLIProxyAPI archive")
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != proxyBinName {
			continue
		}
		tmp := bin + ".tmp"
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o700)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, io.LimitReader(tr, proxyArchiveMax)); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, bin); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	}
}
