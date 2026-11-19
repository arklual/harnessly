package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/builders"
	"github.com/ly-solutions/harnessly/internal/heavy"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A builder machine (docs/hub/builders.md). The hub's admin makes a machine a builder; this agent runs the heavy
// jobs the hub hands it only when the operator of the machine said yes too (AGENT_HUB_JOBS=on): a job is a command from the
// hub's people, so it is limited to a list of tools by name, runs with no shell in a folder of its own and a clean
// environment, and stops at its timeout. Its input is unpacked from the hub, its result files go back to the hub, and the
// folder is removed.
const (
	jobsOptIn = "AGENT_HUB_JOBS"
	// jobsToolsVar replaces the default list of tools a job may start (comma separated names).
	jobsToolsVar = "AGENT_JOBS_TOOLS"
	// jobsCapsVar adds labels to the detected ones (comma separated).
	jobsCapsVar = "AGENT_JOBS_CAPS"
	// jobsSlotsVar sets how many jobs run at once; the default follows the cores.
	jobsSlotsVar = "AGENT_JOBS_SLOTS"

	jobLogKeep   = 8 << 10
	jobsRelayMax = 1 << 30
)

// defaultJobTools are the programs a job may start: tool name → the label it gives the machine.
var defaultJobTools = map[string]string{
	"blender": "blender", "ffmpeg": "ffmpeg", "docker": "docker", "go": "go", "node": "node", "npm": "node", "pnpm": "node",
	"npx": "node", "python3": "python", "make": "make", "cargo": "rust", "git": "git",
}

var jobIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type jobRun struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	state  hubproto.JobState
	log    []byte
	// doneAt is when it ended; the agent forgets it an hour later.
	doneAt time.Time
}

type jobsState struct {
	mu   sync.Mutex
	runs map[string]*jobRun
}

// jobsServed says this machine takes the hub's heavy jobs: the operator opted in and the kill switch is not on.
func (h *hub) jobsServed() bool {
	return !h.policy.Disabled() && os.Getenv(jobsOptIn) == "on"
}

// jobTools are the allowed tools found on this machine, by name.
func jobTools() map[string]string {
	allowed := defaultJobTools
	if v := strings.TrimSpace(os.Getenv(jobsToolsVar)); v != "" {
		allowed = map[string]string{}
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				allowed[t] = defaultJobTools[t]
			}
		}
	}
	found := map[string]string{}
	for t, label := range allowed {
		if strings.ContainsAny(t, `/\`) {
			continue
		}
		if p, err := exec.LookPath(t); err == nil && !strings.Contains(p, heavy.Marker) {
			found[t] = label
		}
	}
	return found
}

func jobSlots() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(jobsSlotsVar))); err == nil && n > 0 {
		return n
	}
	return heavy.Slots(runtime.NumCPU())
}

func jobCaps(tools map[string]string) []string {
	seen := map[string]bool{}
	var caps []string
	add := func(c string) {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" && !seen[c] {
			seen[c] = true
			caps = append(caps, c)
		}
	}
	for _, l := range tools {
		add(l)
	}
	if _, err := os.Stat("/dev/nvidia0"); err == nil {
		add("gpu")
	} else if _, err := exec.LookPath("nvidia-smi"); err == nil {
		add("gpu")
	}
	for _, c := range strings.Split(os.Getenv(jobsCapsVar), ",") {
		add(c)
	}
	sort.Strings(caps)
	return caps
}

func (h *hub) opJob(op string, req map[string]json.RawMessage) (any, func(), error) {
	if err := h.allow(op, h.jobsServed(), "heavy jobs of the hub"); err != nil {
		return nil, nil, err
	}
	switch op {
	case hubproto.OpJobCaps:
		tools := jobTools()
		names := make([]string, 0, len(tools))
		for t := range tools {
			names = append(names, t)
		}
		sort.Strings(names)
		return hubproto.JobCaps{Caps: jobCaps(tools), Slots: jobSlots(), Tools: names}, nil, nil
	case hubproto.OpJobStart:
		var st hubproto.JobStart
		raw, _ := json.Marshal(req)
		if json.Unmarshal(raw, &st) != nil || !jobIDRe.MatchString(st.JobID) || len(st.Argv) == 0 {
			return nil, nil, invalid("jobId and argv are required")
		}
		if err := h.startJob(st); err != nil {
			return nil, nil, err
		}
		h.audit.Record(op, true, map[string]any{"jobId": st.JobID, "tool": st.Argv[0]})
		return empty(), nil, nil
	case hubproto.OpJobPoll:
		ids, _ := field[[]string](req, "jobIds")
		if len(ids) > hubproto.JobPollMax {
			return nil, nil, invalid("at most %d jobs in one poll", hubproto.JobPollMax)
		}
		out := map[string]hubproto.JobState{}
		h.jobs.mu.Lock()
		for _, id := range ids {
			if r := h.jobs.runs[id]; r != nil {
				out[id] = r.snapshot()
			}
		}
		h.jobs.mu.Unlock()
		return map[string]any{"jobs": out}, nil, nil
	default: // hubproto.OpJobCancel
		id, _ := field[string](req, "jobId")
		h.jobs.mu.Lock()
		r := h.jobs.runs[id]
		h.jobs.mu.Unlock()
		if r != nil {
			r.cancel()
		}
		h.audit.Record(op, true, map[string]any{"jobId": id})
		return empty(), nil, nil
	}
}

func (r *jobRun) snapshot() hubproto.JobState {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state
	s.Log = string(bytes.ToValidUTF8(r.log, nil))
	return s
}

func (r *jobRun) set(f func(s *hubproto.JobState)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(&r.state)
}

// Write keeps the tail of the command's output.
func (r *jobRun) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, p...)
	if len(r.log) > 2*jobLogKeep {
		r.log = r.log[len(r.log)-jobLogKeep:]
	}
	return len(p), nil
}

func (h *hub) startJob(st hubproto.JobStart) error {
	h.jobs.mu.Lock()
	defer h.jobs.mu.Unlock()
	if h.jobs.runs == nil {
		h.jobs.runs = map[string]*jobRun{}
	}
	if _, again := h.jobs.runs[st.JobID]; again {
		return nil
	}
	for id, r := range h.jobs.runs {
		r.mu.Lock()
		old := !r.doneAt.IsZero() && time.Since(r.doneAt) > time.Hour
		r.mu.Unlock()
		if old {
			delete(h.jobs.runs, id)
		}
	}
	tools := jobTools()
	if _, ok := tools[st.Argv[0]]; !ok || strings.ContainsAny(st.Argv[0], `/\`) {
		return hubproto.Errf(hubproto.CodePolicyDenied, "%s is not a tool this machine runs for jobs (it runs: %s)", st.Argv[0], strings.Join(sortedKeys(tools), ", "))
	}
	running := 0
	for _, r := range h.jobs.runs {
		if r.snapshot().Status == hubproto.HeavyRunning {
			running++
		}
	}
	if running >= jobSlots() {
		return hubproto.Errf(hubproto.CodeBusy, "all %d job slots of this machine are taken", jobSlots())
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &jobRun{cancel: cancel}
	r.state.Status = hubproto.HeavyRunning
	h.jobs.runs[st.JobID] = r
	go h.runJob(ctx, r, st)
	return nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// runJob is one job from its input to its result files.
func (h *hub) runJob(ctx context.Context, r *jobRun, st hubproto.JobStart) {
	defer r.cancel()
	fail := func(status, msg string, exit *int) {
		r.set(func(s *hubproto.JobState) { s.Status, s.Error, s.ExitCode = status, msg, exit })
	}
	dir := filepath.Join(h.dir, "jobs", st.JobID)
	_ = os.RemoveAll(dir)
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		fail(hubproto.HeavyFailed, "no folder for the job: "+err.Error(), nil)
		return
	}
	defer os.RemoveAll(dir)
	if st.Input {
		if err := h.fetchJobInput(ctx, st.JobID, work); err != nil {
			fail(hubproto.HeavyFailed, "the input files did not come: "+err.Error(), nil)
			return
		}
	}
	timeout := time.Duration(st.TimeoutSec) * time.Second
	if timeout <= 0 || timeout > 24*time.Hour {
		timeout = time.Hour
	}
	runCtx, stop := context.WithTimeout(ctx, timeout)
	defer stop()
	path, err := exec.LookPath(st.Argv[0])
	if err != nil {
		fail(hubproto.HeavyFailed, st.Argv[0]+" is not installed on this machine", nil)
		return
	}
	cmd := exec.CommandContext(runCtx, path, st.Argv[1:]...)
	cmd.Dir = work
	cmd.Env = jobEnv(work, st.Env)
	cmd.Stdout, cmd.Stderr = r, r
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	runErr := cmd.Run()
	exit := 0
	var msg string
	status := hubproto.HeavyDone
	if runErr != nil {
		status, exit = hubproto.HeavyFailed, 1
		var ee *exec.ExitError
		switch {
		case ctx.Err() != nil:
			status, msg = hubproto.HeavyCancelled, "cancelled"
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			msg = fmt.Sprintf("the timeout of %s ended the command", timeout)
		case errors.As(runErr, &ee):
			exit = ee.ExitCode()
			msg = fmt.Sprintf("the command ended with exit code %d", exit)
		default:
			msg = runErr.Error()
		}
	}
	// The result files go back even when the command failed: a render that stopped half way still has its frames.
	files, uerr := h.sendJobOutputs(h.jobsCtx(), st, work)
	if uerr != nil && status == hubproto.HeavyDone {
		status, msg = hubproto.HeavyFailed, "the result files did not reach the hub: "+uerr.Error()
	}
	r.set(func(s *hubproto.JobState) { s.Status, s.Error, s.ExitCode, s.Outputs = status, msg, &exit, files })
	r.mu.Lock()
	r.doneAt = time.Now()
	r.mu.Unlock()
}

func (h *hub) jobsCtx() context.Context { return context.Background() }

// jobEnv is a clean environment: the program's own PATH and a home inside the job's folder.
func jobEnv(work string, extra map[string]string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + work, "TMPDIR=" + work, "LANG=C.UTF-8", "HARNESSLY_JOB=1"}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// inputSettle is how long a builder asks again for an input the hub does not give it yet: a hub of an older release
// writes the job's builder down only after the start reached it, so the first ask can come before and get 404.
var inputSettle = 5 * time.Second

func (h *hub) fetchJobInput(ctx context.Context, id, work string) error {
	for deadline := time.Now().Add(inputSettle); ; {
		res, err := h.jobsDo(ctx, http.MethodGet, "/"+id+"/input", nil, nil, "")
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusNotFound && time.Now().Before(deadline) {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
			res.Body.Close()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("the hub answered HTTP %d", res.StatusCode)
		}
		return builders.Unpack(res.Body, work)
	}
}

func (h *hub) sendJobOutputs(ctx context.Context, st hubproto.JobStart, work string) ([]hubproto.JobFile, error) {
	rels, err := builders.Collect(work, st.Outputs)
	if err != nil {
		return nil, err
	}
	var out []hubproto.JobFile
	for _, rel := range rels {
		f, err := os.Open(filepath.Join(work, filepath.FromSlash(rel)))
		if err != nil {
			return out, err
		}
		info, _ := f.Stat()
		res, err := h.jobsDo(ctx, http.MethodPut, "/"+st.JobID+"/out", url.Values{"path": {rel}}, f, "application/octet-stream")
		f.Close()
		if err != nil {
			return out, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return out, fmt.Errorf("the hub answered HTTP %d for %s", res.StatusCode, rel)
		}
		if info != nil {
			out = append(out, hubproto.JobFile{Path: rel, Size: info.Size()})
		}
	}
	return out, nil
}

// jobsDo calls the hub's /agent/jobs<path>, signed with the machine's key.
func (h *hub) jobsDo(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string) (*http.Response, error) {
	m := h.machine
	host, err := hubproto.DialHost(m.DialURL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(m.DialURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	u.Path, u.RawQuery = hubproto.JobsPath+path, q.Encode()
	auth, err := hubproto.SignJobs(m.SigningKey(), m.MachineID, time.Now(), host)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("could not reach the hub: %w", err)
	}
	return res, nil
}

// jobsRoute is /jobs/... on the loopback: this machine's runtime and its CLI reach the hub's pool of builders through it.
// The agent.json token is the key; the request goes to the hub as it is, signed with the machine's key.
func (a *Agent) jobsRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, http.StatusForbidden, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	h := a.hub
	if h == nil || h.policy.Disabled() {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "this machine is not connected to a hub"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/jobs")
	if rest != "" && !strings.HasPrefix(rest, "/") {
		reply(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	body := http.MaxBytesReader(w, r.Body, jobsRelayMax)
	res, err := h.jobsDo(r.Context(), r.Method, rest, r.URL.Query(), body, r.Header.Get("Content-Type"))
	if err != nil {
		reply(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound && rest == "/builders" {
		reply(w, http.StatusNotFound, map[string]string{"error": "this hub has no pool of builders (HUB_BUILDERS)"})
		return
	}
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Disposition"} {
		if v := res.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}
