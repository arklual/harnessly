package apps

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// What the core keeps of an extension beyond its settings form and secrets, which HRN-645 keeps (setup.go, the
// safe's app/<id>/<name>): the version of its own data and the way the values of a secret reach a command the person
// runs for it. The values of a secret are never returned to a page or to the extension's views.

// RevealSecrets opens the values of names for one process of the app: each has to be declared in the manifest and
// granted by its secrets:<name> permission. The values go to the process (harnessly app run) and nowhere else.
func RevealSecrets(safe *secrets.Store, m *Manifest, names []string) (map[string]string, error) {
	for _, n := range names {
		if !m.hasSecret(n) {
			return nil, ErrSecretUndeclared
		}
		if !contains(m.Permissions, "secrets:"+n) {
			return nil, ErrSecretRight
		}
	}
	return safe.RevealApp(m.ID, names)
}

// ---- the app's data ----

const dataMetaFile = "_data.json"

// DataVersion is the version the app's stored data is at; 0 when the app has stored none.
func (s *Storage) DataVersion(app string) int {
	raw, err := os.ReadFile(filepath.Join(s.Root, app, dataMetaFile))
	if err != nil {
		return 0
	}
	var meta struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(raw, &meta)
	return meta.Version
}

func (s *Storage) setDataVersion(app string, v int) error {
	dir := filepath.Join(s.Root, app)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, dataMetaFile), []byte(`{"version":`+strconv.Itoa(v)+`}`))
}

// areas lists the area files of an app.
func (s *Storage) areas(app string) []string {
	var out []string
	_ = filepath.WalkDir(filepath.Join(s.Root, app), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".json") && d.Name() != dataMetaFile {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// MigrateData brings the app's stored data to the version the package declares: every area goes through the steps in
// turn, all in memory first, and nothing is written when one of them fails. Data of an app with no earlier version is
// only stamped. A package that declares no data version leaves the data alone.
func (s *Storage) MigrateData(p *Package) (from, to int, err error) {
	d := p.Manifest.Data
	if d == nil {
		return 0, 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	from = s.DataVersion(p.Manifest.ID)
	to = d.Version
	files := s.areas(p.Manifest.ID)
	if from == 0 && len(files) > 0 {
		from = 1 // data written before the app declared a version is the first
	}
	switch {
	case from == to:
		return from, to, nil
	case from == 0 || len(files) == 0:
		return from, to, s.setDataVersion(p.Manifest.ID, to)
	case from > to:
		return from, to, fmt.Errorf("%w: the data is at version %d, this package is at %d", ErrMigration, from, to)
	}
	next := make(map[string][]byte, len(files))
	for _, f := range files {
		doc, err := os.ReadFile(f)
		if err != nil {
			return from, to, err
		}
		cur := json.RawMessage(doc)
		budget := newPatchBudget(len(doc))
		for v := from; v < to; v++ {
			raw, err := p.File(d.Migrations[strconv.Itoa(v)])
			if err != nil {
				return from, to, fmt.Errorf("%w: step %d: %v", ErrMigration, v, err)
			}
			ops, err := ParsePatch(raw)
			if err != nil {
				return from, to, fmt.Errorf("%w: step %d: %v", ErrMigration, v, err)
			}
			if cur, err = applyPatchWith(cur, ops, budget); err != nil {
				return from, to, fmt.Errorf("%w: step %d: %v", ErrMigration, v, err)
			}
		}
		next[f] = cur
	}
	for f, doc := range next {
		if err := writeFileAtomic(f, doc); err != nil {
			return from, to, err
		}
	}
	return from, to, s.setDataVersion(p.Manifest.ID, to)
}

// Purge deletes everything the app stored.
func (s *Storage) Purge(app string) error {
	if !idRe.MatchString(app) {
		return ErrStorageArgs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(filepath.Join(s.Root, app))
}

// PutSecret writes one secret the manifest declares into the safe (app/<id>/<name>); the page and the CLI never
// read it back.
func PutSecret(safe *secrets.Store, m *Manifest, name, value string) error {
	if !m.hasSecret(name) {
		return ErrSecretUndeclared
	}
	return safe.PutApp(m.ID, name, value)
}

// RemoveSecret forgets one declared secret; a secret that was never set is not an error.
func RemoveSecret(safe *secrets.Store, m *Manifest, name string) error {
	if !m.hasSecret(name) {
		return ErrSecretUndeclared
	}
	if err := safe.RemoveApp(m.ID, name); err != nil && secrets.ReasonOf(err) != secrets.ReasonNotFound {
		return err
	}
	return nil
}
