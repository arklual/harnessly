package agenteval

import (
	"bufio"
	"bytes"
	"os"
	"path"
	"sort"
	"strings"
)

// The agent's surface is what an agent is told and given: the workspace prompt, the skills, the MCP tools, the
// `harnessly` commands. A change there changes what agents do, so it comes with a scenario (docs/agent-evals.md,
// «New features»). evals/agent/surface.txt lists it, one glob per line.

// ReadSurface reads the surface file: globs, # comments, blank lines skipped.
func ReadSurface(file string) ([]string, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out, sc.Err()
}

// MatchPath says whether a repository path matches a glob: path.Match per segment, "**" for any number of segments,
// and a leading "!" is not handled here (see Affected).
func MatchPath(glob, p string) bool {
	return matchSegs(strings.Split(glob, "/"), strings.Split(p, "/"))
}

func matchSegs(g, p []string) bool {
	for len(g) > 0 {
		if g[0] == "**" {
			for i := 0; i <= len(p); i++ {
				if matchSegs(g[1:], p[i:]) {
					return true
				}
			}
			return false
		}
		if len(p) == 0 {
			return false
		}
		if ok, _ := path.Match(g[0], p[0]); !ok {
			return false
		}
		g, p = g[1:], p[1:]
	}
	return len(p) == 0
}

// matchAny: a path matches the list when a glob takes it and no later "!glob" takes it back.
func matchAny(globs []string, p string) bool {
	in := false
	for _, g := range globs {
		if neg, ok := strings.CutPrefix(g, "!"); ok {
			if in && MatchPath(neg, p) {
				in = false
			}
			continue
		}
		if MatchPath(g, p) {
			in = true
		}
	}
	return in
}

// Impact is what a change does to the agent's surface.
type Impact struct {
	// Surface are the changed files of the surface.
	Surface []string `json:"surface"`
	// Scenarios are the scenarios whose covers a changed file matches.
	Scenarios []string `json:"scenarios"`
	// ScenarioFiles are the changed files of the scenarios' folder.
	ScenarioFiles []string `json:"scenarioFiles"`
}

// NeedsScenario: the surface changed and no scenario was added or changed with it.
func (i Impact) NeedsScenario() bool { return len(i.Surface) > 0 && len(i.ScenarioFiles) == 0 }

// Affected reads a change (repository paths) against the surface and the scenarios; dir is the scenarios' folder as a
// repository path (evals/agent).
func Affected(changed, surface []string, scenarios []*Scenario, dir string) Impact {
	var im Impact
	seen := map[string]bool{}
	dir = strings.TrimSuffix(dir, "/") + "/"
	for _, f := range changed {
		if strings.HasPrefix(f, dir) {
			im.ScenarioFiles = append(im.ScenarioFiles, f)
		}
		if matchAny(surface, f) {
			im.Surface = append(im.Surface, f)
		}
		for _, s := range scenarios {
			if !seen[s.ID] && matchAny(s.Covers, f) {
				seen[s.ID] = true
				im.Scenarios = append(im.Scenarios, s.ID)
			}
		}
	}
	sort.Strings(im.Scenarios)
	return im
}
