// Package timescale compresses the schedule of a run in time: with HL_TIME_SCALE=60 an hour of the
// agents' schedule passes in a minute. It exists for the eval stands of evallab, where a cron «every 30 minutes»
// would otherwise hold a runner for hours; a runtime without the variable runs on the plain clock.
//
// Two clocks come out of it. The logical clock is the one the agents live by: it starts at the epoch
// (HL_TIME_EPOCH_MS, the stand's start, shared by the runtime and every agent of the stand) and runs Factor times
// faster than the wall clock from there. Durations of the schedule — an idle window, a sweep, a wait for a subchat —
// are logical and last Real(d) of the wall clock. What the work itself takes (a model's answer, a command) is not
// compressed: nothing can make it faster.
//
// Claude Code reads its wall clock for its crons and wakeups (CronCreate, /loop, ScheduleWakeup: Date.now polled every
// second), so its clock is moved with a preload (Preload, PreloadEnv) that turns Date only: its timers and timeouts stay
// on the real clock, a request or a command is never cut short by the scale. Its transcript is then written on the
// logical clock, which is what evallab reads the windows of its checkpoints in.
package timescale

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Env is the variable of the scale: how many times faster the schedule goes; 1 or nothing is the plain clock.
	Env = "HL_TIME_SCALE"
	// EpochEnv is the moment both clocks agree, in Unix milliseconds: the stand's start.
	EpochEnv = "HL_TIME_EPOCH_MS"
	// Max is the largest scale taken: a day in 24 seconds, past which a sweep of seconds would see nothing.
	Max = 3600
)

var (
	mu     sync.RWMutex
	loaded bool
	factor = 1.0
	epoch  time.Time
)

func load() {
	mu.RLock()
	ok := loaded
	mu.RUnlock()
	if ok {
		return
	}
	f, e := Parse(os.Getenv(Env)), time.Now()
	if ms, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(EpochEnv)), 10, 64); err == nil && ms > 0 {
		e = time.UnixMilli(ms)
	}
	mu.Lock()
	if !loaded {
		factor, epoch, loaded = f, e, true
	}
	mu.Unlock()
}

// Parse reads a scale: a number from 1 to Max; anything else is the plain clock (1).
func Parse(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || f <= 1 {
		return 1
	}
	return math.Min(f, Max)
}

// Set fixes the scale and the epoch of this process (tests, and a process that takes them from flags).
func Set(f float64, at time.Time) {
	c := New(f, at)
	mu.Lock()
	defer mu.Unlock()
	factor, epoch, loaded = c.Factor, c.Epoch, true
}

// New is a clock of scale f from at: f is taken as Parse takes it.
func New(f float64, at time.Time) Clock {
	return Clock{Factor: Parse(strconv.FormatFloat(f, 'f', -1, 64)), Epoch: at}
}

// Clock is a scale with its epoch: the process's (Process) or a stand's.
type Clock struct {
	Factor float64
	Epoch  time.Time
}

// Process is this process's clock, from HL_TIME_SCALE and HL_TIME_EPOCH_MS (the epoch defaults to the first read).
func Process() Clock {
	load()
	mu.RLock()
	defer mu.RUnlock()
	return Clock{Factor: factor, Epoch: epoch}
}

// On reports whether the clock compresses the schedule.
func (c Clock) On() bool { return c.Factor > 1 }

// Real is how long a logical duration lasts on the wall clock: d itself on the plain clock, never less than a
// millisecond for a positive d.
func (c Clock) Real(d time.Duration) time.Duration {
	if !c.On() || d <= 0 {
		return d
	}
	return max(time.Duration(float64(d)/c.Factor), time.Millisecond)
}

// Logical is how long a wall-clock duration is on the logical clock.
func (c Clock) Logical(d time.Duration) time.Duration {
	if !c.On() {
		return d
	}
	return time.Duration(float64(d) * c.Factor)
}

// At is the logical time of a wall-clock moment.
func (c Clock) At(t time.Time) time.Time {
	if !c.On() {
		return t
	}
	return c.Epoch.Add(time.Duration(float64(t.Sub(c.Epoch)) * c.Factor))
}

// Env are the variables that put a process on this clock: a runtime, or with PreloadEnv an agent.
func (c Clock) Env() map[string]string {
	if !c.On() {
		return nil
	}
	return map[string]string{Env: strconv.FormatFloat(c.Factor, 'f', -1, 64), EpochEnv: fmt.Sprint(c.Epoch.UnixMilli())}
}

// PreloadEnv are the variables that give a Claude Code this logical clock, the preload being written at path. old
// is the BUN_OPTIONS it had, kept after the preload.
func (c Clock) PreloadEnv(path, old string) map[string]string {
	env := c.Env()
	if env == nil {
		return nil
	}
	opts := "--preload " + path
	if old = strings.TrimSpace(old); old != "" {
		opts += " " + old
	}
	env["BUN_OPTIONS"] = opts
	return env
}

// Factor is the scale of this process: 1 on the plain clock.
func Factor() float64 { return Process().Factor }

// On reports whether this process's schedule is compressed.
func On() bool { return Process().On() }

// Real is how long a logical duration of this process's schedule lasts on the wall clock.
func Real(d time.Duration) time.Duration { return Process().Real(d) }

// Logical is how long a wall-clock duration is on this process's logical clock.
func Logical(d time.Duration) time.Duration { return Process().Logical(d) }

// At is the logical time of a wall-clock moment on this process's clock.
func At(t time.Time) time.Time { return Process().At(t) }

// Now is the logical now: what an agent of the stand reads off its clock. Compare the times an agent wrote (a wakeup,
// a cron job's creation) with this, not with time.Now.
func Now() time.Time { return At(time.Now()) }

// Preload is the script that moves a Claude Code's wall clock (Bun's --preload): Date.now, new Date() and Date()
// run on the logical clock of HL_TIME_SCALE and HL_TIME_EPOCH_MS; a date built from a value, the timers and
// performance.now stay as they are.
const Preload = `// harnessly timescale (HRN-1645): this process's wall clock runs HL_TIME_SCALE times faster from HL_TIME_EPOCH_MS.
(() => {
  const k = Number(process.env.HL_TIME_SCALE), e = Number(process.env.HL_TIME_EPOCH_MS);
  if (!(k > 1) || !(e > 0) || globalThis.__hlTimeScale) return;
  const R = globalThis.Date, real = R.now.bind(R);
  const now = () => Math.round(e + (real() - e) * k);
  class D extends R {
    constructor(...a) { if (a.length === 0) super(now()); else super(...a); }
    static now() { return now(); }
  }
  globalThis.Date = new Proxy(D, { apply: () => new D().toString() });
  globalThis.__hlTimeScale = k;
})();
`

// Label is how a scale is shown to a person: «×60», "" on the plain clock.
func Label(f float64) string {
	if f <= 1 {
		return ""
	}
	return "×" + strconv.FormatFloat(f, 'f', -1, 64)
}
