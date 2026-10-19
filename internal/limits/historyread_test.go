package limits

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// slowRecord is parseRecord without its fast path.
func slowRecord(line string) (Record, bool) {
	saved := line
	r, ok := parseRecordGeneral(saved)
	return r, ok
}

func normalized(r Record) Record {
	sort.Slice(r.W, func(i, j int) bool { return r.W[i].ID < r.W[j].ID })
	if len(r.W) == 0 {
		r.W = nil
	}
	return r
}

// The fast reader of history lines must read every line it is sure of exactly as the general JSON reader does.
func TestFastRecordAgreesWithJSON(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	pieces := []string{`{`, `}`, `"t"`, `"p"`, `"w"`, `:`, `,`, `"claude"`, `"codex"`, `"other"`, `[`, `]`, `1`, `-2`, `0.5`, `1e3`, `01`, `1.`,
		`"five_hour"`, `"x\"y"`, `" "`, `null`, `true`, `1790365540`, `-0`, `1e400`, `"é"`, `{}`, `[]`, ` `}
	lines := []string{
		`{"t":1790365540,"p":"claude","w":{"five_hour":[46,1790375999,1790365538],"seven_day":[81,1790737199,1790365538]}}`,
		`{"t":1,"p":"codex","w":{}}`, `{"t":1,"p":"codex","w":[]}`, `{"t":1,"p":"codex","w":{"a":[1,2]}}`, `{"t":1,"p":"codex","w":{"a":[1,2,3,4]}}`,
		`{"t":1,"p":"codex","w":{"a":[1,2,3],"a":[4,5,6]}}`, `{"t":1,"p":"codex","w":{"a":[1,2,3],}}`, `{"t":1,"p":"codex","w":{"a":[1,2,3]},"x":1}`,
		` { "t" : 1 , "p" : "claude" , "w" : { "a" : [ 1 , 2 , 3 ] } } `, `{"t":1,"p":"claude","w":{"a":[1,"2",3]}}`, `{"T":1,"p":"claude","w":{}}`,
		`{"t":1,"p":"claude","w":{"a":[1,2,3]}}x`, `{"t":12345678901234567,"p":"claude","w":{"a":[1.5,2e2,-3]}}`,
	}
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for k := 0; k < 3+rng.Intn(25); k++ {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		lines = append(lines, b.String())
		base := lines[rng.Intn(13)]
		pos := rng.Intn(len(base))
		lines = append(lines, base[:pos]+pieces[rng.Intn(len(pieces))]+base[pos:])
	}
	for _, line := range lines {
		fast, ok, sure := fastRecord(line)
		if !sure {
			continue
		}
		slow, sok := slowRecord(line)
		if ok != sok || (ok && !reflect.DeepEqual(normalized(fast), normalized(slow))) {
			t.Fatalf("%s: fast %v %+v, json %v %+v", line, ok, fast, sok, slow)
		}
	}
}

func TestHistoryReaderAppendsAndCuts(t *testing.T) {
	file := filepath.Join(t.TempDir(), "h.jsonl")
	line := func(t, u string) string {
		return `{"t":` + t + `,"p":"claude","w":{"five_hour":[` + u + `,2000000,` + t + `]}}`
	}
	_ = os.WriteFile(file, []byte(line("1000", "1")+"\n"+line("1000000", "2")+"\n"+`{"t":1000100,"p":"cla`), 0o600)
	h := &HistoryReader{File: file}
	if got := SamplesOf(h.Read(1000200), "claude", "five_hour"); len(got) != 1 || got[0].U != 2 {
		t.Fatalf("%+v", got)
	}
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`ude","w":{"five_hour":[3,2000000,1000000]}}` + "\n")
	f.Close()
	if got := SamplesOf(h.Read(1000200), "claude", "five_hour"); len(got) != 1 || got[0].U != 3 {
		t.Fatalf("the last line of a moment wins: %+v", got)
	}
}
