package heavy

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stats counts the heavy calls of <dir>/events since a moment.
type Stats struct {
	Dir   string `json:"dir"`
	Slots int    `json:"slots"`
	Since string `json:"since"`
	// Ran took a slot at once; Waited took one after waiting; Refused found none in time and did not run.
	Ran     int `json:"ran"`
	Waited  int `json:"waited"`
	Refused int `json:"refused"`
	// WaitP50Ms and WaitMaxMs are over the calls that waited or were refused.
	WaitP50Ms int64          `json:"wait_p50_ms"`
	WaitMaxMs int64          `json:"wait_max_ms"`
	ByTool    map[string]int `json:"by_tool"`
}

// ReadStats counts the events at or after since.
func ReadStats(dir string, since time.Time) (Stats, error) {
	st := Stats{Dir: dir, Since: since.UTC().Format(time.RFC3339), ByTool: map[string]int{}}
	f, err := openRegular(filepath.Join(dir, "events"), os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	defer f.Close()
	var waits []int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) != 4 {
			continue
		}
		ts, err1 := strconv.ParseInt(parts[0], 10, 64)
		ms, err2 := strconv.ParseInt(parts[3], 10, 64)
		if err1 != nil || err2 != nil || time.Unix(ts, 0).Before(since) {
			continue
		}
		switch parts[2] {
		case "ran":
			st.Ran++
		case "waited":
			st.Waited++
			waits = append(waits, ms)
		case "refused":
			st.Refused++
			waits = append(waits, ms)
		default:
			continue
		}
		st.ByTool[parts[1]]++
	}
	if len(waits) > 0 {
		sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
		st.WaitP50Ms = waits[len(waits)/2]
		st.WaitMaxMs = waits[len(waits)-1]
	}
	return st, sc.Err()
}

func statsCmd(args []string, env Env) int {
	since := 24 * time.Hour
	asJSON := false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			asJSON = true
		case a == "--since" && i+1 < len(args):
			d, err := time.ParseDuration(args[i+1])
			if err != nil || d <= 0 {
				fmt.Fprintf(env.Stderr, "harnessly: heavy stats: --since takes a duration such as 24h, not %q\n", args[i+1])
				return 2
			}
			since = d
			i++
		default:
			fmt.Fprintf(env.Stderr, "harnessly: heavy stats: unknown argument %q\n", a)
			return 2
		}
	}
	dir := Dir(env.Getenv)
	st, err := ReadStats(dir, env.Now().Add(-since))
	if err != nil {
		fmt.Fprintf(env.Stderr, "harnessly: heavy stats: %v\n", err)
		return 1
	}
	st.Slots = Slots(env.CPUs)
	if v, err := strconv.Atoi(strings.TrimSpace(env.Getenv(envSlots))); err == nil && v > 0 {
		st.Slots = v
	}
	if asJSON {
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Fprintln(env.Stdout, string(b))
		return 0
	}
	fmt.Fprintf(env.Stdout, "heavy checks on this machine in the last %s (%d %s, %s)\n", since, st.Slots, plural(st.Slots, "slot", "slots"), dir)
	fmt.Fprintf(env.Stdout, "  ran at once  %d\n  waited       %d\n  refused      %d\n", st.Ran, st.Waited, st.Refused)
	if st.Waited+st.Refused > 0 {
		fmt.Fprintf(env.Stdout, "  wait         median %s, longest %s\n",
			roundWait(time.Duration(st.WaitP50Ms)*time.Millisecond), roundWait(time.Duration(st.WaitMaxMs)*time.Millisecond))
	}
	return 0
}
