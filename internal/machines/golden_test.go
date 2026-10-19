package machines

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/limits"
	"harnessly/internal/nodejson"
)

// testdata/golden.json holds the previous runtime's answers on generated inputs: its own code run under node by
// testdata/gen. A TS_GOLDEN environment variable points the tests at a larger set made the same way.

type golden struct {
	Fixed     float64 `json:"fixed"`
	Forecasts []struct {
		Kind    string       `json:"kind"`
		Now     float64      `json:"now"`
		Samples [][3]float64 `json:"samples"`
		AsOf    *float64     `json:"asOf"`
		Events  [][2]float64 `json:"events"`
		Result  string       `json:"result"`
	} `json:"forecasts"`
	Limits []struct {
		Now     float64           `json:"now"`
		Raw     string            `json:"raw"`
		History []string          `json:"history"`
		AsOf    *float64          `json:"asOf"`
		Events  [][2]float64      `json:"events"`
		Result  map[string]string `json:"result"`
	} `json:"limits"`
	HistorySteps []struct {
		Op      string  `json:"op"`
		Text    string  `json:"text"`
		Now     float64 `json:"now"`
		Samples string  `json:"samples"`
	} `json:"historySteps"`
	Transcripts []struct {
		AsOf   float64           `json:"asOf"`
		Files  map[string]string `json:"files"`
		Claude *activityDigest   `json:"claude"`
		Codex  *activityDigest   `json:"codex"`
	} `json:"transcripts"`
	Redact   [][2]string `json:"redact"`
	Agents   [][3]any    `json:"agents"`
	Units    [][2]any    `json:"units"`
	Services []struct {
		Input struct {
			Cgroup      *string `json:"cgroup"`
			Exe         *string `json:"exe"`
			Args        string  `json:"args"`
			Cwd         *string `json:"cwd"`
			RuntimeUnit *string `json:"runtimeUnit"`
		}
		Result bool
	} `json:"-"`
	RawServices [][2]json.RawMessage `json:"services"`
	Load        loadGolden           `json:"load"`
	API         apiGolden            `json:"api"`
}

type activityDigest struct {
	Start   float64 `json:"start"`
	FirstAt float64 `json:"firstAt"`
	AsOf    float64 `json:"asOf"`
	Slots   int     `json:"slots"`
	Cum     string  `json:"cum"`
	Total   float64 `json:"total"`
}

func loadGoldenFile(t *testing.T) golden {
	t.Helper()
	path := os.Getenv("TS_GOLDEN")
	if path == "" {
		path = "testdata/golden.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	for _, pair := range g.RawServices {
		var s struct {
			Input struct {
				Cgroup      *string `json:"cgroup"`
				Exe         *string `json:"exe"`
				Args        string  `json:"args"`
				Cwd         *string `json:"cwd"`
				RuntimeUnit *string `json:"runtimeUnit"`
			}
			Result bool
		}
		_ = json.Unmarshal(pair[0], &s.Input)
		_ = json.Unmarshal(pair[1], &s.Result)
		g.Services = append(g.Services, s)
	}
	return g
}

const goldenTZ = 3 * 3600

func samplesOf(rows [][3]float64) []limits.Sample {
	out := make([]limits.Sample, len(rows))
	for i, r := range rows {
		out[i] = limits.Sample{T: r[0], U: r[1], R: r[2]}
	}
	return out
}

func TestGoldenForecasts(t *testing.T) {
	g := loadGoldenFile(t)
	bad := 0
	for i, c := range g.Forecasts {
		spec := FiveHour
		if c.Kind == "seven_day" {
			spec = SevenDay
		}
		var a *Activity
		if c.AsOf != nil {
			a = BuildActivity(c.Events, *c.AsOf, goldenTZ)
		}
		got := string(nodejson.Marshal(ForecastWindow(samplesOf(c.Samples), c.Now, spec, a)))
		if got != c.Result {
			bad++
			if bad <= 5 {
				t.Errorf("forecast %d (%s):\n got %s\nwant %s", i, c.Kind, got, c.Result)
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d forecasts differ", bad, len(g.Forecasts))
	}
}

func TestGoldenProviderLimits(t *testing.T) {
	g := loadGoldenFile(t)
	dir := t.TempDir()
	bad := 0
	for i, c := range g.Limits {
		file := filepath.Join(dir, "h.jsonl")
		_ = os.WriteFile(file, []byte(strings.Join(c.History, "\n")+"\n"), 0o600)
		records := (&limits.HistoryReader{File: file}).Read(c.Now)
		raw, err := jsvalue.Parse([]byte(c.Raw))
		if err != nil {
			t.Fatal(err)
		}
		var a *Activity
		if c.AsOf != nil {
			a = BuildActivity(c.Events, *c.AsOf, goldenTZ)
		}
		for _, p := range []string{"claude", "codex"} {
			act := a
			if p != "claude" {
				act = nil
			}
			samples := func(id string) []limits.Sample { return limits.SamplesOf(records, p, id) }
			lim := BuildProviderLimits(getOr(raw, p), samples, c.Now, act)
			var got string
			if lim == nil {
				got = "null"
			} else {
				got = string(nodejson.Marshal(lim))
			}
			if got != c.Result[p] {
				bad++
				if bad <= 5 {
					t.Errorf("limits %d %s:\n got %s\nwant %s", i, p, got, c.Result[p])
				}
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d provider limits differ", bad)
	}
}

func TestGoldenHistoryReader(t *testing.T) {
	g := loadGoldenFile(t)
	file := filepath.Join(t.TempDir(), "live.jsonl")
	h := &limits.HistoryReader{File: file}
	for i, s := range g.HistorySteps {
		switch s.Op {
		case "write", "truncate":
			_ = os.WriteFile(file, []byte(s.Text), 0o600)
		case "append":
			f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
			_, _ = f.WriteString(s.Text)
			f.Close()
		case "replace":
			_ = os.WriteFile(file+".new", []byte(s.Text), 0o600)
			_ = os.Rename(file+".new", file)
		}
		samples := limits.SamplesOf(h.Read(s.Now), "claude", "five_hour")
		out := make([]any, len(samples))
		for k, x := range samples {
			out[k] = jsvalue.NewObject("t", x.T, "u", x.U, "r", x.R)
		}
		if got := string(jsvalue.Stringify(out, "")); got != s.Samples {
			t.Errorf("step %d (%s): got %s want %s", i, s.Op, got, s.Samples)
		}
	}
}

func digest(a *Activity) *activityDigest {
	if a == nil {
		return nil
	}
	parts := make([]string, len(a.Cum))
	for i, x := range a.Cum {
		parts[i] = jscompat.Number(x)
	}
	sum := sha1.Sum([]byte("[" + strings.Join(parts, ",") + "]"))
	return &activityDigest{Start: a.Start, FirstAt: a.FirstAt, AsOf: a.AsOf, Slots: len(a.Cum), Cum: hex.EncodeToString(sum[:]), Total: a.Cum[len(a.Cum)-1]}
}

func TestGoldenTranscriptActivity(t *testing.T) {
	g := loadGoldenFile(t)
	for i, c := range g.Transcripts {
		base := t.TempDir()
		for name, content := range c.Files {
			file := filepath.Join(base, name)
			_ = os.MkdirAll(filepath.Dir(file), 0o755)
			_ = os.WriteFile(file, []byte(content), 0o600)
		}
		u := &Usage{ClaudeRoot: filepath.Join(base, "claude", "projects"), CodexRoot: filepath.Join(base, "codex", "sessions"), TZOffset: goldenTZ,
			Now: func() time.Time { return time.UnixMilli(int64(c.AsOf * 1000)) }}
		for pass := 0; pass < 2; pass++ { // the second pass reads nothing new and must give the same history
			acts := u.Build()
			for _, p := range []struct {
				name string
				want *activityDigest
			}{{"claude", c.Claude}, {"codex", c.Codex}} {
				got, _ := json.Marshal(digest(acts[p.name]))
				want, _ := json.Marshal(p.want)
				if string(got) != string(want) {
					t.Errorf("transcripts %d pass %d %s:\n got %s\nwant %s", i, pass, p.name, got, want)
				}
			}
		}
	}
}

func TestGoldenClassifiers(t *testing.T) {
	g := loadGoldenFile(t)
	bad := 0
	for _, c := range g.Redact {
		if got := redactCommand(c[0]); got != c[1] {
			bad++
			if bad <= 10 {
				t.Errorf("redact %q:\n got %q\nwant %q", c[0], got, c[1])
			}
		}
	}
	for _, c := range g.Agents {
		if got := isAgentProcess(c[0].(string), c[1].(string)); got != c[2].(bool) {
			t.Errorf("agent %q %q: got %v", c[0], c[1], got)
		}
	}
	for _, c := range g.Units {
		want, _ := c[1].(string)
		if got := systemServiceUnit(c[0].(string)); got != want {
			t.Errorf("unit %q: got %q want %q", c[0], got, want)
		}
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for _, c := range g.Services {
		units := map[string]bool{}
		if c.Input.RuntimeUnit != nil {
			units[*c.Input.RuntimeUnit] = true
		}
		in := serviceInput{cgroup: str(c.Input.Cgroup), exe: str(c.Input.Exe), args: c.Input.Args, cwd: str(c.Input.Cwd), runtimeUnits: units}
		if got := isSystemService(in, goldenReleaseRoot); got != c.Result {
			t.Errorf("service %+v: got %v", c.Input, got)
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d redactions differ", bad, len(g.Redact))
	}
}

const goldenReleaseRoot = "/opt/acme-ai/"
