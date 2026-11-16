package design

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/designkit/kitadd"
)

// ConnectKitPackage is the button «Подключить UI-кит» for a package: the package becomes a kit
// (kit add), its components are drawn once (render), the kit is installed on the machine and the project is pinned to
// it. source is "npm:<package>[@version]" or "shadcn:<registry>"; registry is an optional private npm registry.
func (l *Library) ConnectKitPackage(project, source, registry string) (*KitPin, error) {
	source = strings.TrimSpace(source)
	if source != "" && !strings.Contains(source, ":") {
		source = "npm:" + source // a bare name in the field is an npm package: «antd», «@scope/pkg»
	}
	src, err := kitadd.ParseSource(source)
	if err != nil {
		return nil, &KitPinError{err.Error()}
	}
	tmp, err := os.MkdirTemp("", "hx-kit-connect-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	id := kitadd.DefaultID(src)
	folder := filepath.Join(tmp, id)
	rep, err := kitadd.Add(kitadd.Options{Source: src, Folder: folder, Registry: registry, Cwd: tmp})
	if err != nil {
		return nil, &KitPinError{err.Error()}
	}
	if items, rerr := RenderKit(folder, KitRenderOptions{KitsDir: l.KitsDir()}); rerr == nil {
		failed := map[string]string{}
		for _, it := range items {
			if it.Status != "ok" {
				failed[it.ID] = "it does not render alone"
			}
		}
		ns, root := kitIdentityOf(folder)
		if err := kitadd.DropSamples(folder, ns, root, failed); err != nil {
			return nil, err
		}
	}
	var body bytes.Buffer
	if err := tarFolder(folder, &body); err != nil {
		return nil, err
	}
	if _, err := designkit.Install(l.KitsDir(), id, rep.Version, &body, designkit.Options{}); err != nil && !errors.Is(err, designkit.ErrExists) {
		return nil, &KitPinError{err.Error()}
	}
	return l.UseProjectKit(project, id+"@"+rep.Version)
}

func tarFolder(dir string, w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func kitIdentityOf(folder string) (ns, root string) {
	raw, err := os.ReadFile(filepath.Join(folder, designkit.ManifestFile))
	if err != nil {
		return "", ""
	}
	var d struct {
		Namespace string `json:"namespace"`
		Kit       struct {
			Root string `json:"root"`
		} `json:"kit"`
	}
	_ = json.Unmarshal(raw, &d)
	return d.Namespace, d.Kit.Root
}
