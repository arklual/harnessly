package wire

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
)

// MaxRuntimeSettingsBody is the largest PUT /api/v2/runtime-settings body.
const MaxRuntimeSettingsBody = 4 << 10

// RuntimeSettingsStore keeps the settings of the runtime itself (RuntimeSettings).
type RuntimeSettingsStore interface {
	RuntimeSettings() RuntimeSettings
	SetRuntimeSettings(s RuntimeSettings) (RuntimeSettings, error)
}

// FileRuntimeSettings is a RuntimeSettingsStore in a file. A file that is missing or does not read is the defaults; so is
// a value it does not know.
type FileRuntimeSettings struct {
	Path string

	mu sync.Mutex
}

// DefaultRuntimeSettings are the settings of a runtime nobody set.
func DefaultRuntimeSettings() RuntimeSettings {
	return RuntimeSettings{IdleStop: DefaultIdleStop, LimitContinue: On, RestartContinue: On}
}

// over is s with what it leaves out taken from base: a file or a PUT from before a switch existed keeps that switch.
func (s RuntimeSettings) over(base RuntimeSettings) RuntimeSettings {
	if s.LimitContinue == "" {
		s.LimitContinue = base.LimitContinue
	}
	if s.RestartContinue == "" {
		s.RestartContinue = base.RestartContinue
	}
	if s.Delegation == nil {
		s.Delegation = base.Delegation
	}
	return s
}

func (f *FileRuntimeSettings) RuntimeSettings() RuntimeSettings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.load()
}

func (f *FileRuntimeSettings) load() RuntimeSettings {
	out := DefaultRuntimeSettings()
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return out
	}
	var saved RuntimeSettings
	if json.Unmarshal(raw, &saved) == nil && saved.Validate() == nil {
		out = saved.over(out)
	}
	return out
}

func (f *FileRuntimeSettings) SetRuntimeSettings(s RuntimeSettings) (RuntimeSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s = s.over(f.load())
	raw, err := json.Marshal(s)
	if err != nil {
		return f.load(), err
	}
	if err := writeFileAtomic(f.Path, raw); err != nil {
		return f.load(), err
	}
	return s, nil
}

// runtimeSettings serves GET /api/v2/runtime-settings and PUT {idleStop, limitContinue?, restartContinue?, delegation?}:
// the settings as they are after it.
func (s *Server) runtimeSettings(w http.ResponseWriter, r *http.Request) {
	if s.RuntimeSettings == nil {
		refuse(w, RefuseNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, settingsFor(s.RuntimeSettings.RuntimeSettings(), CapsOf(r)))
	case http.MethodPut:
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRuntimeSettingsBody))
		var next RuntimeSettings
		if err == nil {
			err = decodeStrict(raw, &next)
		}
		if err == nil {
			err = next.Validate()
		}
		if err != nil {
			refuse(w, RefuseBadRequest)
			return
		}
		got, err := s.RuntimeSettings.SetRuntimeSettings(next)
		if err != nil {
			refuse(w, RefuseInternal)
			return
		}
		writeJSON(w, http.StatusOK, settingsFor(got, CapsOf(r)))
	default:
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
