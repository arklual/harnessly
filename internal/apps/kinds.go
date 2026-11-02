package apps

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The document types an app adds to Sinopia (the manifest's sinopia.kinds).
//
//   - The host owns the project: its folder, meta/project.json, the history, the claims, the links, the comments and the
//     rights. A project of an app's type is made by the host (CreateKindDocument in cmd/runtime) and carries the type's
//     name; it is never listed as a site, a deck or a video.
//   - The app owns the editor, which is one of its views with the target sinopia.<type>, and the files of its format. It
//     reads and writes the document through the SDK (hl.sinopia, hl.storage.doc) like any other app in a document.
//   - The type is named like the app that adds it: the app's id, or the id and up to two more segments, so a name is the
//     app's alone and two apps cannot add one type. Without the app the document is there to read and says it needs it.
//   - A format changes in versions. schemaVersion is the current one; migrations["1"] is the JSON Patch that takes a
//     document from version 1 to 2, and a step is there for every version before the current (migrate.go).

// SinopiaKind is a document type an app adds.
type SinopiaKind struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Icon is a file of the package (an image or an SVG).
	Icon string `json:"icon,omitempty"`
	// Editor is the id of the view that edits a document of this type.
	Editor string `json:"editor"`
	// Schema is the package file that describes the format: a JSON Schema.
	Schema        string `json:"schema,omitempty"`
	SchemaVersion int    `json:"schemaVersion"`
	// Migrations name a JSON Patch file for each version before the current one, by the version it starts from.
	Migrations map[string]string `json:"migrations,omitempty"`
	// DataKey is the key of hl.storage.doc that holds the document; the host migrates the value under it when a document of
	// an older version is opened. It is required when there are migrations.
	DataKey string `json:"dataKey,omitempty"`
	// Guest is read so that it can be refused with its reason (not supported yet).
	Guest *SinopiaKindGuest `json:"guest,omitempty"`
}

// SinopiaKindGuest is the static render a guest sees.
type SinopiaKindGuest struct {
	Render string `json:"render"`
}

// MaxKindsPerApp bounds the types one app adds.
const MaxKindsPerApp = 8

// ErrKindTaken: another installed app already adds this document type.
var ErrKindTaken = errors.New("apps: another installed app adds this document type")

// ownsKind says whether a type's name is the app's own: the app's id, or the id and further segments.
func ownsKind(appID, kind string) bool { return kind == appID || strings.HasPrefix(kind, appID+".") }

// checkKinds is the part of the manifest check about sinopia.kinds.
func (m *Manifest) checkKinds(s Sinopia, targets map[string]bool, add func(path, format string, a ...any)) {
	if len(s.Kinds) == 0 {
		return
	}
	if len(s.Kinds) > MaxKindsPerApp {
		add("sinopia.kinds", "at most %d document types", MaxKindsPerApp)
	}
	if !contains(m.Permissions, "sinopia.kind") {
		add("sinopia.kinds", "adding a document type needs the permission sinopia.kind")
	}
	seen := map[string]bool{}
	for i, k := range s.Kinds {
		path := fmt.Sprintf("sinopia.kinds[%d]", i)
		switch {
		case !appKindRe.MatchString(k.ID):
			add(path+".id", "%q: a document type is named like the app, e.g. %s or %s.board", k.ID, m.ID, m.ID)
		case !ownsKind(m.ID, k.ID):
			add(path+".id", "%q is not the app's own name: it starts with %s", k.ID, m.ID)
		case seen[k.ID]:
			add(path+".id", "%q is used twice", k.ID)
		}
		seen[k.ID] = true
		if strings.TrimSpace(k.Title) == "" {
			add(path+".title", "is required: the name in the list of new documents")
		}
		if k.ID != "" && !targets["sinopia."+k.ID] {
			add(path, "the app has no target sinopia.%s: add it to targets, a type is edited in the app's own view", k.ID)
		}
		switch v := m.view(k.Editor); {
		case k.Editor == "":
			add(path+".editor", "is required: the view that edits the document")
		case v == nil:
			add(path+".editor", "there is no view %q", k.Editor)
		case !contains(v.Targets, "sinopia."+k.ID):
			add(path+".editor", "the view %q has no target sinopia.%s", k.Editor, k.ID)
		}
		if k.SchemaVersion < 1 || k.SchemaVersion > 1_000 {
			add(path+".schemaVersion", "is required: the version of the format, from 1")
		}
		for from, file := range k.Migrations {
			n, err := strconv.Atoi(from)
			switch {
			case err != nil || n < 1 || strconv.Itoa(n) != from:
				add(path+".migrations", "%q is not a version: a step is named by the version it starts from (1, 2, …)", from)
			case n >= k.SchemaVersion:
				add(path+".migrations."+from, "the format is at version %d: nothing to migrate from %d", k.SchemaVersion, n)
			case file == "":
				add(path+".migrations."+from, "is a JSON Patch file of the package")
			}
		}
		for n := 1; n < k.SchemaVersion && n < 1_000; n++ {
			if k.Migrations[strconv.Itoa(n)] == "" {
				add(path+".migrations", "no step from version %d: a document of that version could not be opened", n)
				break
			}
		}
		switch {
		case k.DataKey != "" && (strings.HasPrefix(k.DataKey, "@") || len(k.DataKey) > 200):
			add(path+".dataKey", "is a key of hl.storage.doc, up to 200 characters, not starting with @")
		case k.DataKey == "" && len(k.Migrations) > 0:
			add(path+".dataKey", "is required with migrations: the key of hl.storage.doc that holds the document")
		}
		// The static view a guest sees is not built: a field that would promise it is refused rather than ignored.
		if k.Guest != nil {
			add(path+".guest", "is not supported yet: remove it")
		}
	}
}

// KindInfo is a document type as the page lists it.
type KindInfo struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Icon          string `json:"icon,omitempty"`
	App           string `json:"app"`
	AppName       string `json:"appName"`
	Editor        string `json:"editor"`
	SchemaVersion int    `json:"schemaVersion"`
}

// Kinds are the document types of the apps that are on for the person, by id.
func (r *Registry) Kinds() ([]KindInfo, error) {
	list, err := r.List()
	if err != nil {
		return nil, err
	}
	return kindsOf(list), nil
}

func kindsOf(list []Installed) []KindInfo {
	out := []KindInfo{}
	for _, a := range list {
		if a.Manifest == nil {
			continue
		}
		s, err := a.Manifest.SinopiaOf()
		if err != nil {
			continue
		}
		for _, k := range s.Kinds {
			out = append(out, KindInfo{ID: k.ID, Title: k.Title, Icon: k.Icon, App: a.ID, AppName: a.Manifest.Name, Editor: k.Editor, SchemaVersion: k.SchemaVersion})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Kind is one document type of an installed app; ErrNotInstalled when no app that is on adds it.
func (r *Registry) Kind(id string) (KindInfo, SinopiaKind, error) {
	list, err := r.List()
	if err != nil {
		return KindInfo{}, SinopiaKind{}, err
	}
	for _, a := range list {
		if a.Manifest == nil {
			continue
		}
		s, _ := a.Manifest.SinopiaOf()
		for _, k := range s.Kinds {
			if k.ID == id {
				return KindInfo{ID: k.ID, Title: k.Title, Icon: k.Icon, App: a.ID, AppName: a.Manifest.Name, Editor: k.Editor, SchemaVersion: k.SchemaVersion}, k, nil
			}
		}
	}
	return KindInfo{}, SinopiaKind{}, ErrNotInstalled
}

// kindConflict is the first type the manifest adds that an app other than itself already adds.
func kindConflict(m *Manifest, others []*Manifest) (kind, app string) {
	s, err := m.SinopiaOf()
	if err != nil || len(s.Kinds) == 0 {
		return "", ""
	}
	mine := map[string]bool{}
	for _, k := range s.Kinds {
		mine[k.ID] = true
	}
	for _, o := range others {
		if o == nil || o.ID == m.ID {
			continue
		}
		os, err := o.SinopiaOf()
		if err != nil {
			continue
		}
		for _, k := range os.Kinds {
			if mine[k.ID] {
				return k.ID, o.ID
			}
		}
	}
	return "", ""
}

func manifestsOf(list []Installed) []*Manifest {
	out := make([]*Manifest, 0, len(list))
	for _, a := range list {
		out = append(out, a.Manifest)
	}
	return out
}
