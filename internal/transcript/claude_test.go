package transcript

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The golden cases are the previous runtime's own parser run on the same bytes (testdata/claude/gen), ids aside: its
// random ids became the ones made of the transcript's bytes, and HARNESSLY_UPDATE_GOLDEN=1 rewrote the file;
// every case had been checked equal id-blind first. The mass runs below (CLAUDE_TRANSCRIPT_MASS) still carry the old
// counter's ids and cannot pass until the generator is run again.

type claudeGolden struct {
	Now   float64 `json:"now"`
	Dir   string  `json:"dir"`
	Built map[string]struct {
		Parts  []string `json:"parts"`
		Repeat int      `json:"repeat"`
	} `json:"built"`
	Cases []claudeCase `json:"cases"`
}

type claudeCase struct {
	File   string  `json:"file"`
	Op     string  `json:"op"`
	From   *int64  `json:"from"`
	To     *int64  `json:"to"`
	Cut    *int64  `json:"cut"`
	Max    int     `json:"max"`
	Before int64   `json:"before"`
	Steps  []int64 `json:"steps"`
	Hash   string  `json:"hash"`
	JSON   *string `json:"json"`
}

// claudeDeterministic fixes the clock and the zone. Entry ids need nothing: they come from the transcript's own bytes.
func claudeDeterministic(t *testing.T, now float64) {
	t.Helper()
	oldNow, oldLocal := claudeNow, time.Local
	claudeNow = func() float64 { return now }
	time.Local = time.UTC
	t.Cleanup(func() { claudeNow, time.Local = oldNow, oldLocal })
}

func chunkJSON(c Chunk) *jsvalue.Object {
	o := jsvalue.NewObject("entries", entryArray(c.Entries), "startByteOffset", c.StartByteOffset, "fileSize", c.FileSize,
		"hasMore", c.HasMore, "errorCount", c.ErrorCount)
	if c.Summary != nil {
		o.Set("summary", *c.Summary)
	}
	if c.CustomTitle != nil {
		o.Set("customTitle", *c.CustomTitle)
	}
	return o
}

func runClaudeCase(t *testing.T, c claudeCase, data []byte, path string) string {
	write := func(cut *int64) {
		b := data
		if cut != nil {
			b = data[:min(*cut, int64(len(data)))]
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	str := func(v any) string { return string(jsvalue.Stringify(v, "")) }
	switch c.Op {
	case "content":
		from, to := int64(0), int64(len(data))
		if c.From != nil {
			from = *c.From
		}
		if c.To != nil {
			to = *c.To
		}
		var text string
		if to > from {
			text = claudeDecode(data[from:to])
		}
		entries := ParseContent(text)
		if entries == nil {
			return "throw"
		}
		return str(entryArray(entries))
	case "tail":
		write(c.Cut)
		return str(chunkJSON(ReadTail(path, c.Max)))
	case "before":
		write(c.Cut)
		return str(chunkJSON(ReadBefore(path, c.Before, c.Max)))
	case "file":
		write(c.Cut)
		return str(entryArray(ParseFile(path)))
	case "inc":
		var steps []any
		var offset int64
		pending := ""
		for _, cut := range c.Steps {
			write(&cut)
			r := ParseIncremental(path, offset, pending)
			steps = append(steps, jsvalue.NewObject("newEntries", entryArray(r.NewEntries), "newOffset", r.NewOffset,
				"pendingBuffer", r.PendingBuffer))
			offset, pending = r.NewOffset, r.PendingBuffer
		}
		return str(steps)
	}
	t.Fatalf("unknown op %q", c.Op)
	return ""
}

func checkClaudeGolden(t *testing.T, goldenPath, dir string) {
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var g claudeGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if g.Dir != "" {
		dir = g.Dir
	}
	claudeDeterministic(t, g.Now)
	files := map[string][]byte{}
	load := func(name string) []byte {
		if b, ok := files[name]; ok {
			return b
		}
		var b []byte
		if built, ok := g.Built[name]; ok {
			var once bytes.Buffer
			for _, p := range built.Parts {
				part, err := os.ReadFile(filepath.Join(dir, p))
				if err != nil {
					t.Fatal(err)
				}
				once.Write(part)
			}
			b = bytes.Repeat(once.Bytes(), built.Repeat)
		} else if b, err = os.ReadFile(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		files[name] = b
		return b
	}
	var full []string
	if p := os.Getenv("CLAUDE_GOLDEN_FULL"); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &full)
		}
	}
	path := filepath.Join(t.TempDir(), "case.jsonl")
	update := updatingGolden()
	doc, err := jsvalue.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	cases := doc.(*jsvalue.Object).Array("cases")
	failed := 0
	for i, c := range g.Cases {
		got := runClaudeCase(t, c, load(c.File), path)
		if update {
			if c.Op == "file" && c.Hash == claudeThrowHash && got == "[]" {
				continue
			}
			sum := sha256.Sum256([]byte(got))
			o := cases[i].(*jsvalue.Object)
			o.Set("hash", hex.EncodeToString(sum[:]))
			if jsLength(got) <= claudeKeepJSON {
				o.Set("json", got)
			} else {
				o.Delete("json")
			}
			continue
		}
		if c.Op == "file" && c.Hash == claudeThrowHash && got == "[]" {
			// parseJsonlFile rejects where its tail read throws (nothing awaits it); ParseFile is empty there.
			continue
		}
		sum := sha256.Sum256([]byte(got))
		if hex.EncodeToString(sum[:]) == c.Hash {
			continue
		}
		failed++
		if failed > 10 {
			continue
		}
		want := "(hash only)"
		if c.JSON != nil {
			want = *c.JSON
		} else if i < len(full) {
			want = full[i]
		}
		t.Errorf("case %d %s %s cut=%v from=%v to=%v max=%d before=%d steps=%v:\n%s", i, c.Op, c.File, deref(c.Cut), deref(c.From),
			deref(c.To), c.Max, c.Before, c.Steps, firstDifference(got, want))
	}
	if update {
		writeGolden(t, goldenPath, doc)
		return
	}
	if failed > 0 {
		t.Errorf("%d of %d cases differ", failed, len(g.Cases))
	}
	t.Logf("%d cases", len(g.Cases))
}

// claudeKeepJSON is how long (in UTF-16 units) an answer may be to stay in the golden file whole beside its hash.
const claudeKeepJSON = 1500

var claudeThrowHash = func() string { s := sha256.Sum256([]byte("throw")); return hex.EncodeToString(s[:]) }()

func deref(p *int64) any {
	if p == nil {
		return "-"
	}
	return *p
}

func firstDifference(got, want string) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	from := max(0, i-300)
	cut := func(s string) string { return s[min(from, len(s)):min(len(s), i+300)] }
	return "  got:  …" + cut(got) + "\n  want: …" + cut(want)
}

func TestClaudeGolden(t *testing.T) {
	checkClaudeGolden(t, filepath.Join("testdata", "claude", "golden.json"), filepath.Join("testdata", "claude"))
}

// TestClaudeMass replays a mass run of the generator (MASS_DIR, uncommitted) when CLAUDE_TRANSCRIPT_MASS names it.
func TestClaudeMass(t *testing.T) {
	p := os.Getenv("CLAUDE_TRANSCRIPT_MASS")
	if p == "" {
		t.Skip("CLAUDE_TRANSCRIPT_MASS is not set")
	}
	checkClaudeGolden(t, p, "")
}

func TestClaudeDecode(t *testing.T) {
	cases := map[string]string{
		"ok Ж":                 "ok Ж",
		"\xe2\x82A":            "\uFFFDA",
		"\xff\xfe":             "\uFFFD\uFFFD",
		"\xf0\x9f\x98":         "\uFFFD",
		"\xed\xa0\x80":         "\uFFFD\uFFFD\uFFFD",
		"\xe0\x80\xaf":         "\uFFFD\uFFFD\uFFFD",
		"\xf4\x90\x80\x80":     "\uFFFD\uFFFD\uFFFD\uFFFD",
		"\x80\x80\n":           "\uFFFD\uFFFD\n",
		"\xc3\x28":             "\uFFFD(",
		"\xf0\x9f\x98\x80\xc3": "😀\uFFFD",
	}
	for in, want := range cases {
		if got := claudeDecode([]byte(in)); got != want {
			t.Errorf("claudeDecode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClaudeParseJSON(t *testing.T) {
	lines := []string{`{"a":1,"b":[true,false,null,-0.5e3,"xé😀\ud800A\"\\\/\b\f\n\r\t"],"a":{"c":{}}}`,
		` [ ] `, `"s"`, `1e999`, `-0`, `{"__proto__":1,"2":3,"1":4}`, `01`, `1.`, `.5`, `-`, `{"a":1,}`, `[1,]`, `{"a" 1}`, "\"\x01\"",
		`"\x"`, `"\u12"`, `tru`, `nul`, `{}x`, ``, `"unterminated`, `{"a":"b"`, `[1 2]`, `"\ud83d"`, `1E+2`, `{"k":"v","k":"w"}`}
	files, _ := filepath.Glob(filepath.Join("testdata", "claude", "*.jsonl"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		lines = append(lines, strings.Split(claudeDecode(b), "\n")...)
	}
	for _, l := range lines {
		want, wantErr := jsvalue.Parse([]byte(l))
		got, gotErr := claudeParseJSON(l)
		if (wantErr != nil) != (gotErr != nil) || wantErr == nil && !jsvalue.Equal(got, want) {
			t.Errorf("claudeParseJSON(%.80q) = %v, %v; want %v, %v", l, got, gotErr, want, wantErr)
		}
	}
}
