package config

import "strings"

// ExtensionsConfig is what the extension protocol is switched on by (docs/hub/extensions.md). It is off by default and
// then changes nothing.
type ExtensionsConfig struct {
	// On (HUB_EXTENSIONS=on) registers the adapters of File and asks them to describe themselves.
	On bool
	// File (HUB_EXTENSIONS_FILE) is a JSON array of adapters: name, kind, url, tokenFile, protocol (ext.Spec).
	File string
}

func loadExtensions(lookup func(string) (string, bool), def func(string, string) string, bad func(string, ...any)) ExtensionsConfig {
	var e ExtensionsConfig
	if v, ok := lookup("HUB_EXTENSIONS_FILE"); ok {
		e.File = strings.TrimSpace(v)
	}
	switch v := strings.ToLower(def("HUB_EXTENSIONS", "off")); v {
	case "on":
		e.On = true
		if e.File == "" {
			bad("HUB_EXTENSIONS=on needs HUB_EXTENSIONS_FILE")
		}
	case "off":
	default:
		bad("HUB_EXTENSIONS must be on or off, got %q", v)
	}
	return e
}
