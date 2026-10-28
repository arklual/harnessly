package engine

import (
	"os"
	"path/filepath"
)

// Needs is what a preparation of the built-in recognition would still download.
type Needs struct {
	// Binary: no whisper-server is on the machine yet.
	Binary bool
	// Model is the model file that is not there yet (nil when it is, or a file of the operator's own is used).
	Model *Model
	// ModelPath is where that model goes.
	ModelPath string
	// Offline: downloads are off (RUNTIME_VOICE_DOWNLOAD=off).
	Offline bool
}

// Needs says what the engine would fetch at the next dictation; nothing for an engine that sends to a server of the
// person's own. The model is judged by its size only (the checksum is checked when it is used).
func (e *Engine) Needs() Needs {
	if e.UsesRemote() {
		return Needs{}
	}
	n := Needs{Offline: e.cfg.NoDownload}
	n.Binary = e.cfg.Bin == "" && e.localBinary() == ""
	if e.cfg.ModelFile == "" {
		m := e.pick()
		dest := filepath.Join(e.cfg.Dir, "models", m.File)
		if st, err := os.Stat(dest); err != nil || st.Size() != m.Size {
			n.Model, n.ModelPath = &m, dest
		}
	}
	return n
}

// ModelURL is where a model is downloaded from.
func (e *Engine) ModelURL(m Model) string { return e.cfg.ModelBase + m.File }

// BinaryURL is where the server binary of this machine is downloaded from.
func (e *Engine) BinaryURL() string {
	return e.cfg.BinaryBase + "whisper-server-" + e.cfg.GOOS + "-" + e.cfg.GOARCH
}
