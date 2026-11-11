package apps

import (
	"errors"
	"sort"
	"strconv"
)

// Extensions. An app is harnessly's one extension package; three kinds of thing are told apart so a person
// and a check can say what a package is:
//
//   - ui      an extension of the interface: it only fills slots (settings, onboarding, object-action, hub-settings,
//     branding) with sandboxed views. No agent, no chat, no backend.
//   - runtime an extension of the runtime: views in a pane or a screen of its own, chat cards and commands, Sinopia
//     documents, a backend; it may carry an agent profile too.
//   - tool    a tool for the agent: instructions, skills, MCP servers, no view at all.
//
// The Hub has its own extensions (internal/hub/ext, an address the Hub talks to); they are not apps and are not in this
// list. `kind` in the manifest is optional: it is worked out from what the package contains, and when it is written it
// has to agree with that.
const (
	KindUI      = "ui"
	KindRuntime = "runtime"
	KindTool    = "tool"
)

// ExtensionKinds are the kinds above in the order the docs name them.
var ExtensionKinds = []string{KindUI, KindRuntime, KindTool}

// ExtensionKind is the kind of the package: the one written in the manifest or, when none is, the one its contents make.
func (m *Manifest) ExtensionKind() string {
	if m.Kind != "" {
		return m.Kind
	}
	return m.contentKind()
}

func (m *Manifest) contentKind() string {
	slotsOnly, any := true, false
	for _, t := range m.Targets {
		any = true
		if !IsSlot(t) {
			slotsOnly = false
		}
	}
	switch {
	case m.Agent != nil || m.Chat != nil || m.Backend != nil || len(m.Sinopia) > 0:
		return KindRuntime
	case any && slotsOnly:
		return KindUI
	case len(m.Views) == 0 && !any:
		return KindTool
	case len(m.Views) == 0:
		return KindRuntime
	}
	return KindRuntime
}

// checkExtension is the part of the manifest check that is about HRN-754: kind and data.
func (m *Manifest) checkExtension(targets map[string]bool, add func(path, format string, a ...any), warn func(path, format string, a ...any)) {
	if m.Kind != "" {
		switch {
		case !contains(ExtensionKinds, m.Kind):
			add("kind", "%q is not ui, runtime or tool", m.Kind)
		case m.Kind == KindUI && m.contentKind() != KindUI:
			add("kind", "a ui extension fills the slots only: it has no agent, chat, backend or Sinopia part and no target outside %v", SlotTargets)
		case m.Kind == KindTool && len(m.Views) > 0:
			add("kind", "a tool for the agent has no views")
		}
	}
	if d := m.Data; d != nil {
		if d.Version < 1 || d.Version > 1_000 {
			add("data.version", "is the version of the app's data, from 1")
		}
		for from, file := range d.Migrations {
			n, err := strconv.Atoi(from)
			switch {
			case err != nil || n < 1 || strconv.Itoa(n) != from:
				add("data.migrations", "%q is not a version: a step is named by the version it starts from (1, 2, …)", from)
			case n >= d.Version:
				add("data.migrations."+from, "the data is at version %d: nothing to migrate from %d", d.Version, n)
			case file == "":
				add("data.migrations."+from, "is a JSON Patch file of the package")
			}
		}
		for n := 1; n < d.Version && n < 1_000; n++ {
			if d.Migrations[strconv.Itoa(n)] == "" {
				add("data.migrations", "no step from version %d: data of that version could not be brought up to date", n)
				break
			}
		}
	} else if len(m.Storage) > 0 {
		warn("data", "the app keeps data but declares no data.version: an update cannot migrate it")
	}
	_ = targets
}

// DataSpec is the version of the app's own data and the JSON Patch file that brings it from each earlier version.
type DataSpec struct {
	Version    int               `json:"version"`
	Migrations map[string]string `json:"migrations,omitempty"`
}

// checkDataFiles reads the migration files of the package: each is a JSON Patch the engine accepts.
func (p *Package) checkDataFiles(ps *Problems) {
	d := p.Manifest.Data
	if d == nil {
		return
	}
	froms := make([]string, 0, len(d.Migrations))
	for from := range d.Migrations {
		froms = append(froms, from)
	}
	sort.Strings(froms)
	for _, from := range froms {
		path := "data.migrations." + from
		raw, err := p.File(d.Migrations[from])
		if err != nil {
			*ps = append(*ps, Problem{Path: path, Message: err.Error()})
			continue
		}
		if _, err := ParsePatch(raw); err != nil {
			*ps = append(*ps, Problem{Path: path, Message: err.Error()})
		}
	}
}

var (
	// ErrSecretUndeclared is a secret the manifest does not name.
	ErrSecretUndeclared = errors.New("apps: the app declares no such secret")
	// ErrSecretRight is a secret the app has no secrets:<name> permission for.
	ErrSecretRight = errors.New("apps: the app has no right to this secret")
	// ErrDisabled is an app that is switched off.
	ErrDisabled = errors.New("apps: the app is disabled")
)

// KindsOf is the kind of every installed app, by id.
func KindsOf(list []Installed) map[string]string {
	out := make(map[string]string, len(list))
	for _, a := range list {
		if a.Manifest != nil {
			out[a.ID] = a.Manifest.ExtensionKind()
		}
	}
	return out
}
