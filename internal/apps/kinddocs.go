package apps

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// The documents of the types apps add: which project is a document of which type. The project is the library's, made by
// the host; this file only says what it is for, so the library's list leaves it out of the sites, decks and videos and
// the Document API tells its type. It lives in the apps' folder, which the agent's tools do not write (policy.go).

var kindDocsMu sync.Mutex

func (r *Registry) kindDocsFile() string { return filepath.Join(r.Root, "kind-docs.json") }

func (r *Registry) readKindDocs() (map[string]string, error) {
	out := map[string]string{}
	raw, err := os.ReadFile(r.kindDocsFile())
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DocKinds are the documents of the apps' types: project id → type.
func (r *Registry) DocKinds() map[string]string {
	kindDocsMu.Lock()
	defer kindDocsMu.Unlock()
	m, err := r.readKindDocs()
	if err != nil {
		return map[string]string{}
	}
	return m
}

// DocKind is the type of a project that is a document of an app's type.
func (r *Registry) DocKind(project string) (string, bool) {
	k, ok := r.DocKinds()[project]
	return k, ok
}

// SetDocKind records that a project is a document of an app's type.
func (r *Registry) SetDocKind(project, kind string) error {
	kindDocsMu.Lock()
	defer kindDocsMu.Unlock()
	m, err := r.readKindDocs()
	if err != nil {
		return err
	}
	m[project] = kind
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(r.kindDocsFile(), raw)
}

// ProtectedDirs are the folders no app and no tool of the agent writes: the registry, and the package folder and the source
// folder of every installed app (a dev install points at its author's folder), whichever app owns them. The launch's
// guard and the files bridge both take this set, so they cannot drift; users of it compare in both the written and the
// resolved form (protectedForms).
func (r *Registry) ProtectedDirs() ([]string, error) {
	list, err := r.ListAll()
	if err != nil {
		return nil, err
	}
	out := []string{r.Root}
	for _, a := range list {
		for _, d := range []string{a.Dir, a.Source} {
			if d != "" && filepath.IsAbs(d) && !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	return out, nil
}
