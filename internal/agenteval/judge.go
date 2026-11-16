package agenteval

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Verdict is one expectation's outcome.
type Verdict struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Why  string `json:"why,omitempty"`
}

// Check checks the facts of a run against the scenario's expectations, in order.
func (s *Scenario) Check(f Facts) []Verdict {
	out := make([]Verdict, 0, len(s.Expect))
	for _, e := range s.Expect {
		ok, why := e.holds(f)
		out = append(out, Verdict{Name: e.label(), OK: ok, Why: why})
	}
	return out
}

// Passed says whether every expectation held.
func Passed(v []Verdict) bool {
	for _, x := range v {
		if !x.OK {
			return false
		}
	}
	return len(v) > 0
}

func (e Expect) label() string {
	if e.Name != "" {
		return e.Name
	}
	switch {
	case e.Called != "":
		return "called " + e.Called + argsLabel(e.Args)
	case e.NotCalled != "":
		return "did not call " + e.NotCalled + argsLabel(e.Args)
	case e.Pane != nil:
		return strings.TrimSpace("opened " + e.Pane.Tab + " " + e.Pane.Target + " " + e.Pane.Place)
	case e.Images > 0:
		return fmt.Sprintf("drew %d picture(s)", e.Images)
	case e.File != nil:
		if e.File.Absent {
			return "no file " + e.File.Glob
		}
		return "file " + e.File.Glob
	case e.Text != "":
		return "said " + e.Text
	case e.TextNot != "":
		return "did not say " + e.TextNot
	case e.MaxCalls > 0:
		return fmt.Sprintf("at most %d calls", e.MaxCalls)
	case e.Check != "":
		return "check " + clip(e.Check, 80)
	}
	parts := make([]string, 0, len(e.AnyOf))
	for _, a := range e.AnyOf {
		parts = append(parts, a.label())
	}
	return "one of: " + strings.Join(parts, " | ")
}

func argsLabel(a map[string]string) string {
	if len(a) == 0 {
		return ""
	}
	b, _ := json.Marshal(a)
	return " " + string(b)
}

func whole(re string) *regexp.Regexp { return regexp.MustCompile(`^(?:` + re + `)$`) }

// argsMatch says whether a call's arguments match every regexp of args.
func argsMatch(raw json.RawMessage, args map[string]string) bool {
	if len(args) == 0 {
		return true
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	for k, re := range args {
		v, ok := m[k]
		if !ok {
			return false
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			s = string(v)
		}
		if !regexp.MustCompile(re).MatchString(s) {
			return false
		}
	}
	return true
}

func (e Expect) holds(f Facts) (bool, string) {
	switch {
	case e.Called != "":
		name := whole(e.Called)
		var near []string
		for _, c := range f.Calls {
			if !name.MatchString(c.Name) {
				continue
			}
			if c.Error {
				near = append(near, c.Name+" failed: "+clip(c.Result, 160))
				continue
			}
			if argsMatch(c.Args, e.Args) {
				return true, ""
			}
			near = append(near, c.Name+" with "+clip(string(c.Args), 200))
		}
		if len(near) > 0 {
			return false, "no matching call; there were: " + strings.Join(near, "; ")
		}
		return false, "never called; calls were: " + callNames(f)
	case e.NotCalled != "":
		name := whole(e.NotCalled)
		for _, c := range f.Calls {
			if name.MatchString(c.Name) && !c.Error && argsMatch(c.Args, e.Args) {
				return false, "called " + c.Name + " " + clip(string(c.Args), 200)
			}
		}
		return true, ""
	case e.Pane != nil:
		tab := whole(e.Pane.Tab)
		var target *regexp.Regexp
		if e.Pane.Target != "" {
			target = regexp.MustCompile(e.Pane.Target)
		}
		var place *regexp.Regexp
		if e.Pane.Place != "" {
			place = regexp.MustCompile(e.Pane.Place)
		}
		var seen []string
		for _, p := range f.Pane {
			if p.Kind != "open" {
				continue
			}
			if tab.MatchString(p.Tab) && (target == nil || target.MatchString(p.Target)) && (place == nil || place.MatchString(p.Place)) {
				return true, ""
			}
			seen = append(seen, strings.TrimSpace(p.Tab+" "+p.Target+" "+p.Place))
		}
		if len(seen) == 0 {
			return false, "nothing was opened in the pane"
		}
		return false, "opened instead: " + strings.Join(seen, "; ")
	case e.Images > 0:
		n := 0
		var errs []string
		for _, j := range f.Images {
			n += j.Images
			if j.Error != "" {
				errs = append(errs, j.Error)
			}
		}
		if n >= e.Images {
			return true, ""
		}
		why := fmt.Sprintf("%d picture(s) in %d job(s)", n, len(f.Images))
		if len(errs) > 0 {
			why += ": " + strings.Join(errs, "; ")
		}
		return false, why
	case e.File != nil:
		var contains *regexp.Regexp
		if e.File.Contains != "" {
			contains = regexp.MustCompile(e.File.Contains)
		}
		matched := 0
		for _, ff := range f.Files {
			if ok, _ := path.Match(e.File.Glob, ff.Path); !ok {
				continue
			}
			matched++
			if contains == nil || contains.MatchString(ff.Text) {
				if e.File.Absent {
					return false, "there is " + ff.Path
				}
				return true, ""
			}
		}
		if e.File.Absent {
			return true, ""
		}
		if matched > 0 {
			return false, fmt.Sprintf("%d file(s) match the glob, none has the text", matched)
		}
		return false, "no such file"
	case e.Text != "":
		if regexp.MustCompile(e.Text).MatchString(strings.Join(f.Said, "\n")) {
			return true, ""
		}
		return false, "the agent's last words: " + clip(f.Final, 200)
	case e.TextNot != "":
		if m := regexp.MustCompile(e.TextNot).FindString(strings.Join(f.Said, "\n")); m != "" {
			return false, "said «" + clip(m, 120) + "»"
		}
		return true, ""
	case e.MaxCalls > 0:
		if len(f.Calls) <= e.MaxCalls {
			return true, ""
		}
		return false, fmt.Sprintf("%d calls", len(f.Calls))
	case e.Check != "":
		for _, c := range f.Checks {
			if c.Command != e.Check {
				continue
			}
			if c.Exit == 0 {
				return true, ""
			}
			return false, fmt.Sprintf("exit %d: %s", c.Exit, clip(strings.TrimSpace(c.Output), 400))
		}
		return false, "the check did not run"
	}
	var whys []string
	for _, a := range e.AnyOf {
		ok, why := a.holds(f)
		if ok {
			return true, ""
		}
		whys = append(whys, why)
	}
	return false, strings.Join(whys, " | ")
}

func callNames(f Facts) string {
	if len(f.Calls) == 0 {
		return "none"
	}
	names := make([]string, 0, len(f.Calls))
	for _, c := range f.Calls {
		n := c.Name
		if c.Name == "Bash" {
			var a struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(c.Args, &a)
			n += "(" + clip(a.Command, 60) + ")"
		}
		names = append(names, n)
	}
	return clip(strings.Join(names, ", "), 600)
}

// Checks are the check commands of the expectations, any_of included, each once.
func (s *Scenario) Checks() []string {
	var out []string
	seen := map[string]bool{}
	var walk func([]Expect)
	walk = func(es []Expect) {
		for _, e := range es {
			if e.Check != "" && !seen[e.Check] {
				seen[e.Check] = true
				out = append(out, e.Check)
			}
			walk(e.AnyOf)
		}
	}
	walk(s.Expect)
	return out
}
