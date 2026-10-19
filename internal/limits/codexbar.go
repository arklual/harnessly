package limits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"harnessly/internal/jscompat"
)

// Claude subscription usage from the codexbar CLI, once a minute. Only percentages, reset times, the plan name and
// codexbar's pace are kept: the account identity never leaves the parser. A failure is stored as `codexbarError` next
// to the old values, never instead of them. codexbar's HTTP server is not used: its /usage times out on Claude.

const (
	CodexbarInterval  = 60 * time.Second
	codexbarTimeout   = 20 * time.Second
	codexbarMaxOutput = 1 << 20
)

// CodexbarArgs is the fixed command line.
var CodexbarArgs = []string{"usage", "--provider", "claude", "--source", "oauth", "--format", "json"}

var (
	emailLike    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	controlChars = regexp.MustCompile(`[\x00-\x1f\x7f]+`)
	slots        = []string{"primary", "secondary", "tertiary"}
)

// Sanitize is error text safe to store and show: no e-mail addresses, no control characters, at most 200 units.
func Sanitize(text string) string {
	text = emailLike.ReplaceAllString(text, "<email>")
	text = controlChars.ReplaceAllString(text, " ")
	return jscompat.Slice(jscompat.Trim(text), 200)
}

func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// resetSeconds is an ISO string or epoch seconds (milliseconds when obviously so).
func resetSeconds(v any) (float64, bool) {
	if finite(v) {
		f := v.(float64)
		if f > 1e11 {
			return jsRound(f / 1000), true
		}
		return f, true
	}
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	ms := jscompat.ParseDate(s)
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return 0, false
	}
	return jsRound(ms / 1000), true
}

func codexbarWindow(raw any, at float64) (minutes float64, window map[string]any, ok bool) {
	m, isObj := object(raw)
	if !isObj || !finite(m["usedPercent"]) {
		return 0, nil, false
	}
	reset, ok := resetSeconds(m["resetsAt"])
	if !ok {
		return 0, nil, false
	}
	minutes = math.NaN()
	if finite(m["windowMinutes"]) {
		minutes = m["windowMinutes"].(float64)
	}
	return minutes, map[string]any{"used_percentage": m["usedPercent"], "resets_at": reset, "at": at, "source": SourceCodexbar}, true
}

func windowKeyOfMinutes(minutes float64) string {
	switch minutes {
	case 300:
		return "five_hour"
	case 10080:
		return "seven_day"
	}
	return ""
}

func codexbarPace(raw any) any {
	m, ok := object(raw)
	if !ok {
		return nil
	}
	stage, okStage := m["stage"].(string)
	summary, okSummary := m["summary"].(string)
	last, okLast := m["willLastToReset"].(bool)
	if !okStage || !okSummary || !okLast || !finite(m["deltaPercent"]) || !finite(m["expectedUsedPercent"]) {
		return nil
	}
	var eta any
	if finite(m["etaSeconds"]) {
		eta = m["etaSeconds"]
	}
	return map[string]any{"stage": stage, "summary": Sanitize(summary), "deltaPercent": m["deltaPercent"], "willLastToReset": last,
		"etaSeconds": eta, "expectedUsedPercent": m["expectedUsedPercent"]}
}

// ParseCodexbar reads codexbar's answer measured at now (epoch seconds): the provider data, or the failure message.
func ParseCodexbar(stdout []byte, now float64) (Data, string) {
	var parsed any
	if json.Unmarshal(stdout, &parsed) != nil {
		return nil, "codexbar: ответ не JSON"
	}
	entries, isList := parsed.([]any)
	if !isList {
		entries = []any{parsed}
	}
	var entry map[string]any
	for _, item := range entries {
		if m, ok := object(item); ok && m["provider"] == "claude" {
			entry = m
			break
		}
	}
	if entry == nil {
		return nil, "codexbar: в ответе нет Claude"
	}
	if e, ok := object(entry["error"]); ok {
		msg, isStr := e["message"].(string)
		if !isStr {
			msg = "без описания"
		}
		return nil, "codexbar: " + Sanitize(msg)
	}
	usage, ok := object(entry["usage"])
	if !ok {
		return nil, "codexbar: в ответе нет usage"
	}
	data := Data{"ts": now, "five_hour": nil, "seven_day": nil, "source": SourceCodexbar, "codexbarError": nil}
	slotWindow := map[string]string{}
	for _, slot := range slots {
		if usage[slot] == nil {
			continue
		}
		minutes, w, ok := codexbarWindow(usage[slot], now)
		if !ok {
			return nil, "codexbar: окно " + slot + " не по контракту"
		}
		key := windowKeyOfMinutes(minutes)
		if key == "" || data[key] != nil {
			continue
		}
		data[key] = w
		slotWindow[slot] = key
	}
	if data["five_hour"] == nil && data["seven_day"] == nil {
		return nil, "codexbar: нет окон 5 ч и 7 д"
	}
	if login, ok := usage["loginMethod"].(string); ok && jscompat.Trim(login) != "" {
		data["plan"] = map[string]any{"name": Sanitize(login), "at": now}
	}
	if items, ok := usage["extraRateWindows"].([]any); ok {
		extra := []any{}
		for _, item := range items {
			m, isObj := object(item)
			id, okID := m["id"].(string)
			title, okTitle := m["title"].(string)
			if !isObj || !okID || !okTitle {
				continue
			}
			if _, w, ok := codexbarWindow(m["window"], now); ok {
				extra = append(extra, map[string]any{"id": jscompat.Slice(id, 80), "title": Sanitize(title), "window": w})
			}
		}
		data["extra"] = extra
	}
	if paces, ok := object(entry["pace"]); ok {
		pace := map[string]any{"at": now}
		for _, slot := range slots {
			if key := slotWindow[slot]; key != "" {
				pace[key] = codexbarPace(paces[slot])
			}
		}
		data["pace"] = pace
	}
	return data, ""
}

// CodexbarBin is the configured CLI when it is an absolute path; otherwise there is no poller at all.
func CodexbarBin(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return ""
}

// RunCodexbar runs the CLI; codexbar exits 1 with a JSON error entry on stdout, which is more useful than the exit code.
type RunCodexbar func(ctx context.Context, bin string, args []string) ([]byte, error)

type limitedBuffer struct {
	bytes.Buffer
	cancel context.CancelFunc
	over   bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := codexbarMaxOutput - b.Len(); len(p) > room {
		b.Buffer.Write(p[:max(room, 0)])
		b.over = true
		b.cancel()
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func runCodexbarCLI(ctx context.Context, bin string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, codexbarTimeout)
	defer cancel()
	out := &limitedBuffer{cancel: cancel}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = out
	err := cmd.Run()
	if err != nil && out.Len() == 0 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("таймаут " + strconv.Itoa(int(codexbarTimeout/time.Second)) + " с")
		}
		return nil, errors.New("не запустился (" + failureCode(err) + ")")
	}
	return out.Bytes(), nil
}

// failureCode is what Node puts in error.code: the errno name of a failed start, the exit code of a failed run.
func failureCode(err error) string {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if code := exit.ExitCode(); code >= 0 {
			return strconv.Itoa(code)
		}
		return "ошибка"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ENOENT:
			return "ENOENT"
		case syscall.EACCES:
			return "EACCES"
		case syscall.ENOEXEC:
			return "ENOEXEC"
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "ENOENT"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "EACCES"
	}
	return "ошибка"
}

// PollCodexbarOnce runs codexbar once and writes the result (or the failure) as Claude's entry.
func PollCodexbarOnce(ctx context.Context, bin string, run RunCodexbar, cache *Cache, clock func() float64) (Data, error) {
	if run == nil {
		run = runCodexbarCLI
	}
	var data Data
	var failure string
	if stdout, err := run(ctx, bin, CodexbarArgs); err != nil {
		failure = "codexbar: " + Sanitize(err.Error())
	} else {
		data, failure = ParseCodexbar(stdout, clock())
	}
	if data == nil {
		now := clock()
		data = Data{"ts": now, "five_hour": nil, "seven_day": nil, "codexbarError": map[string]any{"message": failure, "at": now}}
	}
	_, err := cache.Write("claude", data)
	return data, err
}
