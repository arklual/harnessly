package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Linker reaches a machine over the agent link.
type Linker interface {
	// Online says the machine's link is up.
	Online(machineID string) bool
	Do(ctx context.Context, machineID, op string, params any, timeout time.Duration) (json.RawMessage, error)
}

// Deps are what the service is built from.
type Deps struct {
	DB   *store.DB
	Link Linker
	// Offers says whether the machine's agent said the feature in its hello.
	Offers func(machineID, feature string) bool
	// Dir is the folder of the jobs' files.
	Dir string
	Log *slog.Logger
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Tick is how often the scheduler looks at the queue; 0 is 2 s.
	Tick time.Duration
}

// Service is the pool and the queue.
type Service struct {
	db     *store.DB
	link   Linker
	offers func(string, string) bool
	files  Files
	log    *slog.Logger
	now    func() time.Time
	every  time.Duration
	wake   chan struct{}

	mu       sync.Mutex
	misses   map[string]int       // job id → polls that did not know the job
	firstBad map[string]time.Time // machine id → when its polls began to fail
	sweptAt  time.Time
	// lastAt is the creation time of the last job submitted here: the queue goes by it, so two jobs of one
	// millisecond keep the order they came in.
	lastAt time.Time
}

const (
	callTimeout = 20 * time.Second
	// lostAfter is how long a builder's polls may fail before its running jobs are failed.
	lostAfter = 3 * time.Minute
	// inputWait is how long a queued job waits for its input files.
	inputWait = 20 * time.Minute
	// localDraftSec caps a local fallback run: it is a draft, the builder pool is where the real run goes.
	localDraftSec = 600
	jobKeep       = 30 * 24 * time.Hour
	maxQueueWait  = 6 * time.Hour
)

// New makes the service.
func New(d Deps) *Service {
	s := &Service{db: d.DB, link: d.Link, offers: d.Offers, files: Files{Root: d.Dir}, log: d.Log, now: d.Now, every: d.Tick,
		wake: make(chan struct{}, 1), misses: map[string]int{}, firstBad: map[string]time.Time{}}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.every <= 0 {
		s.every = 2 * time.Second
	}
	return s
}

// Files is the folder of the jobs' files.
func (s *Service) Files() Files { return s.files }

// Wake asks the scheduler to look at the queue now.
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run is the scheduler: it places the queued jobs, follows the running ones and tidies up. It returns when ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(s.every)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
	}
}

// BuilderView is a builder as the list shows it.
type BuilderView struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Role  string   `json:"role"`
	Caps  []string `json:"caps"`
	Slots int      `json:"slots"`
	// Running is the jobs it runs now; Waiting the queued jobs that asked for this builder by name.
	Running int  `json:"running"`
	Waiting int  `json:"waiting"`
	Online  bool `json:"online"`
	// Ready: online and its agent runs jobs; a builder that is not ready takes none.
	Ready bool `json:"ready"`
	// Note says in words why it is not ready.
	Note string `json:"note,omitempty"`
}

// Pool is the space's machines as place() sees them, and the builders among them as the lists show them.
func (s *Service) Pool(ctx context.Context, spaceID string) ([]Candidate, []BuilderView, error) {
	ms, err := s.db.S().MachinesBySpace(ctx, spaceID)
	if err != nil {
		return nil, nil, err
	}
	roles, err := s.db.S().BuildersBySpace(ctx, spaceID)
	if err != nil {
		return nil, nil, err
	}
	active, err := s.db.S().ActiveHeavyJobs(ctx)
	if err != nil {
		return nil, nil, err
	}
	running, waiting := map[string]int{}, map[string]int{}
	for _, j := range active {
		switch {
		case j.Status == hubproto.HeavyRunning && !j.Local:
			running[j.MachineID]++
		case j.Status == hubproto.HeavyQueued && j.WantMachine != "":
			waiting[j.WantMachine]++
		}
	}
	var cands []Candidate
	var views []BuilderView
	for _, m := range ms {
		if m.DisabledAt != nil {
			continue
		}
		b, isBuilder := roles[m.ID]
		c := Candidate{ID: m.ID, Name: m.Name, Role: store.RoleAgents, Running: running[m.ID]}
		if isBuilder {
			c.Role, c.Caps, c.Slots = b.Role, b.Caps, b.Slots
		}
		c.Online = s.link.Online(m.ID)
		c.Offers = c.Online && s.offers != nil && s.offers(m.ID, hubproto.FeatureJobRun)
		cands = append(cands, c)
		if c.Role != store.RoleBuilder {
			continue
		}
		v := BuilderView{ID: m.ID, Name: m.Name, Role: c.Role, Caps: nonNil(c.Caps), Slots: c.Slots, Running: c.Running,
			Waiting: waiting[m.ID] + waiting[m.Name], Online: c.Online, Ready: c.Online && c.Offers}
		switch {
		case !c.Online:
			v.Note = "offline"
		case !c.Offers:
			v.Note = "its agent does not run jobs: set AGENT_HUB_JOBS=on on the machine"
		}
		views = append(views, v)
	}
	return cands, views, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Builders lists the space's builders.
func (s *Service) Builders(ctx context.Context, spaceID string) ([]BuilderView, error) {
	_, v, err := s.Pool(ctx, spaceID)
	if v == nil {
		v = []BuilderView{}
	}
	return v, err
}

var capName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// CleanCaps normalises labels: lower case, no duplicates, valid names.
func CleanCaps(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, c := range in {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" || seen[c] {
			continue
		}
		if !capName.MatchString(c) {
			return nil, httpx.Invalid("caps", "Метка — латиница, цифры, точка, дефис, до 32 знаков: "+c)
		}
		seen[c] = true
		out = append(out, c)
	}
	if len(out) > 24 {
		return nil, httpx.Invalid("caps", "Не больше 24 меток")
	}
	return out, nil
}

// SetRole makes a machine a builder (with labels and slots) or an agents' machine again. A builder given no labels or
// slots takes what its agent detects, when it is online.
func (s *Service) SetRole(ctx context.Context, m store.Machine, role string, caps []string, slots int, by string) (store.MachineBuilder, error) {
	switch role {
	case store.RoleAgents, store.RoleBuilder:
	default:
		return store.MachineBuilder{}, httpx.Invalid("role", "Роль — agents или builder")
	}
	caps, err := CleanCaps(caps)
	if err != nil {
		return store.MachineBuilder{}, err
	}
	if slots < 0 || slots > 64 {
		return store.MachineBuilder{}, httpx.Invalid("slots", "Слотов от 1 до 64")
	}
	prev, _, err := s.db.S().MachineBuilderOf(ctx, m.ID)
	if err != nil {
		return store.MachineBuilder{}, err
	}
	b := store.MachineBuilder{MachineID: m.ID, Role: role, Caps: caps, Slots: slots, UpdatedBy: by, UpdatedAt: s.now().UTC()}
	if role == store.RoleBuilder {
		if len(caps) == 0 && len(prev.Caps) > 0 {
			b.Caps = prev.Caps
		}
		// Slots the person changed in this request are theirs. Otherwise, while the agent has never told its labels,
		// the slots kept are the stand-in 1 of a role set before the agent could be asked (offline, or its hello with
		// the job feature not in yet), and the agent's count replaces them together with the labels.
		personSlots := slots > 0 && slots != prev.Slots
		if !personSlots {
			b.Slots = prev.Slots
		}
		detect := !personSlots && len(prev.Caps) == 0
		if (len(b.Caps) == 0 || b.Slots == 0) && s.link.Online(m.ID) && s.offers != nil && s.offers(m.ID, hubproto.FeatureJobRun) {
			var got hubproto.JobCaps
			if raw, err := s.link.Do(ctx, m.ID, hubproto.OpJobCaps, struct{}{}, callTimeout); err == nil && json.Unmarshal(raw, &got) == nil {
				if len(b.Caps) == 0 {
					b.Caps, _ = CleanCaps(got.Caps)
				}
				if got.Slots > 0 && (b.Slots == 0 || detect) {
					b.Slots = got.Slots
				}
			}
		}
		if b.Slots == 0 {
			b.Slots = 1
		}
	} else {
		b.Caps, b.Slots = prev.Caps, prev.Slots
	}
	if err := s.db.S().SetMachineBuilder(ctx, b); err != nil {
		return store.MachineBuilder{}, err
	}
	s.Wake()
	return b, nil
}

// Request is what a person or a machine hands the hub to run.
type Request struct {
	Title string `json:"title"`
	Need  struct {
		Kind    string   `json:"kind"`
		Caps    []string `json:"caps"`
		Machine string   `json:"machine"`
	} `json:"need"`
	Argv       []string          `json:"argv"`
	Env        map[string]string `json:"env"`
	Outputs    []string          `json:"outputs"`
	TimeoutSec int               `json:"timeoutSec"`
	// HasInput says an input archive follows (PUT input) when the job goes to a builder.
	HasInput bool `json:"hasInput"`
}

// Actor is who submits: a person of a space, from a machine.
type Actor struct {
	SpaceID   string
	UserID    string
	MachineID string
}

var envName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

func (r *Request) clean() error {
	if r.Need.Kind == "" {
		r.Need.Kind = hubproto.JobKindHeavy
	}
	if _, ok := Kinds[r.Need.Kind]; !ok || r.Need.Kind != hubproto.JobKindHeavy {
		return httpx.Invalid("need.kind", "Сейчас бывает только heavy")
	}
	caps, err := CleanCaps(r.Need.Caps)
	if err != nil {
		return err
	}
	r.Need.Caps = caps
	r.Need.Machine = strings.TrimSpace(r.Need.Machine)
	if strings.EqualFold(r.Need.Machine, "auto") {
		r.Need.Machine = ""
	}
	if len(r.Argv) == 0 || len(r.Argv) > 256 {
		return httpx.Invalid("argv", "Нужна команда: программа и её аргументы")
	}
	for _, a := range r.Argv {
		if len(a) > 8192 || strings.ContainsRune(a, 0) {
			return httpx.Invalid("argv", "Аргумент слишком длинный или с нулевым байтом")
		}
	}
	if p := r.Argv[0]; p == "" || strings.ContainsAny(p, `/\`) {
		return httpx.Invalid("argv", "Программа — имя без пути (blender, ffmpeg): хозяин машины решает, что на ней запускать")
	}
	if len(r.Env) > 32 {
		return httpx.Invalid("env", "Не больше 32 переменных")
	}
	for k, v := range r.Env {
		if !envName.MatchString(k) || strings.HasPrefix(k, "LD_") || k == "PATH" || k == "HOME" || len(v) > 4096 {
			return httpx.Invalid("env", "Переменная не подходит: "+k)
		}
	}
	if len(r.Outputs) > 32 {
		return httpx.Invalid("outputs", "Не больше 32 масок результата")
	}
	for _, o := range r.Outputs {
		if _, err := CleanOut(o); err != nil || filepath.IsAbs(o) {
			return httpx.Invalid("outputs", "Маска результата — относительный путь: "+o)
		}
	}
	if r.TimeoutSec <= 0 {
		r.TimeoutSec = 3600
	}
	if r.TimeoutSec > 24*3600 {
		return httpx.Invalid("timeoutSec", "Не больше 24 часов")
	}
	if len(r.Title) > 200 {
		r.Title = r.Title[:200]
	}
	return nil
}

// JobView is a job as the API shows it.
type JobView struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Need   struct {
		Kind string   `json:"kind"`
		Caps []string `json:"caps"`
	} `json:"need"`
	// Choice is what the person chose: auto, or a builder by name.
	Choice struct {
		Mode    string `json:"mode"`
		Machine string `json:"machine,omitempty"`
	} `json:"choice"`
	// Builder is where it was placed; nil while it waits.
	Builder *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"builder"`
	Local bool `json:"local"`
	// LocalLimits are the draft limits of a local run.
	LocalLimits *struct {
		TimeoutSec int    `json:"timeoutSec"`
		Note       string `json:"note"`
	} `json:"localLimits,omitempty"`
	Reason      string                `json:"reason"`
	ExitCode    *int                  `json:"exitCode"`
	Error       string                `json:"error,omitempty"`
	Log         string                `json:"log,omitempty"`
	Outputs     []store.JobResultFile `json:"outputs"`
	Argv        []string              `json:"argv"`
	OutputGlobs []string              `json:"outputGlobs"`
	TimeoutSec  int                   `json:"timeoutSec"`
	CreatedAt   string                `json:"createdAt"`
	StartedAt   *string               `json:"startedAt"`
	FinishedAt  *string               `json:"finishedAt"`
}

// View renders a job; names resolves a builder id to its name.
func (s *Service) View(ctx context.Context, j store.HeavyJob) JobView {
	var v JobView
	v.ID, v.Title, v.Status, v.Need.Kind, v.Need.Caps = j.ID, j.Title, j.Status, j.Kind, nonNil(j.Caps)
	v.Choice.Mode = "auto"
	if j.WantMachine != "" {
		v.Choice.Mode, v.Choice.Machine = "builder", s.machineName(ctx, j.WantMachine)
	}
	if j.MachineID != "" {
		v.Builder = &struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{j.MachineID, s.machineName(ctx, j.MachineID)}
	}
	v.Local = j.Local
	if j.Local {
		v.LocalLimits = &struct {
			TimeoutSec int    `json:"timeoutSec"`
			Note       string `json:"note"`
		}{min(j.TimeoutSec, localDraftSec), "a draft run on this machine's own heavy slots: at most 10 minutes, one at a time with the machine's other builds"}
	}
	v.Reason, v.ExitCode, v.Error, v.Log = j.Reason, j.ExitCode, j.Error, j.LogTail
	v.Outputs = j.Result
	if v.Outputs == nil {
		v.Outputs = []store.JobResultFile{}
	}
	v.Argv, v.OutputGlobs, v.TimeoutSec = j.Argv, nonNil(j.Outputs), j.TimeoutSec
	v.CreatedAt, v.StartedAt, v.FinishedAt = httpx.Time(j.CreatedAt), httpx.TimePtr(j.StartedAt), httpx.TimePtr(j.FinishedAt)
	return v
}

func (s *Service) machineName(ctx context.Context, id string) string {
	if m, err := s.db.S().MachineByID(ctx, id); err == nil {
		return m.Name
	}
	return id
}

// Submit records a job and answers where it goes: a builder (queued until the scheduler hands it over), the queue, or
// this machine's own slots.
func (s *Service) Submit(ctx context.Context, a Actor, req Request) (store.HeavyJob, error) {
	if err := req.clean(); err != nil {
		return store.HeavyJob{}, err
	}
	cands, _, err := s.Pool(ctx, a.SpaceID)
	if err != nil {
		return store.HeavyJob{}, err
	}
	want := req.Need.Machine
	if want != "" {
		// the explicit choice is kept by id
		for _, c := range cands {
			if c.ID == want || strings.EqualFold(c.Name, want) {
				want = c.ID
			}
		}
	}
	j := store.HeavyJob{SpaceID: a.SpaceID, UserID: a.UserID, FromMachine: a.MachineID, Title: req.Title, Kind: req.Need.Kind, Caps: req.Need.Caps,
		WantMachine: want, Argv: req.Argv, Env: req.Env, Outputs: req.Outputs, TimeoutSec: req.TimeoutSec, HasInput: req.HasInput,
		Status: hubproto.HeavyQueued, CreatedAt: s.createdAt()}
	pl := Place(Need{Kind: j.Kind, Caps: j.Caps, Machine: j.WantMachine}, cands)
	switch {
	case pl.Local && a.MachineID != "":
		s.startLocal(&j, pl.Reason)
	case pl.Local:
		// nobody to run it for the caller: from the web there is no local slot
		j.Status, j.Error, j.Reason = hubproto.HeavyFailed, "no online builder fits and the request has no machine of its own to run on: "+pl.Reason, pl.Reason
		fin := s.now().UTC()
		j.FinishedAt = &fin
	default:
		j.Reason = pl.Reason
	}
	if err := s.db.S().CreateHeavyJob(ctx, &j); err != nil {
		return store.HeavyJob{}, err
	}
	s.Wake()
	return j, nil
}

func (s *Service) startLocal(j *store.HeavyJob, reason string) {
	now := s.now().UTC()
	j.Local, j.Status, j.MachineID, j.Reason, j.StartedAt = true, hubproto.HeavyRunning, j.FromMachine, reason, &now
}

// Job reads a job of a space.
func (s *Service) Job(ctx context.Context, spaceID, id string) (store.HeavyJob, error) {
	j, err := s.db.S().HeavyJobByID(ctx, id)
	if err != nil {
		if store.IsNotFound(err) {
			return store.HeavyJob{}, httpx.ErrNotFound
		}
		return store.HeavyJob{}, err
	}
	if j.SpaceID != spaceID {
		return store.HeavyJob{}, httpx.ErrNotFound
	}
	return j, nil
}

// Jobs lists a space's jobs, newest first.
func (s *Service) Jobs(ctx context.Context, spaceID string, limit int) ([]store.HeavyJob, error) {
	return s.db.S().HeavyJobsOfSpace(ctx, spaceID, limit)
}

// Cancel ends a job: a queued one at once, a running one by asking its builder (or, for a local run, by its status: the
// submitter's own process stops when it reads it).
func (s *Service) Cancel(ctx context.Context, j store.HeavyJob) (store.HeavyJob, error) {
	if !hubproto.HeavyActive(j.Status) {
		return j, nil
	}
	j.CancelWanted = true
	if j.Status == hubproto.HeavyQueued || j.Local {
		s.finish(&j, hubproto.HeavyCancelled, nil, "cancelled", "")
	}
	if err := s.db.S().UpdateHeavyJob(ctx, j); err != nil {
		return j, err
	}
	if !j.Local && j.MachineID != "" && j.Status == hubproto.HeavyRunning {
		_, _ = s.link.Do(ctx, j.MachineID, hubproto.OpJobCancel, map[string]string{"jobId": j.ID}, callTimeout)
	}
	s.Wake()
	return j, nil
}

// ReportLocal is the submitter telling how its local run ended.
func (s *Service) ReportLocal(ctx context.Context, j store.HeavyJob, exit int, errText, log string) (store.HeavyJob, error) {
	if !j.Local || !hubproto.HeavyActive(j.Status) {
		return j, httpx.Conflict("not_local", "Задача не идёт на локальных слотах")
	}
	st := hubproto.HeavyDone
	if exit != 0 {
		st = hubproto.HeavyFailed
	}
	s.finish(&j, st, &exit, errText, log)
	return j, s.db.S().UpdateHeavyJob(ctx, j)
}

func (s *Service) finish(j *store.HeavyJob, status string, exit *int, errText, log string) {
	now := s.now().UTC()
	j.Status, j.ExitCode, j.FinishedAt = status, exit, &now
	if status == hubproto.HeavyDone {
		errText = ""
	}
	j.Error = errText
	if log != "" {
		j.LogTail = tail(log)
	}
	if !j.Local {
		j.Result = s.files.ListOut(j.ID)
	}
}

func tail(s string) string {
	const max = 8 << 10
	if len(s) > max {
		return s[len(s)-max:]
	}
	return s
}

// Tick is one look at the queue and the running jobs.
func (s *Service) Tick(ctx context.Context) {
	active, err := s.db.S().ActiveHeavyJobs(ctx)
	if err != nil {
		s.log.Warn("jobs: read the queue", "error", err)
		return
	}
	byMachine := map[string][]store.HeavyJob{}
	for _, j := range active {
		if j.Status == hubproto.HeavyRunning && !j.Local {
			byMachine[j.MachineID] = append(byMachine[j.MachineID], j)
		}
	}
	for id, js := range byMachine {
		s.poll(ctx, id, js)
	}
	pools := map[string][]Candidate{}
	for _, j := range active {
		if j.Status != hubproto.HeavyQueued {
			if j.Local && j.StartedAt != nil && s.now().Sub(*j.StartedAt) > time.Duration(min(j.TimeoutSec, localDraftSec))*time.Second+10*time.Minute {
				s.finish(&j, hubproto.HeavyFailed, nil, "the machine that started this run did not report its end", "")
				_ = s.db.S().UpdateHeavyJob(ctx, j)
			}
			continue
		}
		s.place(ctx, j, pools)
	}
	if s.now().Sub(s.sweptAt) > 10*time.Minute {
		s.sweptAt = s.now()
		s.sweep(ctx)
	}
}

// place gives one queued job its place, if it has one now.
func (s *Service) place(ctx context.Context, j store.HeavyJob, pools map[string][]Candidate) {
	age := s.now().Sub(j.CreatedAt)
	if j.HasInput && !s.files.HasInput(j.ID) {
		if age > inputWait {
			s.finish(&j, hubproto.HeavyFailed, nil, "the input files did not arrive", "")
			_ = s.db.S().UpdateHeavyJob(ctx, j)
		}
		return
	}
	cands, ok := pools[j.SpaceID]
	if !ok {
		var err error
		if cands, _, err = s.Pool(ctx, j.SpaceID); err != nil {
			return
		}
		pools[j.SpaceID] = cands
	}
	pl := Place(Need{Kind: j.Kind, Caps: j.Caps, Machine: j.WantMachine}, cands)
	switch {
	case pl.Local:
		if j.FromMachine == "" {
			s.finish(&j, hubproto.HeavyFailed, nil, "no online builder fits: "+pl.Reason, "")
		} else {
			s.startLocal(&j, "the builder went away while the job waited: "+pl.Reason)
		}
		_ = s.db.S().UpdateHeavyJob(ctx, j)
	case pl.Wait:
		if age > maxQueueWait {
			s.finish(&j, hubproto.HeavyFailed, nil, "waited in the queue too long: "+pl.Reason, "")
			_ = s.db.S().UpdateHeavyJob(ctx, j)
		} else if pl.Reason != j.Reason {
			j.Reason = pl.Reason
			_ = s.db.S().UpdateHeavyJob(ctx, j)
		}
	default:
		s.start(ctx, j, *pl.Machine, pl.Reason, pools)
	}
}

func (s *Service) start(ctx context.Context, j store.HeavyJob, m Candidate, reason string, pools map[string][]Candidate) {
	// The builder is written down before it hears of the job: it asks for the input as soon as the start lands, maybe
	// before this call returns, and the hub hands the input only to the job's own machine.
	j.MachineID = m.ID
	if err := s.db.S().UpdateHeavyJob(ctx, j); err != nil {
		s.log.Warn("jobs: record the builder", "job", j.ID, "error", err)
		return
	}
	_, err := s.link.Do(ctx, m.ID, hubproto.OpJobStart, hubproto.JobStart{JobID: j.ID, Argv: j.Argv, Env: j.Env, TimeoutSec: j.TimeoutSec,
		Input: j.HasInput, Outputs: j.Outputs}, callTimeout)
	if err != nil {
		j.MachineID = ""
		var he *httpx.Error
		if errors.As(err, &he) && (he.Code == "machine_policy" || he.Code == "invalid_request") {
			s.finish(&j, hubproto.HeavyFailed, nil, "the builder "+m.Name+" refused it: "+he.Message, "")
		} else if !errors.As(err, &he) || (he.Code != "busy" && he.Code != "machine_offline") {
			s.log.Warn("jobs: start", "job", j.ID, "machine", m.ID, "error", err)
		}
		// busy, offline or another error: back in the queue for the next look
		_ = s.db.S().UpdateHeavyJob(ctx, j)
		return
	}
	now := s.now().UTC()
	j.Status, j.MachineID, j.Reason, j.StartedAt = hubproto.HeavyRunning, m.ID, reason, &now
	if err := s.db.S().UpdateHeavyJob(ctx, j); err != nil {
		s.log.Warn("jobs: record start", "job", j.ID, "error", err)
	}
	// the slot is taken: the next job of this look sees one less
	cs := pools[j.SpaceID]
	for i := range cs {
		if cs[i].ID == m.ID {
			cs[i].Running++
		}
	}
	s.Wake()
}

// poll asks a builder about its running jobs and writes down what it says.
func (s *Service) poll(ctx context.Context, machineID string, js []store.HeavyJob) {
	ids := make([]string, 0, len(js))
	for _, j := range js {
		ids = append(ids, j.ID)
	}
	var ans struct {
		Jobs map[string]hubproto.JobState `json:"jobs"`
	}
	raw, err := s.link.Do(ctx, machineID, hubproto.OpJobPoll, map[string]any{"jobIds": ids}, callTimeout)
	if err == nil {
		err = json.Unmarshal(raw, &ans)
	}
	s.mu.Lock()
	if err != nil {
		first, bad := s.firstBad[machineID]
		if !bad {
			s.firstBad[machineID] = s.now()
			first = s.now()
		}
		lost := s.now().Sub(first) > lostAfter
		s.mu.Unlock()
		if lost {
			for _, j := range js {
				s.finish(&j, hubproto.HeavyFailed, nil, "the builder went offline while the job ran", "")
				_ = s.db.S().UpdateHeavyJob(ctx, j)
			}
		}
		return
	}
	delete(s.firstBad, machineID)
	s.mu.Unlock()
	for _, j := range js {
		st, known := ans.Jobs[j.ID]
		if !known {
			s.mu.Lock()
			s.misses[j.ID]++
			gone := s.misses[j.ID] >= 3
			s.mu.Unlock()
			if gone {
				s.finish(&j, hubproto.HeavyFailed, nil, "the builder does not know the job (its agent restarted)", "")
				_ = s.db.S().UpdateHeavyJob(ctx, j)
			}
			continue
		}
		s.mu.Lock()
		delete(s.misses, j.ID)
		s.mu.Unlock()
		switch st.Status {
		case hubproto.HeavyDone, hubproto.HeavyFailed, hubproto.HeavyCancelled:
			msg := st.Error
			if st.Status == hubproto.HeavyFailed && msg == "" && st.ExitCode != nil {
				msg = fmt.Sprintf("the command ended with exit code %d", *st.ExitCode)
			}
			status := st.Status
			if j.CancelWanted {
				status = hubproto.HeavyCancelled
			}
			s.finish(&j, status, st.ExitCode, msg, st.Log)
			_ = s.db.S().UpdateHeavyJob(ctx, j)
		default:
			if st.Log != "" && tail(st.Log) != j.LogTail {
				j.LogTail = tail(st.Log)
				_ = s.db.S().UpdateHeavyJob(ctx, j)
			}
		}
	}
}

func (s *Service) sweep(ctx context.Context) {
	ids, err := s.db.S().DeleteHeavyJobsBefore(ctx, s.now().Add(-jobKeep))
	if err == nil {
		for _, id := range ids {
			s.files.Remove(id)
		}
	}
	s.files.Sweep(func(id string) bool {
		j, err := s.db.S().HeavyJobByID(ctx, id)
		if err != nil {
			return !store.IsNotFound(err)
		}
		return hubproto.HeavyActive(j.Status) || j.FinishedAt == nil || s.now().Sub(*j.FinishedAt) < FilesKeep
	})
}

// createdAt is now, but always after the last job submitted here (the store keeps milliseconds).
func (s *Service) createdAt() time.Time {
	at := s.now().UTC().Truncate(time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !at.After(s.lastAt) {
		at = s.lastAt.Add(time.Millisecond)
	}
	s.lastAt = at
	return at
}
