package eventbus

import (
	"slices"
	"strings"
)

// Filter says which events a subscription takes. Every non-empty part must hold; an empty filter takes everything the
// subscription may see.
type Filter struct {
	// Types are event types; "chat.*" takes every type of the domain.
	Types       []string          `json:"types,omitempty"`
	Sources     []string          `json:"sources,omitempty"`
	Connections []string          `json:"connections,omitempty"`
	Facts       map[string]string `json:"facts,omitempty"` // each fact equal
	// Mine: true takes only what the owner did themselves, false only what others did.
	Mine *bool `json:"mine,omitempty"`
}

// Match says whether the filter takes the event.
func (f Filter) Match(e Event) bool {
	if len(f.Types) > 0 && !slices.ContainsFunc(f.Types, func(t string) bool { return typeMatches(t, e.Type) }) {
		return false
	}
	if len(f.Sources) > 0 && !slices.Contains(f.Sources, e.Source) {
		return false
	}
	if len(f.Connections) > 0 && !slices.Contains(f.Connections, e.Connection) {
		return false
	}
	for k, v := range f.Facts {
		if e.Facts[k] != v {
			return false
		}
	}
	if f.Mine != nil && *f.Mine != e.Mine {
		return false
	}
	return true
}

func typeMatches(pattern, typ string) bool {
	if domain, ok := strings.CutSuffix(pattern, ".*"); ok {
		return strings.HasPrefix(typ, domain+".")
	}
	return pattern == typ
}

// valid checks a filter a person or an app gave.
func (f Filter) valid() error {
	if len(f.Types)+len(f.Sources)+len(f.Connections)+len(f.Facts) > 64 {
		return invalid("the filter is too long")
	}
	for _, t := range f.Types {
		if p, ok := strings.CutSuffix(t, ".*"); ok {
			t = p + ".x"
		}
		if !typeRe.MatchString(t) {
			return invalid("filter type %q", t)
		}
	}
	for _, s := range f.Sources {
		if !sourceRe.MatchString(s) {
			return invalid("filter source %q", s)
		}
	}
	for k := range f.Facts {
		if !factRe.MatchString(k) {
			return invalid("filter fact %q", k)
		}
	}
	return nil
}
