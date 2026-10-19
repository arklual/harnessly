package machines

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"harnessly/internal/jscompat"
)

// Pure parts of the machine load: /proc parsers, the CPU delta, level hysteresis, process kinds, command redaction.

type threshold struct{ warn, over, warnExit, overExit float64 }

// metrics in the order they are judged; swap is the swap-in rate in pages per second, never swap used. A level is
// entered at warn/over and left below warnExit/overExit.
var metrics = []struct {
	name string
	t    threshold
}{
	{"ram", threshold{85, 93, 82, 90}},
	{"swap", threshold{50, 500, 30, 350}},
	{"load", threshold{1.5, 3, 1.35, 2.85}},
	{"disk", threshold{90, 97, 87, 94}},
}

var levelRank = map[string]int{"ok": 0, "warn": 1, "over": 2}

// stuckScans: a dev server that listens but stays silent this many scans in a row counts as stuck.
const stuckScans = 3

const commandMax = 160

type cpuTicks struct{ idle, total float64 }

// jsNumber is Number(s) for the decimal fields of /proc files: blank is 0, junk is NaN.
func jsNumber(s string) float64 {
	s = jscompat.Trim(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || strings.ContainsAny(s, "_xXpP") {
		return math.NaN()
	}
	return f
}

func finiteAll(xs ...float64) bool {
	for _, x := range xs {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

var spaces = regexp.MustCompile(jscompat.SpaceClass + `+`)

func parseCPUStat(text string) (cpuTicks, bool) {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		parts := spaces.Split(jscompat.Trim(line), -1)[1:]
		fields := make([]float64, len(parts))
		for i, p := range parts {
			fields[i] = jsNumber(p)
		}
		if len(fields) < 4 || !finiteAll(fields...) {
			return cpuTicks{}, false
		}
		idle := fields[3]
		if len(fields) > 4 {
			idle += fields[4]
		}
		total := 0.0
		for _, v := range fields[:min(8, len(fields))] {
			total += v
		}
		return cpuTicks{idle, total}, true
	}
	return cpuTicks{}, false
}

func cpuPctFromDelta(prev, cur cpuTicks) float64 {
	total := cur.total - prev.total
	idle := cur.idle - prev.idle
	if total <= 0 || idle < 0 {
		return 0
	}
	return math.Min(100, math.Max(0, ((total-idle)/total)*100))
}

type meminfo struct{ totalBytes, usedPct, swapUsedBytes float64 }

var kbLines = map[string]*regexp.Regexp{}

func kbOf(text, key string) (float64, bool) {
	re := kbLines[key]
	if re == nil {
		return 0, false
	}
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	return jsNumber(m[1]), true
}

func init() {
	for _, k := range []string{"MemTotal", "MemAvailable", "SwapTotal", "SwapFree"} {
		kbLines[k] = regexp.MustCompile(`(?m)^` + k + `:` + jscompat.SpaceClass + `+(\d+)` + jscompat.SpaceClass + `*kB`)
	}
}

func parseMeminfo(text string) (meminfo, bool) {
	total, okT := kbOf(text, "MemTotal")
	available, okA := kbOf(text, "MemAvailable")
	if !okT || total == 0 || !okA {
		return meminfo{}, false
	}
	swapTotal, ok := kbOf(text, "SwapTotal")
	if !ok {
		swapTotal = 0
	}
	swapFree, ok := kbOf(text, "SwapFree")
	if !ok {
		swapFree = swapTotal
	}
	return meminfo{
		totalBytes:    total * 1024,
		usedPct:       math.Min(100, math.Max(0, (1-available/total)*100)),
		swapUsedBytes: math.Max(0, swapTotal-swapFree) * 1024,
	}, true
}

func parseLoadavg(text string) (float64, bool) {
	t := jscompat.Trim(text)
	if t == "" {
		return 0, false
	}
	load := jsNumber(spaces.Split(t, -1)[0])
	return load, finiteAll(load)
}

type procStat struct {
	pid       int
	comm      string
	ppid      int
	ticks     float64 // utime + stime in clock ticks
	starttime float64
}

// parseProcStat: comm is the text between the first "(" and the LAST ")": it may hold spaces and parentheses.
func parseProcStat(text string) (procStat, bool) {
	open := strings.IndexByte(text, '(')
	closing := strings.LastIndexByte(text, ')')
	if open < 0 || closing < open {
		return procStat{}, false
	}
	pid := jsNumber(text[:open])
	var rest []string
	if closing+2 <= len(text) {
		rest = spaces.Split(jscompat.Trim(text[closing+2:]), -1)
	}
	if len(rest) < 20 {
		return procStat{}, false
	}
	ppid, utime, stime, start := jsNumber(rest[1]), jsNumber(rest[11]), jsNumber(rest[12]), jsNumber(rest[19])
	if !finiteAll(pid, ppid, utime, stime, start) {
		return procStat{}, false
	}
	return procStat{pid: int(pid), comm: text[open+1 : closing], ppid: int(ppid), ticks: utime + stime, starttime: start}, true
}

var (
	vmRSS     = regexp.MustCompile(`(?m)^VmRSS:` + jscompat.SpaceClass + `+(\d+)` + jscompat.SpaceClass + `*kB`)
	pswpin    = regexp.MustCompile(`(?m)^pswpin` + jscompat.SpaceClass + `+(\d+)`)
	uidLine   = regexp.MustCompile(`(?m)^Uid:` + jscompat.SpaceClass + `+(\d+)` + jscompat.SpaceClass + `+(\d+)`)
	cgroupRow = regexp.MustCompile(`^(\d+):([^:]*):(.*)$`)
	// The systemd system unit of a cgroup path.
	serviceUnit = regexp.MustCompile(`^(/system\.slice(?:/[^/]+)*?/[^/]+\.service)(?:/|$)`)
)

func parseVmRSSBytes(text string) (float64, bool) {
	m := vmRSS.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	return jsNumber(m[1]) * 1024, true
}

func parseVmstatSwapIn(text string) (float64, bool) {
	m := pswpin.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	return jsNumber(m[1]), true
}

// parseProcUids is the real and effective uid: a setuid program runs as the user but acts as its owner.
func parseProcUids(text string) (real, effective int, ok bool) {
	m := uidLine.FindStringSubmatch(text)
	if m == nil {
		return 0, 0, false
	}
	return int(jsNumber(m[1])), int(jsNumber(m[2])), true
}

func metricLevel(t threshold, value float64, prev string) string {
	if value >= t.over || (prev == "over" && value >= t.overExit) {
		return "over"
	}
	if value >= t.warn || (prev != "ok" && value >= t.warnExit) {
		return "warn"
	}
	return "ok"
}

type levelState struct {
	level   string
	metrics map[string]string
	reason  *string
}

func initialLevels() map[string]string {
	return map[string]string{"ram": "ok", "swap": "ok", "load": "ok", "disk": "ok"}
}

// classifyLevel: each metric keeps its own hysteresis, so one stuck metric never drags another across its threshold. A
// nil value keeps the metric's previous level.
func classifyLevel(values map[string]*float64, prev map[string]string) levelState {
	out := levelState{level: "ok", metrics: initialLevels()}
	for _, m := range metrics {
		v := values[m.name]
		if v == nil {
			out.metrics[m.name] = prev[m.name]
		} else {
			out.metrics[m.name] = metricLevel(m.t, *v, prev[m.name])
		}
		if levelRank[out.metrics[m.name]] > levelRank[out.level] {
			out.level = out.metrics[m.name]
			name := m.name
			out.reason = &name
		}
	}
	return out
}

var (
	serviceComms  = map[string]bool{"netbird": true, "sshd": true, "sshd-session": true, "tbot": true, "teleport": true}
	agentBinaries = map[string]bool{"claude": true, "codex": true}
	shellComms    = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true}
	launchers     = map[string]bool{"node": true, "nodejs": true, "npx": true, "npm": true, "pnpm": true, "pnpx": true, "bun": true, "bunx": true, "yarn": true}
	agentPackage  = regexp.MustCompile(`@openai/codex|@anthropic-ai/claude-code|(^|/)codex\.js$|(^|/)claude(-code)?/cli\.js$`)
	agentVersion  = regexp.MustCompile(`/(claude|codex)/versions/[^/]+$`)
)

func base(v string) string { return v[strings.LastIndexByte(v, '/')+1:] }

func isTmuxComm(comm string) bool { return comm == "tmux" || strings.HasPrefix(comm, "tmux:") }

// isServiceComm: the tmux server and its clients, ssh sessions and access agents, systemd and dbus units.
func isServiceComm(comm string) bool {
	return serviceComms[comm] || isTmuxComm(comm) || strings.HasPrefix(comm, "systemd") || strings.HasPrefix(comm, "dbus")
}

// isAgentProcess: claude or codex as the program itself, the script behind node, or an npm launcher; never as a path
// segment of a project.
func isAgentProcess(comm, args string) bool {
	if agentBinaries[comm] {
		return true
	}
	tokens := strings.Split(args, " ")
	for _, t := range tokens[:min(2, len(tokens))] {
		if agentBinaries[base(t)] || agentVersion.MatchString(t) {
			return true
		}
	}
	if !launchers[base(tokens[0])] {
		return false
	}
	for _, t := range tokens[1:] {
		if agentPackage.MatchString(t) {
			return true
		}
	}
	return false
}

// systemServiceUnit is the systemd system unit of a process from /proc/<pid>/cgroup (the v2 "0::" line or the v1
// name=systemd hierarchy); "" outside system.slice.
func systemServiceUnit(cgroup string) string {
	for _, line := range strings.Split(cgroup, "\n") {
		m := cgroupRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		named := false
		for _, c := range strings.Split(m[2], ",") {
			named = named || c == "name=systemd"
		}
		if !(m[1] == "0" && m[2] == "") && !named {
			continue
		}
		if u := serviceUnit.FindStringSubmatch(m[3]); u != nil {
			return u[1]
		}
	}
	return ""
}

func programAndScript(args string) []string {
	parts := strings.Split(args, " ")
	var out []string
	if parts[0] != "" {
		out = append(out, parts[0])
	}
	for _, t := range parts[1:] {
		if t != "" && !strings.HasPrefix(t, "-") {
			out = append(out, t)
			break
		}
	}
	return out
}

// serviceInput is what tells a production service running as the runtime user apart.
type serviceInput struct {
	cgroup, exe, args, cwd string
	// runtimeUnits are the runtime's own units: agent dev servers live in them through tmux, so they do not make a
	// process a service.
	runtimeUnits map[string]bool
}

// isSystemService: a systemd system unit other than the runtime's, or a program from the release root. The cwd
// catches a production runtime seen from a dev one in the same unit (its exe is node, its argv rewritten).
func isSystemService(in serviceInput, releaseRoot string) bool {
	if u := systemServiceUnit(in.cgroup); u != "" && !in.runtimeUnits[u] {
		return true
	}
	if releaseRoot == "" {
		return false
	}
	for _, v := range append([]string{in.exe, in.cwd}, programAndScript(in.args)...) {
		if strings.HasPrefix(v, releaseRoot) || v == strings.TrimSuffix(releaseRoot, "/") {
			return true
		}
	}
	return false
}

type kindInput struct {
	comm, args                                      string
	hasPort, isPanePid, hasAgentAncestor, isRuntime bool
	isSystemService                                 bool
}

// classifyKind: a portless child of an agent is its tool (an MCP server, a helper): stopping it breaks the agent. A
// dev server it started listens.
func classifyKind(in kindInput) string {
	if in.isRuntime || in.isSystemService || isServiceComm(in.comm) {
		return "service"
	}
	if in.isPanePid || isAgentProcess(in.comm, in.args) || (in.hasAgentAncestor && (shellComms[in.comm] || !in.hasPort)) {
		return "agent"
	}
	if in.hasPort {
		return "dev"
	}
	return "other"
}

func isStoppableKind(kind string) bool { return kind == "dev" || kind == "other" }

type portProbe struct {
	port       int
	responding *bool // nil: no fresh probe result this scan, the count stays
}

// stuckAfter counts consecutive silent scans per port; ports no longer listed are forgotten.
func stuckAfter(prev map[int]int, ports []portProbe) (map[int]int, map[int]bool) {
	misses := map[int]int{}
	stuck := map[int]bool{}
	for _, p := range ports {
		count := prev[p.port]
		if p.responding != nil {
			if *p.responding {
				count = 0
			} else {
				count++
			}
		}
		misses[p.port] = count
		if count >= stuckScans {
			stuck[p.port] = true
		}
	}
	return misses, stuck
}

var redactions = func() []func(string) string {
	sp := jscompat.SpaceClass[1 : len(jscompat.SpaceClass)-1]
	value := `[^` + sp + `"']+`
	rules := []struct{ re, repl string }{
		{`(?i)(authorization[:=][` + sp + `]*)(?:(?:bearer|basic|token)[` + sp + `]+)?` + value, "${1}***"},
		{`(?i)\b(bearer[` + sp + `]+)[\w.~+/=-]+`, "${1}***"},
		{`(?i)([a-z][a-z0-9+.-]*://[^` + sp + `:/@]*):[^` + sp + `@/]+@`, "${1}:***@"},
	}
	var out []func(string) string
	for _, r := range rules {
		re, repl := regexp.MustCompile(r.re), r.repl
		out = append(out, func(s string) string { return re.ReplaceAllString(s, repl) })
	}
	out = append(out, redactDashedSecrets, redactSecretAssignments)
	pFlag := regexp.MustCompile(`([` + sp + `]-p)[^` + sp + `"'-][^` + sp + `"']*`)
	out = append(out, func(s string) string { return pFlag.ReplaceAllString(s, "${1}***") })
	return out
}()

// redactCommand: command lines may carry credentials, so the value of anything named like a secret is hidden, then
// the line is cut.
func redactCommand(args string) string {
	for _, r := range redactions {
		args = r(args)
	}
	return jscompat.Slice(args, commandMax)
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isNameByte(c byte) bool { return isWordByte(c) || c == '-' }

var secretWords = []string{"token", "secret", "password", "passwd", "apikey", "api_key", "api-key", "key"}

// secretName says whether a whole name is named like a secret: it holds a secret word (auth, but not author…), or
// "pat" as a segment of its own (never "path"), or it is a DSN-style name.
func secretName(name string) bool {
	n := strings.ToLower(name)
	for _, w := range secretWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	for i := 0; ; {
		j := strings.Index(n[i:], "auth")
		if j < 0 {
			break
		}
		if !strings.HasPrefix(n[i+j+4:], "or") {
			return true
		}
		i += j + 1
	}
	for i := 0; ; {
		j := strings.Index(n[i:], "pat")
		if j < 0 {
			break
		}
		at := i + j
		before := at == 0 || n[at-1] == '_' || n[at-1] == '-'
		end := at + 3
		after := end == len(n) || n[end] == '_' || n[end] == '-'
		if before && after {
			return true
		}
		i = at + 1
	}
	return n == "database_url" || strings.HasSuffix(n, "_dsn")
}

func isSpaceAt(s string, i int) (int, bool) {
	r, size := utf8.DecodeRuneInString(s[i:])
	return size, size > 0 && jscompat.IsSpace(r)
}

// valueEnd is where a [^\s"']+ value starting at i ends; i when there is none.
func valueEnd(s string, i int) int {
	j := i
	for j < len(s) {
		if s[j] == '"' || s[j] == '\'' {
			break
		}
		if _, sp := isSpaceAt(s, j); sp {
			break
		}
		_, size := utf8.DecodeRuneInString(s[j:])
		j += size
	}
	return j
}

// redactDashedSecrets: --name=value and --name value of a secret name; the value becomes ***.
func redactDashedSecrets(s string) string {
	var b strings.Builder
	last := 0
	for p := 0; p < len(s); p++ {
		if s[p] != '-' {
			continue
		}
		e := p
		for e < len(s) && isNameByte(s[e]) {
			e++
		}
		name1 := s[p+1 : e]
		ok := secretName(name1) && name1 != ""
		if !ok && p+1 < len(s) && s[p+1] == '-' && p+2 <= e {
			name2 := s[p+2 : e]
			ok = name2 != "" && secretName(name2)
		}
		if !ok || e >= len(s) {
			continue
		}
		sepEnd := e
		if s[e] == '=' {
			sepEnd = e + 1
		} else {
			for sepEnd < len(s) {
				n, sp := isSpaceAt(s, sepEnd)
				if !sp {
					break
				}
				sepEnd += n
			}
			if sepEnd == e {
				continue
			}
		}
		v := valueEnd(s, sepEnd)
		if v == sepEnd {
			continue
		}
		b.WriteString(s[last:sepEnd])
		b.WriteString("***")
		last, p = v, v-1
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// redactSecretAssignments: NAME=value of a secret name starting at a word boundary.
func redactSecretAssignments(s string) string {
	var b strings.Builder
	last := 0
	for p := 0; p < len(s); p++ {
		if !isNameByte(s[p]) {
			continue
		}
		prevWord := p > 0 && isWordByte(s[p-1])
		if prevWord == isWordByte(s[p]) {
			continue // no \b here
		}
		e := p
		for e < len(s) && isNameByte(s[e]) {
			e++
		}
		if e >= len(s) || s[e] != '=' || !secretName(s[p:e]) {
			continue
		}
		v := valueEnd(s, e+1)
		if v == e+1 {
			continue
		}
		b.WriteString(s[last : e+1])
		b.WriteString("***")
		last, p = v, v-1
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}
