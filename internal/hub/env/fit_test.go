package env

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// fitStepsOneByOne is how fitSteps used to work: empty one log, marshal the whole run again. Its answer is the one
// fitSteps must give.
func fitStepsOneByOne(steps []hubproto.EnvStep) ([]byte, bool) {
	for i := 0; ; i++ {
		raw, err := json.Marshal(steps)
		if err != nil {
			return nil, false
		}
		if len(raw) <= maxRunBytes {
			return raw, true
		}
		for i < len(steps) && steps[i].Log == "" {
			i++
		}
		if i >= len(steps) {
			return nil, false
		}
		steps[i].Log = ""
	}
}

// fitSteps empties exactly the logs the one-by-one way does, with logs of every size, some empty, some with
// characters JSON escapes, and runs too big even without logs.
func TestFitStepsEmptiesAsFewLogsAsOneByOne(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	// Around the bound: 30 to 70 steps with logs up to EnvLogMax, so that some logs go and the one-by-one way stays cheap.
	for n := 0; n < 8; n++ {
		steps := make([]hubproto.EnvStep, 30+r.IntN(40))
		for i := range steps {
			steps[i] = hubproto.EnvStep{ItemID: fmt.Sprintf("i%d", i), Kind: "script", Name: "S", Status: "ok"}
			if r.IntN(5) > 0 {
				steps[i].Log = strings.Repeat([]string{"x", "<", `"`, "\n", "é"}[r.IntN(5)], r.IntN(hubproto.EnvLogMax+1))
			}
			if n%4 == 0 {
				steps[i].Message = strings.Repeat("m", 16<<10) // too big without logs
			}
		}
		want, wantOK := fitStepsOneByOne(slices.Clone(steps))
		got, gotOK := fitSteps(slices.Clone(steps))
		if gotOK != wantOK || !bytes.Equal(got, want) {
			t.Fatalf("case %d (%d steps): fits %v, want %v; %d bytes, want %d", n, len(steps), gotOK, wantOK, len(got), len(want))
		}
	}
}
