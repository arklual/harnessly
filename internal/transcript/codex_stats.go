package transcript

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

// The session statistics of a Codex transcript (readCodexTimelineSessionStats of the previous runtime's Codex stats):
// the session of the first line, the tokens of the last token_count and the model of the last turn in the last MiB.

const (
	codexStatsTailBytes = 1 << 20
	codexStatsTTL       = 30_000
)

type codexSessionStats struct {
	sessionID                               any
	inputTokens, cachedInputTokens          any
	outputTokens, reasoningOutputTokens     any
	currentContextTokens, contextWindowSize any
	cost                                    any
	usedPercentage                          any
	model, effort                           any
}

type codexStatsCached struct {
	stats    *codexSessionStats
	mtime    time.Time
	cachedAt float64
}

var codexStatsCache = struct {
	sync.Mutex
	m map[string]codexStatsCached
}{m: map[string]codexStatsCached{}}

// readlineLines splits text as Node's readline does with crlfDelay Infinity: \r\n, \n and a lone \r end a line, and a
// last line is one only when it is not empty.
func readlineLines(text string, fn func(line string) bool) {
	for text != "" {
		i := strings.IndexAny(text, "\r\n")
		if i < 0 {
			fn(text)
			return
		}
		next := i + 1
		if text[i] == '\r' && next < len(text) && text[next] == '\n' {
			next++
		}
		if !fn(text[:i]) {
			return
		}
		text = text[next:]
	}
}

// readCodexMeta is readSessionMetaLine: the session id and start time of the first line when it is the session's meta.
func readCodexMeta(path string) (sessionID any, startedAt float64) {
	sessionID = nil
	f, err := os.Open(path)
	if err != nil {
		panic(codexThrow{})
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64<<10)
	for done := false; !done; {
		piece, err := br.ReadBytes('\n')
		if err != nil && err != io.EOF {
			panic(codexThrow{})
		}
		done = err == io.EOF
		readlineLines(decodeUTF8(piece), func(l string) bool {
			trimmed := jscompat.Trim(l)
			if trimmed == "" {
				return true
			}
			done = true
			sessionID, startedAt = codexMetaOf(trimmed)
			return false
		})
	}
	return sessionID, startedAt
}

func codexMetaOf(line string) (any, float64) {
	parsed, ok := parseJSON(line)
	if !ok || parsed == nil {
		return nil, 0
	}
	payload := prop(parsed, "payload")
	if prop(parsed, "type") != "session_meta" || !codexTruthy(payload) {
		return nil, 0
	}
	parse := func(v any) (float64, bool) {
		s, ok := jsToString(v)
		if !ok {
			return 0, false
		}
		return jscompat.ParseDate(s), true
	}
	var started float64
	switch {
	case codexTruthy(prop(payload, "timestamp")):
		if started, ok = parse(prop(payload, "timestamp")); !ok {
			return nil, 0
		}
	case codexTruthy(prop(parsed, "timestamp")):
		if started, ok = parse(prop(parsed, "timestamp")); !ok {
			return nil, 0
		}
	}
	return codexCoalesce(prop(payload, "id"), nil), started
}

// readCodexTailLines is readTailLines: the lines of the last size bytes, the first (cut) one dropped.
func readCodexTailLines(path string, size, fileSize int64) []string {
	start := max(0, fileSize-size)
	f, err := os.Open(path)
	if err != nil {
		panic(codexThrow{})
	}
	defer f.Close()
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		panic(codexThrow{})
	}
	b, err := io.ReadAll(f)
	if err != nil {
		panic(codexThrow{})
	}
	var lines []string
	readlineLines(decodeUTF8(b), func(l string) bool {
		lines = append(lines, l)
		return true
	})
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	return lines
}

func extractCodexTailInfo(lines []string) (info any, haveTokens bool, model, effort any) {
	model, effort = nil, nil
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] == "" {
			continue
		}
		parsed, ok := parseJSON(lines[i])
		if !ok || parsed == nil {
			continue
		}
		payload := prop(parsed, "payload")
		if !codexTruthy(payload) {
			continue
		}
		if !codexTruthy(model) && prop(parsed, "type") == "turn_context" {
			if m, ok := prop(payload, "model").(string); ok {
				model = m
				effort = nil
				if e, ok := prop(payload, "effort").(string); ok {
					effort = e
				}
			}
		}
		if !haveTokens && prop(parsed, "type") == "event_msg" && prop(payload, "type") == "token_count" && codexTruthy(prop(payload, "info")) {
			info, haveTokens = prop(payload, "info"), true
		}
		if haveTokens && codexTruthy(model) {
			break
		}
	}
	return info, haveTokens, model, effort
}

type openAIPrice struct{ input, cachedInput, output float64 }

type openAIPricing struct {
	openAIPrice
	long *openAIPrice
}

// USD per million tokens, the previous runtime's table.
var openAIPrices = map[string]openAIPricing{
	"gpt-5.5":             {openAIPrice{5, 0.5, 30}, &openAIPrice{10, 1, 45}},
	"gpt-5.4":             {openAIPrice{2.5, 0.25, 15}, &openAIPrice{5, 0.5, 22.5}},
	"gpt-5.4-mini":        {openAIPrice{0.75, 0.075, 4.5}, nil},
	"gpt-5.4-nano":        {openAIPrice{0.2, 0.02, 1.25}, nil},
	"gpt-5.2-codex":       {openAIPrice{1.75, 0.175, 14}, nil},
	"gpt-5.2-chat-latest": {openAIPrice{1.75, 0.175, 14}, nil},
	"gpt-5.2":             {openAIPrice{1.75, 0.175, 14}, nil},
	"gpt-5.1-codex-max":   {openAIPrice{1.25, 0.125, 10}, nil},
	"gpt-5.1-codex":       {openAIPrice{1.25, 0.125, 10}, nil},
	"gpt-5.1-chat-latest": {openAIPrice{1.25, 0.125, 10}, nil},
	"gpt-5.1":             {openAIPrice{1.25, 0.125, 10}, nil},
	"gpt-5-codex":         {openAIPrice{1.25, 0.125, 10}, nil},
	"gpt-5-chat-latest":   {openAIPrice{1.25, 0.125, 10}, nil},
	"gpt-5-mini":          {openAIPrice{0.25, 0.025, 2}, nil},
	"gpt-5-nano":          {openAIPrice{0.05, 0.005, 0.4}, nil},
	"gpt-5":               {openAIPrice{1.25, 0.125, 10}, nil},
	"gpt-4.1-mini":        {openAIPrice{0.4, 0.1, 1.6}, nil},
	"gpt-4.1-nano":        {openAIPrice{0.1, 0.025, 0.4}, nil},
	"gpt-4.1":             {openAIPrice{2, 0.5, 8}, nil},
	"gpt-4o-mini":         {openAIPrice{0.15, 0.075, 0.6}, nil},
	"gpt-4o":              {openAIPrice{2.5, 1.25, 10}, nil},
}

// openAIPriceKeys are the table's keys longest first, the order a prefix is looked up in (a stable sort of the table's
// own order).
var openAIPriceKeys = []string{
	"gpt-5.2-chat-latest", "gpt-5.1-chat-latest", "gpt-5.1-codex-max", "gpt-5-chat-latest", "gpt-5.2-codex",
	"gpt-5.1-codex", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-4.1-mini", "gpt-4.1-nano", "gpt-5-codex", "gpt-4o-mini",
	"gpt-5-mini", "gpt-5-nano", "gpt-5.5", "gpt-5.4", "gpt-5.2", "gpt-5.1", "gpt-4.1", "gpt-4o", "gpt-5",
}

var (
	modelPrefixRE = regexp.MustCompile(`^(openai/|models/)`)
	modelDateRE   = regexp.MustCompile(`-[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	modelDate8RE  = regexp.MustCompile(`-[0-9]{8}$`)
)

func pricingForModel(model string) *openAIPricing {
	n := jscompat.Trim(strings.ToLower(model))
	n = modelPrefixRE.ReplaceAllString(n, "")
	n = modelDateRE.ReplaceAllString(n, "")
	n = modelDate8RE.ReplaceAllString(n, "")
	if p, ok := openAIPrices[n]; ok {
		return &p
	}
	for _, k := range openAIPriceKeys {
		if n == k || strings.HasPrefix(n, k+"-") {
			p := openAIPrices[k]
			return &p
		}
	}
	return nil
}

// openAICost is calculateOpenAICost; the numbers are the token fields as Number() reads them.
func openAICost(model any, input, cached, output float64, window any, windowNum float64) any {
	m, _ := model.(string)
	if m == "" {
		return nil
	}
	base := pricingForModel(m)
	if base == nil {
		return nil
	}
	price := base.openAIPrice
	if base.long != nil && codexTruthy(window) && windowNum > 200_000 {
		price = *base.long
	}
	c := jsMax(0, cached)
	nonCached := jsMax(0, input-c)
	const mtok = 1_000_000
	return float64(float64(nonCached/mtok)*price.input) + float64(float64(c/mtok)*price.cachedInput) +
		float64(float64(jsMax(0, output)/mtok)*price.output)
}

func parseCodexStats(path string, size int64) *codexSessionStats {
	sessionID, startedAt := readCodexMeta(path)
	if !codexTruthy(sessionID) || !jscompat.Truthy(startedAt) {
		return nil
	}
	info, _, model, effort := extractCodexTailInfo(readCodexTailLines(path, codexStatsTailBytes, size))
	usage := codexCoalesce(prop(info, "total_token_usage"), prop(info, "last_token_usage"))
	field := func(v any, k string) any { return codexCoalesce(prop(v, k), 0.0) }
	st := &codexSessionStats{
		sessionID:             sessionID,
		inputTokens:           field(usage, "input_tokens"),
		cachedInputTokens:     field(usage, "cached_input_tokens"),
		outputTokens:          field(usage, "output_tokens"),
		reasoningOutputTokens: field(usage, "reasoning_output_tokens"),
		model:                 model,
		effort:                effort,
	}
	raw := codexCoalesce(codexCoalesce(prop(prop(info, "total_token_usage"), "total_tokens"), prop(prop(info, "last_token_usage"), "total_tokens")), 0.0)
	st.currentContextTokens = codexCoalesce(prop(prop(info, "last_token_usage"), "total_tokens"), raw)
	st.contextWindowSize = codexCoalesce(prop(info, "model_context_window"), 0.0)

	// Number() of an object with its own toString throws, and the previous runtime answered null.
	var nums [5]float64
	for i, v := range []any{st.inputTokens, st.cachedInputTokens, st.outputTokens, st.contextWindowSize, st.currentContextTokens} {
		n, ok := jsToNumber(v)
		if !ok {
			return nil
		}
		nums[i] = n
	}
	input, cached, output, window, current := nums[0], nums[1], nums[2], nums[3], nums[4]
	st.usedPercentage = nil
	if window > 0 {
		st.usedPercentage = jsRound(float64(current/window) * 100)
	}
	st.cost = openAICost(model, input, cached, output, st.contextWindowSize, window)
	return st
}

func cachedCodexStats(path string) *codexSessionStats {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	codexStatsCache.Lock()
	c, ok := codexStatsCache.m[path]
	codexStatsCache.Unlock()
	if ok && c.mtime.Equal(st.ModTime()) && Now()-c.cachedAt < codexStatsTTL {
		return c.stats
	}
	stats := func() (out *codexSessionStats) {
		defer recoverCodex(func() { out = nil })
		return parseCodexStats(path, st.Size())
	}()
	codexStatsCache.Lock()
	codexStatsCache.m[path] = codexStatsCached{stats: stats, mtime: st.ModTime(), cachedAt: Now()}
	codexStatsCache.Unlock()
	return stats
}

func clearCodexStatsCache() {
	codexStatsCache.Lock()
	codexStatsCache.m = map[string]codexStatsCached{}
	codexStatsCache.Unlock()
}

// CodexTimelineStats is the session statistics of a Codex transcript (readCodexTimelineSessionStats); nil for null.
func CodexTimelineStats(path string) *jsvalue.Object {
	s := cachedCodexStats(path)
	if s == nil {
		return nil
	}
	current, _ := jsToNumber(s.currentContextTokens)
	return jsvalue.NewObject(
		"sessionId", s.sessionID,
		"transcriptPath", path,
		"inputTokens", s.inputTokens,
		"cachedInputTokens", s.cachedInputTokens,
		"outputTokens", s.outputTokens,
		"reasoningOutputTokens", s.reasoningOutputTokens,
		"cost", s.cost,
		"currentContextTokens", s.currentContextTokens,
		"contextWindowSize", s.contextWindowSize,
		"usedPercentage", s.usedPercentage,
		"model", s.model,
		"effort", s.effort,
		"exceeds200k", current > 200_000,
		"receivedAt", Now(),
	)
}
