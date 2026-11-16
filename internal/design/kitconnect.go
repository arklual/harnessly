package design

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/designkit/kitadd"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// ConnectKitPackage is the button «Подключить UI-кит» for a package: the package becomes a kit
// (kit add), its components are drawn once (render), the kit is installed on the machine and the project is pinned to
// it. source is "npm:<package>[@version]" or "shadcn:<registry>"; registry is an optional private npm registry.
func (l *Library) ConnectKitPackage(project, source, registry string) (*KitPin, error) {
	// Before npm runs: the project is a real one of this library (a gate lets a value that is not a project id through).
	if !IsLibraryID(project) || l.ReadModel(project) == nil {
		return nil, &KitPinError{locale.Pick("The project does not exist", "Такого проекта нет")}
	}
	source = strings.TrimSpace(source)
	if source != "" && !strings.Contains(source, ":") {
		source = "npm:" + source // a bare name in the field is an npm package: «antd», «@scope/pkg»
	}
	src, err := kitadd.ParseSource(source)
	if err != nil {
		return nil, &KitPinError{err.Error()}
	}
	// What a request names is checked before npm sees it: a plain package (never a path, git, file or address) and a
	// registry the machine allows.
	switch src.Kind {
	case "npm":
		spec := src.Package
		if src.Version != "" {
			spec += "@" + src.Version
		}
		if !kitadd.SafeNpmSpec(spec) {
			return nil, &KitPinError{locale.Pick("The package is not a plain npm name[@version]", "Пакет — не обычное имя npm[@версия]")}
		}
	case "shadcn":
		if src.Base != "" {
			if err := checkKitRegistry(src.Base); err != nil {
				return nil, err
			}
		}
	}
	if err := checkKitRegistry(registry); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "hx-kit-connect-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	id := kitadd.DefaultID(src)
	folder := filepath.Join(tmp, id)
	// A package an HTTP request names is somebody's code: it runs in a sandbox (no home, no tokens, no network but the
	// install's), from a build folder of its own, and is installed from a lock file whose every address was checked.
	// A machine that cannot make the sandbox does not connect it.
	if !kitadd.SandboxAvailable() {
		return nil, &KitPinError{locale.Pick("This machine cannot run a package in a sandbox (Landlock is off), so it is not connected from the button. Add it with the command: harnessly design kit add",
			"На этой машине нельзя запустить пакет в песочнице (Landlock выключен), поэтому по кнопке он не подключается. Добавьте его командой: harnessly design kit add")}
	}
	rep, err := kitadd.Add(kitadd.Options{Source: src, Folder: folder, Registry: registry, Cwd: tmp, NoInstallScripts: true,
		Sandbox: true, WorkDir: filepath.Join(tmp, "build"), AllowedHosts: slices.Collect(maps.Keys(AllowedKitRegistryHosts())), SandboxHelper: kitSandboxHelper})
	if err != nil {
		return nil, &KitPinError{err.Error()}
	}
	if items, rerr := RenderKit(folder, KitRenderOptions{KitsDir: l.KitsDir(), Offline: true}); rerr == nil {
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

// kitSandboxHelper is the helper's command line; empty runs this binary again (the runtime and the CLI both answer it).
// Tests set it to their own binary.
var kitSandboxHelper []string

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
