package hlcli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
)

// A board of a project on a kit is not finished while `design check --strict` has findings. The design-guard hook
// notes the boards a chat writes; the Stop hook (design-release) checks them once and, when strict finds something,
// tells the agent to go on instead of ending the turn. It asks once per turn: the Stop that follows the reminder
// (stop_hook_active) lets the turn end.

func (c *cli) strictPendingFile() string {
	name := c.hookSession()
	if name == "" {
		name = "chat"
	}
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, name)
	return filepath.Join(c.dataDir(), "design-strict-pending", name+".json")
}

// noteBoardWrites remembers the boards a tool call writes, for the check at the end of the turn.
func (c *cli) noteBoardWrites(writes []design.BoardWrite) {
	file := c.strictPendingFile()
	var have []string
	if raw, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(raw, &have)
	}
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range writes {
		set[w.Project+"\x00"+w.File] = true
	}
	have = have[:0]
	for k := range set {
		have = append(have, k)
	}
	sort.Strings(have)
	raw, _ := json.Marshal(have)
	if os.MkdirAll(filepath.Dir(file), 0o755) == nil {
		_ = os.WriteFile(file, raw, 0o644)
	}
}

// strictStopReminder runs the strict check on the boards written in the turn and, on a finding, answers the Stop hook
// with a block and the findings. Anything that goes wrong lets the turn end.
func (c *cli) strictStopReminder() {
	file := c.strictPendingFile()
	raw, err := os.ReadFile(file)
	if err != nil {
		return
	}
	_ = os.Remove(file)
	var in struct {
		Active bool `json:"stop_hook_active"`
	}
	if r := c.env.Stdin; r != nil {
		if b, err := io.ReadAll(io.LimitReader(r, 1<<20)); err == nil {
			_ = json.Unmarshal(b, &in)
		}
	}
	if in.Active {
		return
	}
	var have []string
	if json.Unmarshal(raw, &have) != nil {
		return
	}
	dir, err := c.libraryDir("")
	if err != nil {
		return
	}
	l := c.localLibrary(dir, "")
	byProject := map[string][]string{}
	var order []string
	for _, h := range have {
		p, f, ok := strings.Cut(h, "\x00")
		if !ok {
			continue
		}
		if _, seen := byProject[p]; !seen {
			order = append(order, p)
		}
		byProject[p] = append(byProject[p], f)
	}
	var lines []string
	total := 0
	for _, p := range order {
		if l.StrictOn(p) == "" {
			continue
		}
		rep, err := l.CheckStrict(p, byProject[p])
		if err != nil || len(rep.Findings) == 0 {
			continue
		}
		total += len(rep.Findings)
		for i, f := range rep.Findings {
			if i == 8 {
				lines = append(lines, fmt.Sprintf("… and %d more", len(rep.Findings)-8))
				break
			}
			lines = append(lines, fmt.Sprintf("%s: %s:%d:%d %s — %s Fix: %s", p, f.File, f.Line, f.Col, f.Rule, f.What, f.Fix))
		}
	}
	if total == 0 {
		return
	}
	reason := fmt.Sprintf("The work is not done: `design check <project> --strict` finds %d problem(s) in the boards of this turn. "+
		"Fix them (use the kit's components and tokens, a block of your own goes into a local component) and run the check again until it says ok. "+
		"Работа не готова, пока `design check --strict` не даёт ok: поправь и повтори.\n%s", total, strings.Join(lines, "\n"))
	out, _ := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	c.out("%s\n", out)
}
