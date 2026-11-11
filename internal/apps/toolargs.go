package apps

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

// What the host reads of a window tool's inputSchema: the argument names for the block of the agent's turn,
// and a check of a call's arguments before it goes to the window, so that a call with the wrong arguments comes back
// with the schema it should have followed and the agent's next call is right. The check is of the top level only (an
// object, its required names, names the schema closes out, the kind and the enum of a value): it refuses what is
// surely wrong and leaves the rest to the window.

// maxSchemaHint is the most of a schema an answer carries.
const maxSchemaHint = 4096

type argSchema struct {
	Properties map[string]struct {
		Type any               `json:"type"`
		Enum []json.RawMessage `json:"enum"`
	} `json:"properties"`
	Required   []string `json:"required"`
	Additional any      `json:"additionalProperties"`
}

func readArgSchema(raw json.RawMessage) (argSchema, bool) {
	var s argSchema
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return argSchema{}, false
	}
	return s, true
}

// toolArgNames are the names of a tool's arguments: the required ones first, in the schema's order, then the others by
// name; required says how many of them are required.
func toolArgNames(schema json.RawMessage) (names []string, required int) {
	s, ok := readArgSchema(schema)
	if !ok {
		return nil, 0
	}
	for _, n := range s.Required {
		if _, has := s.Properties[n]; has && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	required = len(names)
	var rest []string
	for n := range s.Properties {
		if !slices.Contains(names, n) {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	return append(names, rest...), required
}

func kindOf(v any) string {
	switch n := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if n == math.Trunc(n) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	default:
		return "object"
	}
}

func kindFits(want any, got string) bool {
	one := func(w string) bool { return w == got || (w == "number" && got == "integer") }
	switch w := want.(type) {
	case string:
		return one(w)
	case []any:
		for _, v := range w {
			if s, _ := v.(string); one(s) {
				return true
			}
		}
		return len(w) == 0
	}
	return true
}

// argIssues says what of args surely does not follow the tool's schema; nil when nothing does.
func argIssues(schema, args json.RawMessage) []string {
	s, ok := readArgSchema(schema)
	if !ok {
		return nil
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var got map[string]any
	if json.Unmarshal(args, &got) != nil || got == nil {
		return []string{"the arguments are one JSON object"}
	}
	var issues []string
	for _, n := range s.Required {
		if _, has := got[n]; !has {
			issues = append(issues, n+" is required")
		}
	}
	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	closed := s.Additional == false
	for _, n := range names {
		p, known := s.Properties[n]
		if !known {
			if closed {
				issues = append(issues, n+" is not an argument of this tool")
			}
			continue
		}
		if kind := kindOf(got[n]); !kindFits(p.Type, kind) {
			issues = append(issues, fmt.Sprintf("%s is %s, the schema asks for %s", n, kind, strings.Trim(fmt.Sprint(p.Type), "[]")))
			continue
		}
		if len(p.Enum) > 0 {
			raw, _ := json.Marshal(got[n])
			if !slices.ContainsFunc(p.Enum, func(e json.RawMessage) bool { return string(e) == string(raw) }) {
				issues = append(issues, n+" is not one of the values the schema lists")
			}
		}
	}
	return issues
}

// schemaHint is the sentence that gives a tool's schema back with a refused call; "" for a tool with none.
func schemaHint(tool string, schema json.RawMessage) string {
	if len(schema) == 0 {
		return ""
	}
	return "The arguments of " + tool + " follow this schema: " + clipBytes(strings.Join(strings.Fields(string(schema)), " "), maxSchemaHint)
}
