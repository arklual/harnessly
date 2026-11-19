package timescale

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]float64{"": 1, "0": 1, "1": 1, "-5": 1, "abc": 1, "NaN": 1, "60": 60, " 2.5 ": 2.5, "100000": Max} {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestClock(t *testing.T) {
	epoch := time.UnixMilli(1_700_000_000_000)
	c := New(60, epoch)
	if got := c.Real(30 * time.Minute); got != 30*time.Second {
		t.Errorf("Real(30m) = %v", got)
	}
	if got := c.Real(time.Microsecond); got != time.Millisecond {
		t.Errorf("Real of a tiny duration = %v, want the 1ms floor", got)
	}
	if got := c.Logical(5 * time.Minute); got != 5*time.Hour {
		t.Errorf("Logical(5m) = %v", got)
	}
	if got := c.At(epoch.Add(time.Minute)); !got.Equal(epoch.Add(time.Hour)) {
		t.Errorf("At(epoch+1m) = %v", got)
	}
	plain := New(1, epoch)
	if plain.On() || plain.Real(time.Hour) != time.Hour || !plain.At(epoch.Add(time.Minute)).Equal(epoch.Add(time.Minute)) || plain.Env() != nil {
		t.Errorf("the plain clock moved: %+v", plain)
	}
	env := c.PreloadEnv("/s/timescale.js", " --smol ")
	if env[Env] != "60" || env[EpochEnv] != "1700000000000" || env["BUN_OPTIONS"] != "--preload /s/timescale.js --smol" {
		t.Errorf("PreloadEnv = %v", env)
	}
	if plain.PreloadEnv("/s/timescale.js", "") != nil {
		t.Error("the plain clock asked for a preload")
	}
}

func TestProcessClockFromSet(t *testing.T) {
	defer Set(1, time.Now())
	epoch := time.Now().Add(-time.Second)
	Set(60, epoch)
	if !On() || Factor() != 60 || Real(time.Hour) != time.Minute {
		t.Fatalf("Set(60) not taken: factor %v", Factor())
	}
	if d := Now().Sub(epoch); d < 59*time.Second || d > 2*time.Minute {
		t.Errorf("a second after the epoch the logical clock is %v past it", d)
	}
}

// TestPreloadMovesDateOnly runs the preload in Node (Claude Code runs it in Bun, the same JavaScript): Date moves,
// a date from a value and the timers do not.
func TestPreloadMovesDateOnly(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	dir := t.TempDir()
	pre := filepath.Join(dir, "timescale.js")
	if err := os.WriteFile(pre, []byte(Preload), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `const t0 = Date.now(), p0 = performance.now();
setTimeout(() => {
  const dl = Date.now() - t0, dp = performance.now() - p0;
  const fixed = new Date(0).getTime(), parsed = Date.parse("2020-01-01T00:00:00Z");
  const sameNew = Math.abs(new Date().getTime() - Date.now()) < 1000, str = typeof Date() === "string";
  console.log([dl, dp, fixed, parsed, sameNew, str, new Date() instanceof Date].join(" "));
}, 200);`
	epoch := time.Now().UnixMilli()
	cmd := exec.Command(node, "-r", pre, "-e", script)
	cmd.Env = append(os.Environ(), Env+"=60", EpochEnv+"="+strconv.FormatInt(epoch, 10))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v: %s", err, out)
	}
	f := strings.Fields(string(out))
	if len(f) != 7 {
		t.Fatalf("output %q", out)
	}
	dl, _ := strconv.ParseFloat(f[0], 64)
	dp, _ := strconv.ParseFloat(f[1], 64)
	if dp < 150 || dp > 2000 {
		t.Fatalf("the timer itself moved: %vms of performance.now", dp)
	}
	if ratio := dl / dp; ratio < 40 || ratio > 80 {
		t.Errorf("Date went %vms in %vms of the timer: ×%.1f, want ×60", dl, dp, ratio)
	}
	if f[2] != "0" || f[3] != "1577836800000" || f[4] != "true" || f[5] != "true" || f[6] != "true" {
		t.Errorf("Date from values or its shapes changed: %q", out)
	}
	// Off without the variables.
	cmd = exec.Command(node, "-r", pre, "-e", `console.log(globalThis.__hlTimeScale === undefined)`)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Errorf("the preload moved a clock with no scale: %s %v", out, err)
	}
}
