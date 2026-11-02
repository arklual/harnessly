package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
)

// The checks of the apps on in a document. A check either carries rules the host evaluates over the document's nodes, or
// names a tool of the app's own MCP server, which is given the nodes and answers with findings. A finding is
// { node, level, message, fix? }; the findings of a check become marks on the board (replacing the check's earlier
// ones), and the worst level of a check with blockShare decides whether the document may be shared.

// Finding is one thing a check found.
type Finding struct {
	Node    string `json:"node"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Rule    string `json:"rule,omitempty"`
	// Fix is a patch (a list of ops) that mends the finding, when the check knows one.
	Fix any `json:"fix,omitempty"`
}

// CheckResult is one check run.
type CheckResult struct {
	App      string    `json:"app"`
	AppName  string    `json:"appName"`
	Check    string    `json:"check"`
	Title    string    `json:"title,omitempty"`
	Findings []Finding `json:"findings"`
	// Blocks is true when the check keeps the document from being shared as it is.
	Blocks bool `json:"blocks"`
	// Failed says why the check could not run, in words for the person; its findings are then empty.
	Failed string `json:"failed,omitempty"`
}

func levelRank(l string) int {
	switch l {
	case LevelError:
		return 3
	case LevelWarning:
		return 2
	case LevelInfo:
		return 1
	}
	return 0
}

// blocks says whether any finding reaches the level that blocks sharing.
func blocks(blockShare string, findings []Finding) bool {
	if blockShare == "" {
		return false
	}
	for _, f := range findings {
		if levelRank(f.Level) >= levelRank(blockShare) {
			return true
		}
	}
	return false
}

// CheckSource is what marks of a check are kept under.
func CheckSource(app, check string) string { return "check:" + app + ":" + check }

// ActiveInDocument are the installed apps that are on in a document of a kind.
func ActiveInDocument(list []Installed, kind string) []Installed {
	var out []Installed
	for _, a := range list {
		if a.Manifest != nil && a.Manifest.OnInDocument(kind) {
			out = append(out, a)
		}
	}
	return out
}

// LocksUnbound says whether any app on in the document keeps free hand edits to its bound elements.
func LocksUnbound(list []Installed, kind string) bool {
	for _, a := range ActiveInDocument(list, kind) {
		if s, err := a.Manifest.SinopiaOf(); err == nil && s.Bindings != nil && s.Bindings.LockUnbound {
			return true
		}
	}
	return false
}

// RunChecks runs the checks of the apps on in the document that run on a trigger, keeps their findings as marks and
// returns each check's result. The tools are called with ctx; a check that cannot run says so and finds nothing.
func RunChecks(ctx context.Context, docs SinopiaDocs, state *SinopiaState, project, kind, trigger string, active []Installed, pkgOf func(id string) *Package) []CheckResult {
	var out []CheckResult
	for _, a := range active {
		s, err := a.Manifest.SinopiaOf()
		if err != nil {
			continue
		}
		for _, c := range s.ForKind(kind).Checks {
			if !contains(c.On, trigger) {
				continue
			}
			res := CheckResult{App: a.ID, AppName: a.Manifest.Name, Check: c.ID, Title: c.Title, Findings: []Finding{}}
			var findings []Finding
			if len(c.Rules) > 0 {
				findings, err = runRules(docs, project, c.Rules)
			} else {
				findings, err = runToolCheck(ctx, docs, pkgOf(a.ID), project, kind, trigger, c)
			}
			if err != nil {
				res.Failed = err.Error()
			} else {
				res.Findings = findings
				res.Blocks = blocks(c.BlockShare, findings)
			}
			marks := make([]Mark, len(res.Findings))
			for i, f := range res.Findings {
				marks[i] = Mark{Node: f.Node, Level: f.Level, Note: f.Message, Rule: f.Rule, Fix: f.Fix}
			}
			if res.Failed == "" {
				state.AddMarks(project, CheckSource(a.ID, c.ID), true, marks)
			}
			out = append(out, res)
		}
	}
	state.SetResults(project, out)
	return out
}

// runRules evaluates declarative rules over the document.
func runRules(docs SinopiaDocs, project string, rules []Rule) ([]Finding, error) {
	findings := []Finding{}
	for i, r := range rules {
		nodes, err := docs.Query(project, r.Select, "")
		if err != nil {
			return nil, err
		}
		level := r.Level
		if level == "" {
			level = LevelWarning
		}
		id := r.ID
		if id == "" {
			id = "rule-" + strconv.Itoa(i+1)
		}
		for _, n := range nodes {
			if n.Own == "" && r.MinFontPx == 0 {
				continue
			}
			var value, limit float64
			switch {
			case r.MaxWords > 0 && len(strings.Fields(n.Own)) > r.MaxWords:
				value, limit = float64(len(strings.Fields(n.Own))), float64(r.MaxWords)
			case r.MaxChars > 0 && len([]rune(n.Own)) > r.MaxChars:
				value, limit = float64(len([]rune(n.Own))), float64(r.MaxChars)
			case r.MinFontPx > 0 && n.Own != "":
				px, ok := fontPx(n.Style, n.BoardW)
				if !ok || px >= r.MinFontPx {
					continue
				}
				value, limit = px, r.MinFontPx
			default:
				continue
			}
			f := Finding{Node: n.ID, Level: level, Rule: id, Message: fill(r.Message, value, limit)}
			if len(r.Fix) > 0 {
				style := map[string]string{}
				for k, v := range r.Fix {
					style[k] = v
				}
				f.Fix = []map[string]any{{"op": "setStyle", "node": n.ID, "fp": n.Fingerprint, "style": style}}
			}
			findings = append(findings, f)
		}
	}
	return findings, nil
}

func fill(msg string, n, limit float64) string {
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return strings.NewReplacer("{n}", num(float64(int(n*10+0.5))/10), "{limit}", num(limit)).Replace(msg)
}

var fontSizeRe = regexp.MustCompile(`(?i)(?:^|;)\s*font-size\s*:\s*([0-9.]+)\s*(px|pt|rem|em|cqw|vw)\b`)

// fontPx is the inline font-size of a style in px; boardW (0 unknown, then 1920) is what cqw and vw follow.
func fontPx(style string, boardW int) (float64, bool) {
	m := fontSizeRe.FindStringSubmatch(style)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	if boardW <= 0 {
		boardW = 1920
	}
	switch strings.ToLower(m[2]) {
	case "px":
		return v, true
	case "pt":
		return v * 4 / 3, true
	case "rem", "em":
		return v * 16, true
	case "cqw", "vw":
		return v * float64(boardW) / 100, true
	}
	return 0, false
}

// maxCheckNodes is how many nodes a check's tool is given.
const maxCheckNodes = 1500

// runToolCheck calls the check's tool with the document and its nodes.
func runToolCheck(ctx context.Context, docs SinopiaDocs, pkg *Package, project, kind, trigger string, c SinopiaCheck) ([]Finding, error) {
	if pkg == nil {
		return nil, fmt.Errorf("the app's package could not be read")
	}
	server, tool, _ := strings.Cut(c.Tool, ".")
	srv, ok := pkg.MCP[server]
	if !ok {
		return nil, fmt.Errorf("the app has no MCP server %s", server)
	}
	doc, err := docs.Document(project)
	if err != nil {
		return nil, err
	}
	nodes, err := docs.Nodes(project, "")
	if err != nil {
		return nil, err
	}
	if len(nodes) > maxCheckNodes {
		nodes = nodes[:maxCheckNodes]
	}
	raw, err := CallTool(ctx, srv, tool, map[string]any{"trigger": trigger, "document": doc, "nodes": nodes})
	if err != nil {
		return nil, fmt.Errorf("the check's tool did not answer")
	}
	return parseFindings(raw)
}

// parseFindings reads a tool result: structuredContent.findings, or a JSON text in the first content part, either a
// list of findings or { findings: [...] }.
func parseFindings(raw json.RawMessage) ([]Finding, error) {
	var res struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			Findings []Finding `json:"findings"`
		} `json:"structuredContent"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.IsError {
		return nil, fmt.Errorf("the check's tool failed")
	}
	findings := res.StructuredContent.Findings
	if findings == nil && len(res.Content) > 0 {
		text := []byte(res.Content[0].Text)
		var list []Finding
		var obj struct {
			Findings []Finding `json:"findings"`
		}
		switch {
		case json.Unmarshal(text, &list) == nil:
			findings = list
		case json.Unmarshal(text, &obj) == nil:
			findings = obj.Findings
		default:
			return nil, fmt.Errorf("the check's tool answered with something other than findings")
		}
	}
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if _, _, ok := design.ParseDocNodeID(f.Node); !ok || strings.TrimSpace(f.Message) == "" {
			continue
		}
		if levelRank(f.Level) == 0 {
			f.Level = LevelWarning
		}
		f.Message = clipText(f.Message, maxNote)
		out = append(out, f)
		if len(out) >= maxMarksSource {
			break
		}
	}
	return out, nil
}
