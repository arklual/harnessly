package machines

import (
	"encoding/json"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/limits"
)

// The machine snapshot: this runtime's machine with its load, its provider limits with a forecast per window, what it
// can do and its release. The machine agent sends it to the cluster every 15 s; the page asks for it by machine.

// Machine is one element of the answer; the keys are in the previous runtime's order.
type Machine struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Host       string        `json:"host"`
	User       string        `json:"user"`
	CPUPct     Num           `json:"cpuPct"`
	MemUsedPct Num           `json:"memUsedPct"`
	MemTotalGb Num           `json:"memTotalGb"`
	UptimeSec  Num           `json:"uptimeSec"`
	Limits     MachineLimits `json:"limits"`
	Features   struct {
		Codexbar bool `json:"codexbar"`
	} `json:"features"`
	Release *Release `json:"release"`
}

type MachineLimits struct {
	Claude *ProviderLimits `json:"claude"`
	Codex  *ProviderLimits `json:"codex"`
}

// Release is the release the runtime runs.
type Release struct {
	Version string  `json:"version"`
	Commit  *string `json:"commit"`
}

// LimitWindow is one window as the page and the cluster read it. Values from the limits file pass through as they are.
type LimitWindow struct {
	UsedPct   Num      `json:"usedPct"`
	ResetsAt  Num      `json:"resetsAt"`
	SampledAt any      `json:"sampledAt"`
	Source    any      `json:"source"`
	Forecast  Forecast `json:"forecast"`
	Pace      any      `json:"pace"`
	ID        *string  `json:"id,omitempty"`
	Title     *string  `json:"title,omitempty"`
}

type ProviderLimits struct {
	FiveHour  *LimitWindow  `json:"fiveHour"`
	SevenDay  *LimitWindow  `json:"sevenDay"`
	UpdatedAt *Num          `json:"updatedAt"`
	Plan      *string       `json:"plan"`
	Error     *string       `json:"error"`
	Extra     []LimitWindow `json:"extra"`
}

const (
	periodFiveHour = 5 * 3600
	periodSevenDay = 7 * 86400
)

// get is a property of a parsed JSON value as JavaScript reads it: undefined (false) on anything but an object.
func get(v any, k string) (any, bool) {
	if o, ok := v.(*jsvalue.Object); ok {
		return o.Get(k)
	}
	return nil, false
}

func getOr(v any, k string) any { x, _ := get(v, k); return x }

// nullish is undefined or null: what `??` replaces.
func nullish(v any, present bool) bool { return !present || v == nil }

// effectiveWindow: a window whose reset time has passed restarts from zero (statusline files only refresh while an
// agent is active).
func effectiveWindow(raw any, period, now float64) (used, resetsAt float64, ok bool) {
	u, _ := get(raw, "used_percentage")
	r, _ := get(raw, "resets_at")
	uf, uok := u.(float64)
	rf, rok := r.(float64)
	if !uok || !rok {
		return 0, 0, false
	}
	if rf > now {
		return uf, rf, true
	}
	return 0, rf + math.Ceil((now-rf)/period)*period, true
}

// passed is a value of the limits file written back as JSON.stringify writes it.
func passed(v any) json.RawMessage { return json.RawMessage(jsvalue.Stringify(v, "")) }

type samplesFunc func(windowID string) []limits.Sample

func windowView(raw any, period float64, spec WindowSpec, samples []limits.Sample, providerTs any, hasTs bool, pace any,
	a *Activity, now float64) *LimitWindow {
	used, resetsAt, ok := effectiveWindow(raw, period, now)
	if !ok {
		return nil
	}
	w := &LimitWindow{UsedPct: Num(used), ResetsAt: Num(resetsAt), Pace: passed(pace), SampledAt: passed(nil), Source: passed(nil)}
	if at, has := get(raw, "at"); !nullish(at, has) {
		w.SampledAt = passed(at)
	} else if hasTs && providerTs != nil {
		w.SampledAt = passed(providerTs)
	}
	if src, has := get(raw, "source"); !nullish(src, has) {
		w.Source = passed(src)
	}
	w.Forecast = ForecastWindow(samples, now, spec, a)
	return w
}

// BuildProviderLimits is one provider's limits with a forecast per window; now in epoch seconds. The activity covers
// every model, so codexbar's extra per-model windows go by the pace alone.
func BuildProviderLimits(raw any, samples samplesFunc, now float64, a *Activity) *ProviderLimits {
	if !jsvalue.Truthy(raw) {
		return nil
	}
	ts, hasTs := get(raw, "ts")
	out := &ProviderLimits{Extra: []LimitWindow{}}
	if extra, ok := getOr(raw, "extra").([]any); ok {
		for _, item := range extra {
			id, idOK := getOr(item, "id").(string)
			title, titleOK := getOr(item, "title").(string)
			if !idOK || !titleOK {
				continue
			}
			if v := windowView(getOr(item, "window"), periodSevenDay, SevenDay, samples(id), ts, hasTs, nil, nil, now); v != nil {
				v.ID, v.Title = &id, &title
				out.Extra = append(out.Extra, *v)
			}
		}
	}
	paceOf := func(k string) any {
		if p, has := get(getOr(raw, "pace"), k); !nullish(p, has) {
			return p
		}
		return nil
	}
	out.FiveHour = windowView(getOr(raw, "five_hour"), periodFiveHour, FiveHour, samples("five_hour"), ts, hasTs, paceOf("five_hour"), a, now)
	out.SevenDay = windowView(getOr(raw, "seven_day"), periodSevenDay, SevenDay, samples("seven_day"), ts, hasTs, paceOf("seven_day"), a, now)
	if t, ok := ts.(float64); ok {
		out.UpdatedAt = num(jsRound(t * 1000))
	}
	if name, ok := getOr(getOr(raw, "plan"), "name").(string); ok {
		out.Plan = &name
	}
	if msg, ok := getOr(getOr(raw, "codexbarError"), "message").(string); ok {
		out.Error = &msg
	}
	return out
}

// HostStats are the machine's figures the snapshot shows.
type HostStats struct {
	CPUs              int
	Load1             float64
	TotalMem, FreeMem float64
	Uptime            float64
}

// SnapshotConfig is what the snapshot reads.
type SnapshotConfig struct {
	DataDir string
	// NamePrefix is cut from the start of the host name to name the machine (RUNTIME_MACHINE_NAME_PREFIX).
	NamePrefix string
	// Codexbar says whether subscription usage comes from codexbar (an absolute RUNTIME_CODEXBAR_BIN).
	Codexbar bool
	// ReleaseFile names the release the runtime runs (RUNTIME_RELEASE_FILE); none when empty or unreadable.
	ReleaseFile string
	// Activity is the spend history per provider (Usage.Activity); none when nil.
	Activity func() map[string]*Activity
	// User is the runtime user's name; the process's own when empty.
	User     string
	Host     func() HostStats
	Hostname func() (string, error)
	Now      func() time.Time
}

// Snapshot builds the machine snapshot.
type Snapshot struct {
	cfg     SnapshotConfig
	history *limits.HistoryReader

	once      sync.Once
	user      string
	release   *Release
	relLoaded bool
	mu        sync.Mutex

	samplesKey samplesKey
	samples    map[string][]limits.Sample
}

// samplesKey tells one state of the history from another: it is only appended to and cut at its start.
type samplesKey struct {
	n           int
	first, last float64
}

func NewSnapshot(cfg SnapshotConfig) *Snapshot {
	if cfg.Host == nil {
		cfg.Host = ReadHostStats
	}
	if cfg.Hostname == nil {
		cfg.Hostname = os.Hostname
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Snapshot{cfg: cfg, history: &limits.HistoryReader{File: filepath.Join(cfg.DataDir, "limits-history.jsonl")}}
}

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// readRelease is read once: a release directory never changes under a running process.
func (s *Snapshot) readRelease() *Release {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.relLoaded {
		return s.release
	}
	s.relLoaded = true
	s.release = ReadRelease(s.cfg.ReleaseFile)
	return s.release
}

// ReadRelease reads a release file ({version, commit}); nil without a version.
func ReadRelease(file string) *Release {
	if file == "" {
		return nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return nil
	}
	version, _ := getOr(v, "version").(string)
	if version == "" {
		return nil
	}
	r := &Release{Version: version}
	if c, ok := getOr(v, "commit").(string); ok {
		r.Commit = &c
	}
	return r
}

// Limits are both providers' limits with forecasts from limits-history.jsonl.
func (s *Snapshot) Limits(now float64) MachineLimits {
	var raw any = jsvalue.NewObject()
	if data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "rate-limits.json")); err == nil {
		// A file that holds null or an array has no windows: the machine is shown without limits instead of failing.
		if v, err := jsvalue.Parse(data); err == nil {
			if o, ok := v.(*jsvalue.Object); ok {
				raw = o
			}
		}
	}
	records := s.history.Read(now)
	key := samplesKey{n: len(records)}
	if len(records) > 0 {
		key.first, key.last = records[0].T, records[len(records)-1].T
	}
	s.mu.Lock()
	if s.samplesKey != key || s.samples == nil {
		s.samplesKey, s.samples = key, map[string][]limits.Sample{}
	}
	cache := s.samples
	s.mu.Unlock()
	var activity map[string]*Activity
	if s.cfg.Activity != nil {
		activity = s.cfg.Activity()
	}
	// The samples change only with the history: polls in between reuse them.
	samples := func(provider string) samplesFunc {
		return func(id string) []limits.Sample {
			k := provider + "\x00" + id
			s.mu.Lock()
			got, ok := cache[k]
			s.mu.Unlock()
			if !ok {
				got = limits.SamplesOf(records, provider, id)
				s.mu.Lock()
				cache[k] = got
				s.mu.Unlock()
			}
			return got
		}
	}
	return MachineLimits{
		Claude: BuildProviderLimits(getOr(raw, "claude"), samples("claude"), now, activity["claude"]),
		Codex:  BuildProviderLimits(getOr(raw, "codex"), samples("codex"), now, activity["codex"]),
	}
}

// Machine is the snapshot now.
func (s *Snapshot) Machine() Machine {
	s.once.Do(func() {
		s.user = s.cfg.User
		if s.user == "" {
			s.user = currentUser()
		}
	})
	h := s.cfg.Host()
	cpus := h.CPUs
	if cpus == 0 {
		cpus = 1
	}
	host, _ := s.cfg.Hostname()
	name := strings.SplitN(host, ".", 2)[0]
	if s.cfg.NamePrefix != "" {
		name = strings.TrimPrefix(name, s.cfg.NamePrefix)
	}
	m := Machine{
		ID: host, Name: name, Host: host, User: s.user,
		CPUPct:     Num(math.Min(100, jsRound((h.Load1/float64(cpus))*100))),
		MemUsedPct: Num(jsRound(((h.TotalMem - h.FreeMem) / h.TotalMem) * 100)),
		MemTotalGb: Num(jsRound(h.TotalMem / (1024 * 1024 * 1024))),
		UptimeSec:  Num(jsRound(h.Uptime)),
		Limits:     s.Limits(float64(s.cfg.Now().UnixMilli()) / 1000),
		Release:    s.readRelease(),
	}
	m.Features.Codexbar = s.cfg.Codexbar
	return m
}
