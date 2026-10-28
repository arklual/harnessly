package agent

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// F is one extra field of a log line, kept in the order given.
type F struct {
	K string
	V any
}

// Log writes one JSON line: level, msg, the extra fields, time. Info goes to stdout, warn and error to stderr.
func Log(level, msg string, fields ...F) {
	out := make([]jsonlog.F, len(fields))
	for i, f := range fields {
		out[i] = jsonlog.F{K: f.K, V: f.V}
	}
	jsonlog.Log(level, msg, out...)
}

func isoNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

// marshal is json.Marshal without escaping <, > and &, the way JSON.stringify writes them.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
