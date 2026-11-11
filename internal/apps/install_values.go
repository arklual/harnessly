package apps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// An app's package may name an address the installation gives: ${install:<name>} at the start of a url of .mcp.json,
// as a host of network.frames, and at the start of a view's entry. The hub hands the values to its machines with the
// environment's app items (boardsUrl from HUB_BOARDS_PUBLIC_URL, artifactsUrl from HUB_ARTIFACTS_PUBLIC_URL), and the
// machine puts them in when it installs the app. A placeholder left without a value makes the app «не настроено»: it is not listed,
// its views do not open and its servers are not started, and nothing of it is an error.

var installPlaceholder = regexp.MustCompile(`\$\{install:([A-Za-z][A-Za-z0-9]{0,63})\}`)

// installFiles are the files of a package the values are put into.
var installFiles = []string{ManifestPath, ".mcp.json"}

// installAt says whether s starts with a placeholder of the installation's address (and is nothing but it when whole).
func installAt(s string, whole bool) bool {
	loc := installPlaceholder.FindStringIndex(s)
	if loc == nil || loc[0] != 0 {
		return false
	}
	return !whole || loc[1] == len(s)
}

// InstallNames are the names a text asks the installation for, each once, sorted.
func InstallNames(s string) []string {
	var out []string
	for _, m := range installPlaceholder.FindAllStringSubmatch(s, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// FillInstall puts the installation's values into a JSON text; a name without a value stays as written and is
// reported. A value goes in as the inside of a JSON string.
func FillInstall(s string, values map[string]string) (string, []string) {
	var missing []string
	out := installPlaceholder.ReplaceAllStringFunc(s, func(ph string) string {
		name := installPlaceholder.FindStringSubmatch(ph)[1]
		v, ok := values[name]
		if !ok || v == "" {
			if !slices.Contains(missing, name) {
				missing = append(missing, name)
			}
			return ph
		}
		q, _ := json.Marshal(v)
		return string(q[1 : len(q)-1])
	})
	sort.Strings(missing)
	return out, missing
}

// packageInstallNames are the names the package in dir asks the installation for.
func packageInstallNames(dir string) []string {
	var all []string
	for _, f := range installFiles {
		raw, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		for _, n := range InstallNames(string(raw)) {
			if !slices.Contains(all, n) {
				all = append(all, n)
			}
		}
	}
	sort.Strings(all)
	return all
}

// fillInstallFiles puts the values into the package copied to dir and reports the names that had none.
func fillInstallFiles(dir string, values map[string]string) ([]string, error) {
	var missing []string
	for _, f := range installFiles {
		path := filepath.Join(dir, f)
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out, miss := FillInstall(string(raw), values)
		for _, n := range miss {
			if !slices.Contains(missing, n) {
				missing = append(missing, n)
			}
		}
		if out == string(raw) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(out), info.Mode().Perm()); err != nil {
			return nil, err
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// installSum is the part of an install's sum the values the package uses make: a new address installs it anew.
func installSum(names []string, values map[string]string) string {
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n + "=" + values[n] + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

// Unconfigured says whether the app still waits for an address of the installation: a placeholder is left in its
// manifest or in its .mcp.json.
func (a Installed) Unconfigured() bool {
	if a.Manifest != nil {
		if n := a.Manifest.Network; n != nil && slices.ContainsFunc(n.Frames, func(h string) bool { return installPlaceholder.MatchString(h) }) {
			return true
		}
		if slices.ContainsFunc(a.Manifest.Views, func(v View) bool { return installPlaceholder.MatchString(v.Entry) }) {
			return true
		}
	}
	if a.Dir == "" {
		return false
	}
	if pkg, _ := CachedPackage(a.Dir); pkg != nil {
		for _, s := range pkg.MCP {
			if installPlaceholder.MatchString(s.URL) {
				return true
			}
		}
	}
	return false
}
