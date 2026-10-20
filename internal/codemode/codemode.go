// Package codemode reads the JavaScript Codex sends to its "exec" tool in code mode and says in plain words what the
// script does. There is no JavaScript engine here: a small scanner finds the tools.<name>(...) calls and reads the
// literal arguments it can; anything it cannot read is left out of the summary.
package codemode

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// joinedMax is the longest summary of a script that makes several calls.
	joinedMax = 160
	// partMax is the longest part of such a summary.
	partMax = 60
	// codeMax is the longest "code: ..." line.
	codeMax = 100
)

// Call is one tools.<Name>(Arg) call found in a script. Arg is a string, an []any, an *Object, or nil when the argument
// is not a literal.
type Call struct {
	Name string
	Arg  any
}

// Description is what one script does: the tool to show it under and a one-line summary.
type Description struct {
	Tool    string
	Summary string
}

// Describe summarizes a code-mode script.
func Describe(js string) Description {
	calls := Calls(js)
	if len(calls) == 0 {
		return Description{Tool: "exec", Summary: "code: " + codeLine(js)}
	}
	tools := make([]string, len(calls))
	texts := make([]string, len(calls))
	same := true
	for i, c := range calls {
		tools[i], texts[i] = describeCall(c)
		same = same && tools[i] == tools[0]
	}
	if len(calls) == 1 {
		return Description{Tool: tools[0], Summary: texts[0]}
	}
	if !same {
		return Description{Tool: "exec", Summary: join(texts, false)}
	}
	return Description{Tool: tools[0], Summary: join(texts, tools[0] == "exec_command")}
}

// join puts the first parts that fit side by side; shell parts drop the repeated "$ ".
func join(parts []string, shell bool) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 && shell {
			p = strings.TrimPrefix(p, "$ ")
		}
		p = shorten(p, partMax)
		if p == "" {
			continue
		}
		sep := ""
		if b.Len() > 0 {
			sep = " · "
		}
		if b.Len() > 0 && utf8.RuneCountInString(b.String())+len(sep)+utf8.RuneCountInString(p) > joinedMax {
			b.WriteString(" · +" + itoa(len(parts)-i) + " more")
			return b.String()
		}
		b.WriteString(sep + p)
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}

// codeLine is the first line of the script that is neither empty nor only a comment.
func codeLine(js string) string {
	inBlock := false
	for _, line := range strings.Split(js, "\n") {
		line = strings.TrimSpace(line)
		if inBlock {
			if i := strings.Index(line, "*/"); i >= 0 {
				inBlock = false
				line = strings.TrimSpace(line[i+2:])
			} else {
				continue
			}
		}
		if strings.HasPrefix(line, "/*") {
			if i := strings.Index(line, "*/"); i >= 0 {
				line = strings.TrimSpace(line[i+2:])
			} else {
				inBlock = true
				continue
			}
		}
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		return shorten(line, codeMax)
	}
	return "script"
}

var spaceRun = regexp.MustCompile(`\s+`)

// shorten is s on one line, cut to limit characters with an ellipsis.
func shorten(s string, limit int) string {
	s = strings.TrimSpace(spaceRun.ReplaceAllString(s, " "))
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	r := []rune(s)
	return strings.TrimRightFunc(string(r[:limit-1]), unicode.IsSpace) + "…"
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

var patchFileRE = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+?)\s*$`)

// describeCall names the tool a call is shown under and summarizes it.
func describeCall(c Call) (tool, text string) {
	obj, _ := c.Arg.(*Object)
	switch c.Name {
	case "exec_command", "shell", "bash":
		cmd := obj.str("cmd")
		if cmd == "" {
			cmd = obj.str("command")
		}
		if cmd = firstLine(strings.TrimSpace(cmd)); cmd == "" {
			return "exec_command", "$ …"
		}
		return "exec_command", "$ " + cmd
	case "write_stdin":
		return "write_stdin", "Write stdin"
	case "apply_patch":
		patch, _ := c.Arg.(string)
		if patch == "" {
			patch = obj.str("input")
		}
		var files []string
		for _, m := range patchFileRE.FindAllStringSubmatch(patch, -1) {
			files = append(files, m[1])
		}
		switch len(files) {
		case 0:
			return "apply_patch", "patch"
		case 1:
			return "apply_patch", files[0]
		}
		return "apply_patch", files[0] + " +" + itoa(len(files)-1) + " more"
	case "view_image":
		p := obj.str("path")
		if p == "" {
			p = obj.str("file_path")
		}
		return "view_image", p
	case "web__run":
		return "web_search", webSummary(obj)
	}
	if strings.Contains(c.Name, "__") {
		return "mcp__" + c.Name, shorten(obj.firstString(), partMax)
	}
	return c.Name, shorten(obj.firstString(), partMax)
}

// webSummary is what a web tool call looks up: its queries, else the page it opens or searches.
func webSummary(o *Object) string {
	var qs []string
	for _, key := range []string{"search_query", "image_query"} {
		for _, item := range o.list(key) {
			if q := item.(*Object).str("q"); q != "" {
				qs = append(qs, q)
			}
		}
	}
	if len(qs) > 0 {
		return strings.Join(qs, " · ")
	}
	for _, item := range o.list("open") {
		if ref := item.(*Object).str("ref_id"); ref != "" {
			return "open " + ref
		}
	}
	for _, item := range o.list("find") {
		if p := item.(*Object).str("pattern"); p != "" {
			return `find "` + p + `"`
		}
	}
	for _, key := range []string{"query", "url"} {
		if v := o.str(key); v != "" {
			return v
		}
	}
	return "web"
}
