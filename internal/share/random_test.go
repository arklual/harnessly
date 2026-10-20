package share

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// SHARE_RANDOM points at cases the generator made (GEN_RANDOM): redaction, capture, transcript economics and the page
// and Markdown of random snapshots must come out as the previous runtime's.
func TestGeneratedCases(t *testing.T) {
	file := os.Getenv("SHARE_RANDOM")
	if file == "" {
		t.Skip("SHARE_RANDOM not set")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Cases []json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	g := readGolden(t)
	page := Page{Brand: g.Neutral.Brand, DropHostSuffix: os.Getenv("SHARE_DROP_HOST"), DropPathPrefix: "/agents"}
	failed := map[string]int{}
	total := map[string]int{}
	fail := func(i int, kind, what, got, want string) {
		failed[kind]++
		if failed[kind] <= 40 {
			a, b := got, want
			k := 0
			for k < len(a) && k < len(b) && a[k] == b[k] {
				k++
			}
			from := max(0, k-150)
			t.Errorf("case %d %s %s\n got …%s\nwant …%s", i, kind, what, clip(a[from:], 400), clip(b[from:], 400))
		}
	}
	for i, rawCase := range set.Cases {
		v, err := jsvalue.Parse(rawCase)
		if err != nil {
			t.Fatal(err)
		}
		c := v.(*jsvalue.Object)
		kind := c.String("kind")
		total[kind]++
		redactor := func() *Redactor {
			ctx := c.Object("ctx")
			ws := ""
			if w, ok := ctx.Get("workspaceDir"); ok && w != nil {
				ws = w.(string)
			}
			return NewRedactor(RedactorConfig{KnownSecrets: jsvalue.Strings(ctx.Array("knownSecrets")), HomeDirs: jsvalue.Strings(ctx.Array("homeDirs")),
				WorkspaceDir: ws, ProfileHomes: g.Neutral.ProfileHomes})
		}
		switch kind {
		case "redact":
			r := redactor()
			want := c.Array("outputs")
			for j, in := range jsvalue.Strings(c.Array("inputs")) {
				if got := r.Redact(in); got != want[j] {
					fail(i, kind, "output", got, want[j].(string))
				}
			}
			if got, want := string(jsvalue.Stringify(r.Report(), "")), string(jsvalue.Stringify(get(c, "report"), "")); got != want {
				fail(i, kind, "report", got, want)
			}
		case "capture":
			opts, _ := ParseOptions(get(c, "options"))
			var timeline []*jsvalue.Object
			for _, e := range c.Array("timeline") {
				timeline = append(timeline, e.(*jsvalue.Object))
			}
			input := c.Object("input")
			snapshot, report, ok := Capture(CaptureInput{Timeline: timeline, Provider: input.String("provider"), Title: input.String("title"),
				OwnerSub: get(input, "sub"), Options: opts, Economics: c.Object("economics"), Redactor: redactor(), Now: num(input, "now")})
			got := jsvalue.NewObject("ok", false)
			if ok {
				got = jsvalue.NewObject("ok", true, "snapshot", snapshot, "report", report)
			}
			want := c.Object("result")
			if !truthy(get(want, "ok")) {
				want = jsvalue.NewObject("ok", false)
			}
			if a, b := string(jsvalue.Stringify(got, "")), string(jsvalue.Stringify(want, "")); a != b {
				fail(i, kind, "capture", a, b)
			}
		case "economics":
			e, ok := TranscriptEconomics(c.String("content"), c.String("provider"))
			got := "THROWS"
			if ok {
				got = string(jsvalue.Stringify(e, ""))
			}
			want := string(jsvalue.Stringify(get(c, "result"), ""))
			if s, isStr := get(c, "result").(string); isStr {
				want = s
			}
			if got != want {
				fail(i, kind, "economics", got, want)
			}
		case "render":
			s := c.Object("snapshot")
			o := RenderOptions{Mode: c.String("mode")}
			if v, ok := c.Get("viewer"); ok && v != nil {
				o.Viewer = v.(string)
			}
			if v, ok := c.Get("expiresAt"); ok && v != nil {
				o.ExpiresAt = v.(float64)
			}
			if html := c.String("html"); !strings.HasPrefix(html, "THROWS") {
				if got := page.RenderHTML(s, redactor(), o); got != html {
					fail(i, kind, "html", got, html)
				}
				if got := page.RenderMarkdown(s, redactor()); got != c.String("md") {
					fail(i, kind, "md", got, c.String("md"))
				}
			}
		}
	}
	for k, n := range total {
		t.Logf("%s: %d cases, %d differ", k, n, failed[k])
	}
}
