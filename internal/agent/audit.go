package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Audit is agent-audit.log: one JSON line for each operation that changes something on the machine, and for each thing
// the policy refused. It says what was done and to which name, never a key, a code, a password or a secret's value.
type Audit struct {
	path string
	mu   sync.Mutex
}

// auditMax is the size at which the log is set aside as .1 (replacing an older one).
const auditMax = 5 << 20

// NewAudit writes to path.
func NewAudit(path string) *Audit { return &Audit{path: path} }

// Record appends a line. fields are the operation's own, already safe to show; a failure to write is the log's alone.
func (a *Audit) Record(op string, ok bool, fields map[string]any) {
	if a == nil {
		return
	}
	line := map[string]any{"t": time.Now().UTC().Format(time.RFC3339), "op": op, "ok": ok}
	for k, v := range fields {
		if k != "t" && k != "op" && k != "ok" {
			line[k] = v
		}
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return
	}
	if st, err := os.Stat(a.path); err == nil && st.Size() > auditMax {
		_ = os.Rename(a.path, a.path+".1")
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|noFollow, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}
