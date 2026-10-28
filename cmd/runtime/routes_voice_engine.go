package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/voice"
	"github.com/ly-solutions/harnessly/internal/voice/engine"
)

// Settings → «Голосовой ввод» asks the machine how its dictation stands and which model it recognizes with:
//
//	GET <platform>/voice/engine   {decoder, builtin, state, stage?, percent?, error?, model, models, install}
//	PUT <platform>/voice/engine   {model: "auto"|"tiny"|"base"|"small"}: the same answer after it
//
// decoder: ffmpeg is there (without it the microphone is not offered at all, and install is the command that puts it
// there). builtin: the runtime's own engine recognizes here (not a service of the operator's, RUNTIME_VOICE_SOCKET);
// only then is the model the person's to choose. The choice is kept in the data folder (voice/model) and wins over
// RUNTIME_VOICE_MODEL; the engine takes it at the next dictation.

// voiceModels are the choices of the model, "auto" first (by the processor count).
var voiceModels = []string{"auto", "tiny", "base", "small"}

type voiceEngineAnswer struct {
	Decoder bool     `json:"decoder"`
	Builtin bool     `json:"builtin"`
	State   string   `json:"state"`
	Stage   string   `json:"stage,omitempty"`
	Percent int      `json:"percent,omitempty"`
	Error   string   `json:"error,omitempty"`
	Model   string   `json:"model"`
	Models  []string `json:"models"`
	Install string   `json:"install"`
}

// voiceModelFile keeps the person's choice of the model.
func voiceModelFile(dataDir string) string { return filepath.Join(dataDir, "voice", "model") }

// savedVoiceModel is the model chosen in Settings, "" when none was (the deployment's RUNTIME_VOICE_MODEL then).
func savedVoiceModel(dataDir string) string {
	raw, err := os.ReadFile(voiceModelFile(dataDir))
	if err != nil {
		return ""
	}
	m := strings.TrimSpace(string(raw))
	if _, ok := engine.Models[m]; ok || m == "auto" {
		return m
	}
	return ""
}

// voiceModelOf is the model the engine is configured with: the saved choice, else RUNTIME_VOICE_MODEL; "auto" is "".
func voiceModelOf(dataDir string) string {
	m := savedVoiceModel(dataDir)
	if m == "" {
		m = os.Getenv("RUNTIME_VOICE_MODEL")
	}
	if m == "auto" {
		return ""
	}
	return m
}

// usedBuiltin is the runtime's own engine when it is the one this machine dictates with, else nil. voiceSocket makes
// the engine only where it would be used anyway.
func usedBuiltin(dataDir string) *voiceBuiltin {
	sock := voiceSocket(dataDir)
	voiceEnginesMu.Lock()
	b := voiceEngines[dataDir]
	voiceEnginesMu.Unlock()
	if b == nil || b.eng == nil || b.socket == "" || b.socket != sock {
		return nil
	}
	return b
}

func ffmpegInstall() string {
	if runtime.GOOS == "darwin" {
		return "brew install ffmpeg"
	}
	return "sudo apt-get install ffmpeg"
}

func voiceEngineRoutes(d deps, dictation *voice.Server) []gate.Route {
	answer := func() voiceEngineAnswer {
		a := voiceEngineAnswer{Decoder: dictation.HasDecoder(), State: "external", Models: voiceModels, Install: ffmpegInstall()}
		a.Model = savedVoiceModel(d.DataDir)
		if a.Model == "" {
			a.Model = os.Getenv("RUNTIME_VOICE_MODEL")
		}
		if _, ok := engine.Models[a.Model]; !ok {
			a.Model = "auto"
		}
		b := usedBuiltin(d.DataDir)
		if b == nil {
			return a
		}
		a.Builtin = true
		st := b.eng.Status()
		a.State, a.Stage, a.Percent, a.Error = st.State, st.Stage, st.Percent, st.Error
		return a
	}
	return []gate.Route{{
		Slice: "voice",
		Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/voice/engine") },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !d.SessionOK(r) {
				peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			switch r.Method {
			case http.MethodGet:
				peerJSON(w, http.StatusOK, answer())
			case http.MethodPut:
				var body struct {
					Model string `json:"model"`
				}
				raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<10))
				if err == nil {
					err = json.Unmarshal(raw, &body)
				}
				_, known := engine.Models[body.Model]
				if err != nil || (!known && body.Model != "auto") {
					peerJSON(w, http.StatusBadRequest, map[string]string{"error": "model: auto, tiny, base or small"})
					return
				}
				path := voiceModelFile(d.DataDir)
				err = os.MkdirAll(filepath.Dir(path), 0o755)
				if err == nil {
					err = os.WriteFile(path, []byte(body.Model+"\n"), 0o644)
				}
				if err != nil {
					peerJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				if b := usedBuiltin(d.DataDir); b != nil {
					b.eng.SetModel(voiceModelOf(d.DataDir))
				}
				peerJSON(w, http.StatusOK, answer())
			default:
				w.Header().Set("Allow", "GET, PUT")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			}
		}),
	}}
}
