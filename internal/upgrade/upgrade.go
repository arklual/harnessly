// Package upgrade is `harnessly upgrade plan|apply|rollback`: it reads a release manifest (internal/release), says
// what would change and which registered extensions the new core cannot speak to, and then replaces the programs of one
// release together, checks the result and puts the old programs back if the check fails.
//
// It touches programs only. The machine's registration, the data folders and the hub's database are never opened here:
// a program that is put back runs on the data the new one left, which is why the migrations are additive
// (docs/hub/upgrade.md).
package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/release"
)

// Target is an installed program of a component.
type Target struct {
	Component string
	Path      string
	// Optional: a missing file is installed, and a release without it is no error.
	Optional bool
}

// Extension is a registered extension: its name and the protocol it is registered with.
type Extension struct{ Name, Protocol string }

// AppCheck is what the compatibility check of one installed app (harnessly app compat) says about the release.
type AppCheck struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
	// Off: the app is switched off, so its problem is shown but does not stop apply.
	Off    bool     `json:"off,omitempty"`
	Errors []string `json:"errors,omitempty"`
	// Warnings never stop apply.
	Warnings []string `json:"warnings,omitempty"`
}

// Item is one program in the plan.
type Item struct {
	Component string `json:"component"`
	Path      string `json:"path"`
	// Action: update, install (not there yet), current (already the release's), skip (the release has no such file).
	Action string `json:"action"`
	// Have is the installed version when it could be read; Want is the release's.
	Have string `json:"have,omitempty"`
	Want string `json:"want"`

	key    string
	sha256 string
	size   int64
}

// Plan is what apply would do.
type Plan struct {
	Version    string                   `json:"version"`
	Items      []Item                   `json:"items"`
	Extensions []release.ExtensionCheck `json:"extensions"`
	// Apps is the check of the apps installed on this machine against the release, when it was asked for.
	Apps []AppCheck   `json:"apps,omitempty"`
	Data release.Data `json:"data"`
	// Blocking lists what stops apply: an incompatible extension.
	Blocking []string `json:"blocking,omitempty"`
}

// AddApps puts the check of the installed apps into the plan: an app that is on and does not fit stops apply.
func (p *Plan) AddApps(checks []AppCheck) {
	for _, a := range checks {
		p.Apps = append(p.Apps, a)
		if len(a.Errors) > 0 && !a.Off {
			p.Blocking = append(p.Blocking, fmt.Sprintf("app %s %s: %s", a.ID, a.Version, strings.Join(a.Errors, "; ")))
		}
	}
}

// Blocked says apply refuses.
func (p *Plan) Blocked() bool { return len(p.Blocking) > 0 }

// Changes says whether any program would be replaced.
func (p *Plan) Changes() bool {
	for _, it := range p.Items {
		if it.Action == "update" || it.Action == "install" {
			return true
		}
	}
	return false
}

// MakePlan compares the installed programs with the release. It changes nothing.
func MakePlan(m *release.Manifest, targets []Target, goos, goarch string, exts []Extension, probe func(Target) string) (*Plan, error) {
	p := &Plan{Version: m.Version, Data: m.Data, Items: []Item{}, Extensions: []release.ExtensionCheck{}}
	for _, t := range targets {
		c, ok := m.Components[t.Component]
		if !ok {
			if !t.Optional {
				return nil, fmt.Errorf("the release %s has no %s", m.Version, t.Component)
			}
			p.Items = append(p.Items, Item{Component: t.Component, Path: t.Path, Action: "skip"})
			continue
		}
		key := release.FileKey(t.Component, goos, goarch)
		f, ok := c.Files[key]
		if !ok {
			if !t.Optional {
				return nil, fmt.Errorf("the release %s has no %s", m.Version, key)
			}
			p.Items = append(p.Items, Item{Component: t.Component, Path: t.Path, Action: "skip", Want: c.Version})
			continue
		}
		it := Item{Component: t.Component, Path: t.Path, Want: c.Version, key: key, sha256: f.SHA256, size: f.Size}
		have, err := fileSHA(t.Path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if !t.Optional {
				return nil, fmt.Errorf("%s: %w", t.Path, err)
			}
			it.Action = "install"
		case err != nil:
			return nil, err
		case have == f.SHA256:
			it.Action = "current"
		default:
			it.Action = "update"
		}
		if probe != nil && it.Action != "install" {
			it.Have = probe(t)
		}
		p.Items = append(p.Items, it)
	}
	for _, e := range exts {
		c := m.CheckExtension(e.Name, e.Protocol)
		p.Extensions = append(p.Extensions, c)
		if c.Verdict == release.Incompatible {
			p.Blocking = append(p.Blocking, fmt.Sprintf("extension %s: %s", e.Name, c.Detail))
		}
	}
	return p, nil
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ProbeVersion asks a program for its version (`<path> version`). The runtime is not asked: it has no such command and
// a bare start would run the server.
func ProbeVersion(t Target) string {
	if t.Component == release.Runtime || t.Component == release.UI {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, t.Path, "version").Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// Options of Apply.
type Options struct {
	// Fetch opens the file of the release by its download name; the hash is checked here, not by Fetch.
	Fetch func(ctx context.Context, key string) (io.ReadCloser, error)
	// Restart makes the replaced programs run; nil means the caller does it. It also runs after a rollback.
	Restart func(ctx context.Context) error
	// Verify is the check after the update; an error rolls back.
	Verify func(ctx context.Context, replaced []Item) error
	// Force applies although the plan is blocked.
	Force bool
	// Log gets one line per step; may be nil.
	Log func(string)
}

// Result of Apply.
type Result struct {
	Replaced []string `json:"replaced"`
	// RolledBack: the check failed and the old programs are back.
	RolledBack bool `json:"rolledBack"`
}

// ErrBlocked: the plan has incompatible extensions.
var ErrBlocked = errors.New("the release does not speak the protocol of a registered extension")

const bakSuffix = ".prev"

// Apply downloads and checks every file first, then replaces them one after another, restarts, verifies, and puts the
// old files back when anything fails. A failure before the first replacement leaves the machine as it was.
func Apply(ctx context.Context, p *Plan, o Options) (*Result, error) {
	logf := func(s string) {
		if o.Log != nil {
			o.Log(s)
		}
	}
	if p.Blocked() && !o.Force {
		return nil, fmt.Errorf("%w: %s", ErrBlocked, strings.Join(p.Blocking, "; "))
	}
	var todo []Item
	for _, it := range p.Items {
		if it.Action == "update" || it.Action == "install" {
			todo = append(todo, it)
		}
	}
	res := &Result{Replaced: []string{}}
	if len(todo) == 0 {
		return res, nil
	}
	news := make([]string, len(todo))
	cleanup := func() {
		for _, n := range news {
			if n != "" {
				_ = os.Remove(n)
			}
		}
	}
	for i, it := range todo {
		n, err := download(ctx, o, it)
		if err != nil {
			cleanup()
			return nil, err
		}
		news[i] = n
		logf("downloaded " + it.key)
	}
	// Replace: the old file is renamed to <path>.prev (a running program is not cut), the new one takes its place.
	done := 0
	restore := func() {
		for i := done - 1; i >= 0; i-- {
			putBack(todo[i].Path)
		}
	}
	for i, it := range todo {
		if err := replace(news[i], it); err != nil {
			restore()
			cleanup()
			return nil, err
		}
		news[i] = ""
		done++
		res.Replaced = append(res.Replaced, it.Component)
		logf("replaced " + it.Path)
	}
	if err := afterSwap(ctx, o, todo, logf); err != nil {
		logf("check failed: " + err.Error() + "; putting the old programs back")
		restore()
		res.RolledBack = true
		if o.Restart != nil {
			if rerr := o.Restart(ctx); rerr != nil {
				return res, fmt.Errorf("%w; and restarting the old programs failed: %v", err, rerr)
			}
		}
		return res, err
	}
	return res, nil
}

func afterSwap(ctx context.Context, o Options, items []Item, logf func(string)) error {
	if o.Restart != nil {
		if err := o.Restart(ctx); err != nil {
			return fmt.Errorf("restart: %w", err)
		}
		logf("restarted")
	}
	if o.Verify != nil {
		if err := o.Verify(ctx, items); err != nil {
			return fmt.Errorf("check after the update: %w", err)
		}
		logf("check passed")
	}
	return nil
}

func replace(newPath string, it Item) error {
	_, err := os.Stat(it.Path)
	switch {
	case err == nil:
		if err := os.Rename(it.Path, it.Path+bakSuffix); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNotExist):
		_ = os.Remove(it.Path + bakSuffix) // nothing to go back to: a rollback removes the new file
	default:
		return err
	}
	if err := os.Rename(newPath, it.Path); err != nil {
		putBack(it.Path)
		return err
	}
	return nil
}

// putBack restores <path>.prev, or removes the file when there was no previous one.
func putBack(path string) {
	if _, err := os.Stat(path + bakSuffix); err == nil {
		_ = os.Rename(path+bakSuffix, path)
		return
	}
	_ = os.Remove(path)
}

func download(ctx context.Context, o Options, it Item) (string, error) {
	if o.Fetch == nil {
		return "", errors.New("no way to fetch the release files")
	}
	rc, err := o.Fetch(ctx, it.key)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp(filepath.Dir(it.Path), "."+filepath.Base(it.Path)+".new-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(rc, it.size+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != it.size {
		err = fmt.Errorf("%s: got %d bytes, the manifest says %d", it.key, n, it.size)
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != it.sha256 {
		err = fmt.Errorf("%s does not match the manifest's checksum", it.key)
	}
	if err == nil {
		err = os.Chmod(name, 0o755)
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// Rollback puts back the programs an earlier apply set aside (<path>.prev) and returns the paths restored.
func Rollback(targets []Target) ([]string, error) {
	var back []string
	for _, t := range targets {
		if _, err := os.Stat(t.Path + bakSuffix); err != nil {
			continue
		}
		if err := os.Rename(t.Path+bakSuffix, t.Path); err != nil {
			return back, err
		}
		back = append(back, t.Path)
	}
	if len(back) == 0 {
		return nil, errors.New("nothing to roll back: no " + bakSuffix + " files")
	}
	return back, nil
}
