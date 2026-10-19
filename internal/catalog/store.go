package catalog

// The previous runtime's data files this package reads: the quick prompts and the workspaces. They are read the way
// that runtime read them, malformed shapes included, and never written.

// builtinPrompts are the quick prompts every runtime has; a file can only switch them off.
var builtinPrompts = []map[string]any{
	{"id": "builtin-commit", "name": "Commit", "prompt": "/commit-commands:commit", "enabled": true},
	{"id": "builtin-simplify", "name": "Simplify", "prompt": "/simplify", "enabled": true},
}

func isBuiltinPromptID(v any) bool {
	id, ok := v.(string)
	if !ok {
		return false
	}
	for _, b := range builtinPrompts {
		if b["id"] == id {
			return true
		}
	}
	return false
}

// truthy is JavaScript's truthiness of a parsed JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

// quickPrompts are the builtin prompts (with their switch) followed by the custom ones, each entry as stored; a file
// that cannot be read, or whose shape the previous runtime choked on, gives the builtin prompts alone.
func quickPrompts(file string) []map[string]any {
	defaults := func(disabled map[string]bool, custom []map[string]any) []map[string]any {
		out := make([]map[string]any, 0, len(builtinPrompts)+len(custom))
		for _, b := range builtinPrompts {
			c := map[string]any{}
			for k, v := range b {
				c[k] = v
			}
			c["enabled"] = !disabled[b["id"].(string)]
			out = append(out, c)
		}
		return append(out, custom...)
	}
	parsed, ok := readJSON(file)
	if !ok || parsed == nil {
		return defaults(nil, nil)
	}
	asEntry := func(v any) map[string]any {
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return map[string]any{} // a spread of a primitive or an array keeps no prompt
	}
	// A flat array is the oldest shape: builtin entries carry their switch, the rest are custom.
	if arr, isArr := parsed.([]any); isArr {
		disabled := map[string]bool{}
		var custom []map[string]any
		for _, p := range arr {
			if p == nil {
				return defaults(nil, nil)
			}
			m, _ := p.(map[string]any)
			if m != nil && isBuiltinPromptID(m["id"]) {
				if !truthy(m["enabled"]) {
					disabled[m["id"].(string)] = true
				}
				continue
			}
			custom = append(custom, asEntry(p))
		}
		return defaults(disabled, custom)
	}
	obj, isObj := parsed.(map[string]any)
	if !isObj {
		return defaults(nil, nil)
	}
	disabled := map[string]bool{}
	switch ids := obj["disabledBuiltinIds"].(type) {
	case nil, string: // a string is a set of its characters: no id matches
	case []any:
		for _, id := range ids {
			if s, ok := id.(string); ok {
				disabled[s] = true
			}
		}
	default:
		return defaults(nil, nil)
	}
	var custom []map[string]any
	switch list := obj["custom"].(type) {
	case nil:
	case []any:
		for _, c := range list {
			custom = append(custom, asEntry(c))
		}
	default:
		return defaults(nil, nil)
	}
	return defaults(disabled, custom)
}

// workspaceDirs are the folders of the runtime's workspaces, in file order, as they were listed before resolving:
// none when the file is unreadable or has an entry the previous runtime failed on.
func workspaceDirs(file string) []string {
	parsed, ok := readJSON(file)
	if !ok {
		return nil
	}
	data, _ := parsed.(map[string]any)
	workspaces, isArr := data["workspaces"].([]any)
	if data == nil || !isArr {
		return nil
	}
	if groups, ok := data["groups"].([]any); ok {
		for _, g := range groups {
			if g == nil {
				return nil
			}
		}
	}
	var dirs []string
	for _, w := range workspaces {
		if w == nil {
			return nil
		}
		ws, ok := w.(map[string]any)
		if !ok {
			continue
		}
		v := ws["directories"]
		if !truthy(v) && truthy(ws["directory"]) {
			v = []any{ws["directory"]}
		}
		switch x := v.(type) {
		case string:
			dirs = append(dirs, x)
		case []any:
			for _, d := range x {
				if s, ok := d.(string); ok {
					dirs = append(dirs, s)
				}
			}
		}
	}
	return dirs
}
