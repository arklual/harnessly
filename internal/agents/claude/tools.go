package claude

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
)

// jsKeys are Object.keys(v) of a decoded value: an object's keys, a string's or an array's indices.
func jsKeys(v any) []string {
	switch x := v.(type) {
	case *agents.Obj:
		return x.Keys()
	case []any:
		out := make([]string, len(x))
		for i := range x {
			out[i] = strconv.Itoa(i)
		}
		return out
	case string:
		n := jscompat.Len(x)
		out := make([]string, n)
		for i := range n {
			out[i] = strconv.Itoa(i)
		}
		return out
	}
	return nil
}

// jsGet is v[k] of a decoded value.
func jsGet(v any, k string) any {
	switch x := v.(type) {
	case *agents.Obj:
		return x.Val(k)
	case []any:
		if i, err := strconv.Atoi(k); err == nil && i >= 0 && i < len(x) {
			return x[i]
		}
	case string:
		units := utf16.Encode([]rune(x))
		if i, err := strconv.Atoi(k); err == nil && i >= 0 && i < len(units) {
			return string(utf16.Decode(units[i : i+1]))
		}
	}
	return nil
}

// orDefault is `v ?? def` stringified: String(input.x ?? ”).
func orDefault(v any, def string) string {
	if v == nil {
		return def
	}
	return agents.JSString(v)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// SummarizeToolCall is the one-line description of a tool call shown as the agent's current action.
func SummarizeToolCall(name string, input any) string {
	switch name {
	case "Read":
		fp := jsGet(input, "file_path")
		if fp == nil {
			fp = jsGet(input, "path")
		}
		return "Read " + orDefault(fp, "")
	case "Edit":
		fp := orDefault(jsGet(input, "file_path"), "")
		oldStr := orDefault(jsGet(input, "old_string"), "")
		newStr := orDefault(jsGet(input, "new_string"), "")
		verb := "Create"
		if oldStr != "" {
			verb = "Update"
		}
		added, removed := countDiffLines(oldStr, newStr)
		return verb + " " + fp + " (+" + strconv.Itoa(added) + ", -" + strconv.Itoa(removed) + ")"
	case "Write":
		return "Write " + orDefault(jsGet(input, "file_path"), "")
	case "Bash":
		return "$ " + firstLine(orDefault(jsGet(input, "command"), ""))
	case "Grep", "Glob":
		return name + ` "` + orDefault(jsGet(input, "pattern"), "") + `"`
	}
	keys := jsKeys(input)
	firstVal := ""
	if len(keys) > 0 && keys[0] != "" {
		firstVal = jscompat.Slice(agents.JSString(jsGet(input, keys[0])), 60)
	}
	if firstVal == "" {
		return name
	}
	return name + " " + firstVal
}

var lineSplit = regexp.MustCompile(`\n|\r\n`)

// lineTokens splits text into lines the way a line diff does: each line keeps its line break, a last line without one
// is a token of its own.
func lineTokens(s string) []string {
	parts := []string{}
	last := 0
	for _, loc := range lineSplit.FindAllStringIndex(s, -1) {
		parts = append(parts, s[last:loc[0]], s[loc[0]:loc[1]])
		last = loc[1]
	}
	parts = append(parts, s[last:])
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	var lines []string
	for i, p := range parts {
		if i%2 == 1 {
			lines[len(lines)-1] += p
		} else {
			lines = append(lines, p)
		}
	}
	out := lines[:0]
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// countDiffLines is the number of lines a minimal line diff from oldStr to newStr adds and removes.
func countDiffLines(oldStr, newStr string) (added, removed int) {
	a, b := lineTokens(oldStr), lineTokens(newStr)
	d := editDistance(a, b)
	common := (len(a) + len(b) - d) / 2
	return len(b) - common, len(a) - common
}

// editDistance is the length of the shortest insert/delete script turning a into b (Myers).
func editDistance(a, b []string) int {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return n + m
	}
	ids := map[string]int{}
	id := func(s string) int {
		if v, ok := ids[s]; ok {
			return v
		}
		ids[s] = len(ids)
		return ids[s]
	}
	x, y := make([]int, n), make([]int, m)
	for i, s := range a {
		x[i] = id(s)
	}
	for i, s := range b {
		y[i] = id(s)
	}
	limit := n + m
	v := make([]int, 2*limit+2)
	for d := 0; d <= limit; d++ {
		for k := -d; k <= d; k += 2 {
			var i int
			if k == -d || (k != d && v[limit+k-1] < v[limit+k+1]) {
				i = v[limit+k+1]
			} else {
				i = v[limit+k-1] + 1
			}
			j := i - k
			for i < n && j < m && x[i] == y[j] {
				i++
				j++
			}
			v[limit+k] = i
			if i >= n && j >= m {
				return d
			}
		}
	}
	return limit
}
