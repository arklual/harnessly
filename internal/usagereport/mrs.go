package usagereport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// Merge requests a session touched. Claude Code writes a pr-link entry only for what it recognises in `git push`
// output; agents create and merge requests through the GitLab API (curl, scripts, glab, an MCP server) as well, and
// those leave no entry. So the facts also keep the requests a Bash call or a merge-request MCP call created or pushed
// to, read from its command and its output. Only keys (hubproto.MRKey) are kept, never the text.

var (
	// mrAPIProject: …/projects/<id or url-encoded path>/merge_requests, not followed by a path or a word (checked by hand:
	// RE2 has no look-ahead).
	mrAPIProject = regexp.MustCompile(`/projects/([^/\s"'\\]+)/merge_requests`)
	mrIID        = regexp.MustCompile(`"iid"\s*:\s*(\d+)`)
	mrCreateCmd  = regexp.MustCompile(`merge_request\.create|glab\s+mr\s+create`)
	mrAPIWrite   = regexp.MustCompile(`(?i)-X\s*POST|--request[\s=]+POST|--data\b|--data-(?:raw|binary|urlencode)\b|\s-d\s|\s-F\s|--form\b|--json\b|\s-f\s+source_branch|method\s*=\s*['"]POST|\.post\(`)
	mrAPIRead    = regexp.MustCompile(`--get\b|\s-G\s`)
	mrPushNotice = regexp.MustCompile(`View merge request for`)
	mrHost       = regexp.MustCompile(`https?://([^/\s"'\\]+)`)
)

// mcpHead is how much of an MCP answer is read for addresses.
const mcpHead = 2000

// MRsOfCall is the merge requests a Bash call created or pushed to, read from its command and its output.
func MRsOfCall(command, output string) []string {
	project := ""
	for _, m := range mrAPIProject.FindAllStringSubmatchIndex(command, -1) {
		if end := m[1]; end < len(command) && (command[end] == '/' || isWord(command[end])) {
			continue
		}
		project = command[m[2]:m[3]]
		break
	}
	created := mrCreateCmd.MatchString(command) || (project != "" && mrAPIWrite.MatchString(command) && !mrAPIRead.MatchString(command))
	if !created && !mrPushNotice.MatchString(output) {
		return nil
	}
	keys := hubproto.MRKeysIn(output)
	if len(keys) == 0 && project != "" {
		if m := mrIID.FindStringSubmatch(output); m != nil {
			host := "gitlab"
			if h := mrHost.FindStringSubmatch(command); h != nil {
				host = h[1]
			}
			keys = append(keys, hubproto.MRKey(host, project, m[1]))
		}
	}
	return keys
}

func isWord(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// MRsOfMCP is the merge requests the answer of a merge-request MCP call names (save_merge_request creates or changes
// one; the answer carries its address).
func MRsOfMCP(verb, output string) []string {
	if verb != "save_merge_request" {
		return nil
	}
	if len(output) > mcpHead {
		output = output[:mcpHead]
	}
	return hubproto.MRKeysIn(output)
}

// mrKeysOfUse reads a finished, successful call's merge requests.
func mrKeysOfUse(u toolUse, output string) []string {
	switch u.kind {
	case ToolBash:
		return MRsOfCall(u.command, output)
	case ToolMCP:
		return MRsOfMCP(u.verb, output)
	}
	return nil
}

// PRKeys are the merge request keys a facts' pr-link addresses name, and the addresses that are not GitLab merge
// requests (counted as they were, never sent to the hub).
func PRKeys(prs []string) (keys, other []string) {
	for _, u := range prs {
		if k := hubproto.MRKeyOfURL(u); k != "" {
			keys = append(keys, k)
		} else {
			other = append(other, u)
		}
	}
	return keys, other
}

// The merge request keys of a transcript are kept beside the facts file, not in it: that file's format is the previous
// runtime's and the golden tests hold it byte for byte. An entry is valid while the transcript's size and time are the
// ones the facts file has for it; a transcript with none is read again, a few at a time (backfillPerPass), newest first,
// so that adding this does not make every machine parse every transcript at once.

// MRSuffix names the file next to a facts file that holds the keys.
const MRSuffix = ".mrs"

// backfillPerPass bounds how many already-known transcripts a pass reads again to find their merge requests.
const backfillPerPass = 20

// The tool calls of a transcript are kept in the same entry, summed by UTC day, kind and name: the facts file has no
// place for them either, and without them a machine whose agent reads the runtime's facts file (Stored) would never
// report a skill or an MCP server. An entry written before them has no "tools": its transcript is read again like one
// without keys, but only when it changed within toolBackfillDays.

type mrEntry struct {
	Size    float64  `json:"size"`
	MtimeMs float64  `json:"mtimeMs"`
	Keys    []string `json:"keys"`
	// Tools is nil in an entry written before tool calls were kept, and an empty list for a transcript with none.
	Tools []toolSum `json:"tools"`
	// Listed is how many skills the initial skill listing offered; absent when the transcript has none.
	Listed int `json:"listed,omitempty"`
}

// CountListedSkills is the number of distinct skills a skill listing names: its lines `- name: description` (a line
// without a description is a skill too). Only the count leaves the machine, never a name or a description.
func CountListedSkills(content string) int {
	seen := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		rest, ok := strings.CutPrefix(line, "- ")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, ": ")
		seen[name] = true
	}
	return len(seen)
}

// toolSum is the calls of one tool on one UTC day of a transcript; Ts is the time of the last of them.
type toolSum struct {
	Ts     float64 `json:"ts"`
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Calls  int     `json:"calls"`
	Errors int     `json:"errors,omitempty"`
}

// toolBackfillDays bounds how old a transcript read again for its tool calls may be: the hub keeps the last month.
const toolBackfillDays = 35

// sumTools sums the calls by UTC day, kind and name, in a fixed order; calls of no known time or name are left out.
func sumTools(calls []ToolCall) []toolSum {
	type key struct{ day, kind, name string }
	at := map[key]int{}
	out := []toolSum{}
	for _, c := range calls {
		if c.Ts <= 0 || c.Name == "" {
			continue
		}
		k := key{time.UnixMilli(int64(c.Ts * 1000)).UTC().Format("2006-01-02"), c.Kind, c.Name}
		n, errs := c.Count()
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, toolSum{Kind: c.Kind, Name: c.Name})
		}
		out[i].Ts = max(out[i].Ts, c.Ts)
		out[i].Calls += n
		out[i].Errors += errs
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Ts != b.Ts {
			return a.Ts < b.Ts
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
	return out
}

// callsOf reads the sums back as calls; a nil list stays nil (not known), an empty one is an empty list.
func callsOf(sums []toolSum) []ToolCall {
	if sums == nil {
		return nil
	}
	out := make([]ToolCall, 0, len(sums))
	for _, t := range sums {
		if t.Calls <= 0 {
			continue
		}
		out = append(out, ToolCall{Ts: t.Ts, Kind: t.Kind, Name: t.Name, N: t.Calls, Errs: t.Errors, Error: t.Errors > 0})
	}
	return out
}

type mrFile struct {
	Version int                `json:"version"`
	Files   map[string]mrEntry `json:"files"`
}

func readMRs(path string) map[string]mrEntry {
	raw, err := os.ReadFile(path + MRSuffix)
	if err != nil {
		return nil
	}
	var f mrFile
	if json.Unmarshal(raw, &f) != nil || f.Version != 1 {
		return nil
	}
	return f.Files
}

func writeMRs(path string, files map[string]mrEntry) {
	raw, err := json.Marshal(mrFile{Version: 1, Files: files})
	if err == nil {
		tmp := fmt.Sprintf("%s%s.%d.tmp", path, MRSuffix, os.Getpid())
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			if err = os.WriteFile(tmp, raw, 0o600); err == nil {
				err = os.Rename(tmp, path+MRSuffix)
			}
		}
	}
	if err != nil {
		jsonlog.Log("warn", "usage merge request keys write failed", jsonlog.F{K: "error", V: err.Error()})
	}
}

// applyMRs gives the facts the keys and the tool calls kept for them, when the entry is of the same file state.
func applyMRs(files map[string]*cachedFile, kept map[string]mrEntry) {
	for name, f := range files {
		if e, ok := kept[name]; ok && e.Size == f.size && e.MtimeMs == f.mtimeMs && f.facts != nil {
			f.facts.MRKeys = append([]string{}, e.Keys...)
			f.facts.ToolCalls = callsOf(e.Tools)
			f.facts.SkillsListed = e.Listed
		}
	}
}

// bashCommand keeps a Bash call's command until its result comes.
func bashCommand(input []byte) string {
	s, _ := objectOf(input).str("command")
	return s
}

// mcpVerb is the tool of an MCP name `mcp__<server>__<verb>`.
func mcpVerb(name string) string {
	rest := strings.TrimPrefix(name, "mcp__")
	_, verb, _ := strings.Cut(rest, "__")
	return verb
}
