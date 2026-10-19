package machines

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"harnessly/internal/jsvalue"
	"harnessly/internal/nodejson"
)

// testdata/stop.json: what the previous runtime's stopper answered (testdata/gen/stop.mjs) on the same /proc with the
// same signals, and what its request reader made of generated bodies.

type stopCase struct {
	Request struct {
		PID       int    `json:"pid"`
		StartTime string `json:"starttime"`
	} `json:"request"`
	Plan struct {
		Term  *string `json:"term"`
		After *int    `json:"after"`
		End   string  `json:"end"`
		Kill  *string `json:"kill"`
	} `json:"plan"`
	Calls  []string        `json:"calls"`
	Polls  int             `json:"polls"`
	Result json.RawMessage `json:"result"`
}

var reusedStart = regexp.MustCompile(` 0 (\d+) 100000`)

func TestStopAsTheOldRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/stop.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		UID         int                 `json:"uid"`
		ReleaseRoot string              `json:"releaseRoot"`
		Tree        map[string]any      `json:"tree"`
		Listed      []map[string]any    `json:"listed"`
		Bodies      [][]json.RawMessage `json:"bodies"`
		Cases       []stopCase          `json:"cases"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	for _, b := range g.Bodies {
		var text string
		_ = json.Unmarshal(b[0], &text)
		body, _ := jsvalue.Parse([]byte(text))
		req, ok := ParseStopRequest(body)
		got := "null"
		if ok {
			got = string(nodejson.Marshal(map[string]any{"pid": req.PID, "starttime": req.StartTime}))
		}
		var want any
		_ = json.Unmarshal(b[1], &want)
		if w := string(nodejson.Marshal(want)); got != w {
			t.Errorf("body %s: got %s, want %s", text, got, w)
		}
	}
	var listed []LoadProcess
	for _, l := range g.Listed {
		p := LoadProcess{PID: int(l["pid"].(float64)), StartTime: Num(l["starttime"].(float64))}
		if port, ok := l["port"].(float64); ok {
			n := int(port)
			p.Port = &n
		}
		listed = append(listed, p)
	}
	procDir := filepath.Join(t.TempDir(), "proc")
	for i, c := range g.Cases {
		_ = os.RemoveAll(procDir)
		for name, content := range g.Tree {
			p := filepath.Join(procDir, name)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if link, ok := content.(map[string]any); ok {
				_ = os.Symlink(link["link"].(string), p)
			} else {
				_ = os.WriteFile(p, []byte(content.(string)), 0o644)
			}
		}
		end := func() {
			stat := filepath.Join(procDir, strconv.Itoa(c.Request.PID), "stat")
			text, _ := os.ReadFile(stat)
			switch c.Plan.End {
			case "gone":
				_ = os.Remove(stat)
			case "zombie":
				_ = os.WriteFile(stat, []byte(strings.Replace(string(text), ") S ", ") Z ", 1)), 0o644)
			default:
				_ = os.WriteFile(stat, []byte(reusedStart.ReplaceAllString(string(text), " 0 7$1 100000")), 0o644)
			}
		}
		var calls []string
		polls, termed := 0, false
		s := &Stopper{
			Listed: func() ([]LoadProcess, error) { return listed, nil }, UID: g.UID, ReleaseRoot: g.ReleaseRoot, ProcRoot: procDir,
			RuntimePIDs: func() []int { return []int{1000} }, PanePIDs: func() []int { return []int{1501, 1502} },
			Kill: func(pid int, sig syscall.Signal) error {
				outcome := c.Plan.Kill
				if sig == syscall.SIGTERM {
					calls = append(calls, "SIGTERM")
					outcome = c.Plan.Term
				} else {
					calls = append(calls, "SIGKILL")
					if outcome == nil {
						sent := "sent"
						outcome = &sent
					}
				}
				switch {
				case outcome != nil && *outcome == "gone":
					return syscall.ESRCH
				case outcome != nil && *outcome == "denied":
					return syscall.EPERM
				case sig == syscall.SIGTERM && outcome != nil:
					termed = true
					if c.Plan.After != nil && *c.Plan.After == 0 {
						end()
					}
				}
				return nil
			},
			Sleep: func(time.Duration) {
				polls++
				if termed && c.Plan.After != nil && polls == *c.Plan.After {
					end()
				}
			},
		}
		res, err := s.Stop(StopRequest{PID: c.Request.PID, StartTime: c.Request.StartTime})
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		var plain any
		_ = json.Unmarshal(nodejson.Marshal(map[string]any{"status": res.Status, "body": res.Body}), &plain)
		got := string(nodejson.Marshal(plain))
		var want any
		_ = json.Unmarshal(c.Result, &want)
		if w := string(nodejson.Marshal(want)); got != w || strings.Join(calls, ",") != strings.Join(c.Calls, ",") || polls != c.Polls {
			t.Errorf("case %d %+v: got %s calls %v polls %d, want %s calls %v polls %d", i, c.Request, got, calls, polls, w, c.Calls, c.Polls)
		}
	}
}
