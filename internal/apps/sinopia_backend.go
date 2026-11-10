package apps

import (
	"errors"
	"slices"
	"strings"
)

// Sinopia's backend for the agent (HRN-808, stage 5): the tools sinopia_* are written in its manifest
// (builtin/sinopia/harnessly/app.json, backend.tools, each with the Document API method it calls) and the host's tools
// server offers them as it does any builtin app's. This is the code that answers them: the same calls a view makes
// (sinopia_api.go), for the document the editor shows beside the agent's chat, or the one named in `document`. The
// policy reads them as app.sinopia.*.

// SinopiaHost is what Sinopia's backend needs of the machine.
type SinopiaHost struct {
	Docs  SinopiaDocs
	State *SinopiaState
	// Tab is the design tab of a chat (the holder of its claims); "" when it has none.
	Tab func(conv string) string
	// Package reads an installed app's package; nil when it cannot be read.
	Package func(id string) *Package
}

func init() { builtinCalls[SinopiaApp] = sinopiaBackend }

// sinopiaBackend runs one tool of Sinopia's backend for the agent of a chat.
func sinopiaBackend(t HostTools, tool BackendTool, args map[string]any) (any, error) {
	h := t.Sinopia
	if h == nil || h.Docs == nil {
		return nil, refusal("this machine has no Sinopia")
	}
	text := func(key string) string { s, _ := args[key].(string); return s }
	project, kind := "", ""
	if project = text("document"); project != "" {
		doc, err := h.Docs.Document(project)
		if err != nil {
			return nil, err
		}
		kind = doc.Kind
	} else {
		var ok bool
		if project, kind, ok = h.State.DocumentOf(t.Conv); !ok {
			return nil, refusal("no Sinopia document is open beside this chat: name one in `document`")
		}
	}
	// The apps that work here: those turned on in this chat and those that are on in every document of the kind.
	candidates := slices.Clone(t.Enabled())
	if list, err := t.Registry.List(); err == nil {
		for _, a := range ActiveInDocument(list, kind) {
			if !slices.Contains(candidates, a.ID) {
				candidates = append(candidates, a.ID)
			}
		}
	}
	var working []string
	for _, id := range candidates {
		if a, err := t.Registry.Get(id); err == nil && a.Manifest != nil && a.Manifest.HasTarget("sinopia."+kind) {
			working = append(working, id)
		}
	}
	app := text("app")
	switch {
	case app != "" && !slices.Contains(working, app):
		return nil, refusal(app + " does not work in this " + kind + " document")
	case app == "" && len(working) == 0:
		return nil, refusal("no app works in this " + kind + " document")
	case app == "" && len(working) > 1:
		return nil, refusal("several apps work in this document (" + strings.Join(working, ", ") + "): name one in `app`")
	case app == "":
		app = working[0]
	}
	installed, err := t.Registry.Get(app)
	if err != nil || installed.Manifest == nil {
		return nil, refusal(app + " is not installed")
	}
	params := map[string]any{}
	for k, v := range args {
		if k != "app" && k != "document" {
			params[k] = v
		}
	}
	call := SinopiaCall{Docs: h.Docs, State: h.State, App: installed, Project: project, Who: installed.Manifest.Name, Agent: true}
	if h.Tab != nil {
		call.Tab = h.Tab(t.Conv)
	}
	if h.Package != nil {
		call.Pkg = h.Package(app)
	}
	return call.Call(tool.Call, params)
}

// refusal is an answer of a builtin's backend the agent reads as a refusal in words.
func refusal(msg string) error { return errors.New(msg) }
