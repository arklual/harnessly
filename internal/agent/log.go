package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
	"time"
)

var logMu sync.Mutex

// F is one extra field of a log line, kept in the order given.
type F struct {
	K string
	V any
}

// Log writes one JSON line: level, msg, the extra fields, time. Info goes to stdout, warn and error to stderr.
func Log(level, msg string, fields ...F) {
	var b bytes.Buffer
	b.WriteString(`{"level":`)
	writeJSON(&b, level)
	b.WriteString(`,"msg":`)
	writeJSON(&b, msg)
	for _, f := range fields {
		b.WriteByte(',')
		writeJSON(&b, f.K)
		b.WriteByte(':')
		writeJSON(&b, f.V)
	}
	b.WriteString(`,"time":`)
	writeJSON(&b, isoNow())
	b.WriteString("}\n")
	out := os.Stdout
	if level != "info" {
		out = os.Stderr
	}
	logMu.Lock()
	defer logMu.Unlock()
	_, _ = out.Write(b.Bytes())
}

func writeJSON(b *bytes.Buffer, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		raw = []byte(`null`)
	}
	b.Write(raw)
}

// isoNow is the time as JavaScript's toISOString writes it.
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
