package limits

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

const codexTailBytes = 1 << 20

func codexWindow(v any, now float64) any {
	w, ok := object(v)
	if !ok || !finite(w["used_percent"]) {
		return nil
	}
	var reset float64
	switch {
	case finite(w["resets_at"]):
		reset = w["resets_at"].(float64)
	case finite(w["resets_in_seconds"]):
		reset = jsRound(now + w["resets_in_seconds"].(float64))
	default:
		return nil
	}
	return map[string]any{"used_percentage": w["used_percent"], "resets_at": reset}
}

// normalizeCodex turns Codex's rate_limits into an entry; nil when neither window is usable.
func normalizeCodex(rl any, now float64) Data {
	m, _ := object(rl)
	var five, seven any
	for _, slot := range []string{"primary", "secondary"} {
		if !truthy(m[slot]) {
			continue
		}
		w := codexWindow(m[slot], now)
		if w == nil {
			continue
		}
		minutes, _ := object(m[slot])
		switch minutes["window_minutes"] {
		case 300.0:
			five = w
		case 10080.0:
			seven = w
		}
	}
	// Only a window of unknown length falls back on its slot: a Pro plan's weekly primary is not a five-hour one.
	if p, _ := object(m["primary"]); !hasKey(p, "window_minutes") && five == nil {
		five = codexWindow(m["primary"], now)
	}
	if s, _ := object(m["secondary"]); !hasKey(s, "window_minutes") && seven == nil {
		seven = codexWindow(m["secondary"], now)
	}
	if five == nil && seven == nil {
		return nil
	}
	return Data{"ts": now, "five_hour": five, "seven_day": seven}
}

func hasKey(m map[string]any, k string) bool {
	_, has := m[k]
	return has
}

// latestCodexLimits is the newest token_count rate limits among the lines; the newest such line decides even when it
// has no usable window.
func latestCodexLimits(lines []string, now float64) Data {
	for i := len(lines) - 1; i >= 0; i-- {
		var entry map[string]any
		if json.Unmarshal([]byte(lines[i]), &entry) != nil || entry["type"] != "event_msg" {
			continue
		}
		payload, _ := object(entry["payload"])
		if payload["type"] != "token_count" || !truthy(payload["rate_limits"]) {
			continue
		}
		return normalizeCodex(payload["rate_limits"], now)
	}
	return nil
}

// CodexTranscriptLimits reads the limits Codex wrote into the tail of its transcript; nil when there are none.
func CodexTranscriptLimits(path string, now float64) Data {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return nil
	}
	n := min(st.Size(), codexTailBytes)
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, st.Size()-n); err != nil && err != io.EOF {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(strings.ToValidUTF8(string(buf), "�"), "\n") {
		if jscompat.Trim(line) != "" {
			lines = append(lines, line)
		}
	}
	if st.Size() > n && len(lines) > 0 {
		lines = lines[1:] // the first line was cut
	}
	return latestCodexLimits(lines, now)
}
