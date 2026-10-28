package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/voice"
	"github.com/ly-solutions/harnessly/internal/voice/engine"
)

// Settings → «Голосовой ввод» asks the machine how its dictation stands and what it recognizes with:
//
//	GET <platform>/voice/engine   {decoder, builtin, choosable, engine, state, stage?, percent?, error?, model, models,
//	                               sizes, remote?, install}
//	PUT <platform>/voice/engine   {model: "auto"|"tiny"|…}            the built-in whisper's model
//	                              {engine: "builtin"}                  back to the built-in whisper
//	                              {engine: "remote", url, model, key?} a server of the person's own (OpenAI's API);
//	                                                                   no key keeps the saved one, "" removes it
//	                              the same answer after it, or 400 {error} in the person's words
//
// decoder: ffmpeg is there (without it the microphone is not offered at all, and install is the command that puts it
// there). choosable: the runtime decides how this machine recognizes (no service of the operator's, RUNTIME_VOICE_SOCKET,
// is set up instead). builtin: the runtime's own whisper recognizes here; only then is its model the person's to choose.
// The choices are kept in the data folder — voice/model, and voice/server.json (mode 0600: it holds the server's key) —
// and win over RUNTIME_VOICE_MODEL; the engine takes them at the next dictation. The key never leaves the machine but
// in the Authorization header of the recognition requests: the page gets its last four characters (remote.keyMask).

// voiceModels are the choices of the model, "auto" first (by the processor count), then by size.
var voiceModels = func() []string {
	out := make([]string, 0, len(engine.Models)+1)
	for id := range engine.Models {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return engine.Models[out[i]].Size < engine.Models[out[j]].Size })
	return append([]string{"auto"}, out...)
}()

type voiceRemoteAnswer struct {
	URL     string `json:"url"`
	Model   string `json:"model"`
	KeyMask string `json:"keyMask"`
}

type voiceEngineAnswer struct {
	Decoder   bool               `json:"decoder"`
	Builtin   bool               `json:"builtin"`
	Choosable bool               `json:"choosable"`
	Engine    string             `json:"engine"`
	State     string             `json:"state"`
	Stage     string             `json:"stage,omitempty"`
	Percent   int                `json:"percent,omitempty"`
	Error     string             `json:"error,omitempty"`
	Model     string             `json:"model"`
	Models    []string           `json:"models"`
	Sizes     map[string]int64   `json:"sizes"`
	Remote    *voiceRemoteAnswer `json:"remote,omitempty"`
	Install   string             `json:"install"`
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

// voiceServer is what voice/server.json keeps: which engine recognizes, and the person's server (kept when they go
// back to the built-in whisper, so that switching again needs no retyping).
type voiceServer struct {
	Engine string `json:"engine"`
	engine.Remote
}

func voiceServerFile(dataDir string) string { return filepath.Join(dataDir, "voice", "server.json") }

// loadVoiceServer is the saved choice; the built-in whisper when there is none or it does not hold together.
func loadVoiceServer(dataDir string) voiceServer {
	raw, err := os.ReadFile(voiceServerFile(dataDir))
	if err != nil {
		return voiceServer{Engine: "builtin"}
	}
	var s voiceServer
	if json.Unmarshal(raw, &s) != nil {
		return voiceServer{Engine: "builtin"}
	}
	if s.Engine != "remote" || s.Remote.Check() != nil {
		s.Engine = "builtin"
	}
	return s
}

// saveVoiceServer writes the choice, private to the runtime's user from the first byte, and puts it in place by a rename.
func saveVoiceServer(dataDir string, s voiceServer) error {
	path := voiceServerFile(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, _ := json.Marshal(s)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".server-*.json")
	if err != nil {
		return err
	}
	_, err = tmp.Write(raw)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
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

// voiceChange is a PUT's body: a model of the built-in whisper, or an engine.
type voiceChange struct {
	Model  string  `json:"model"`
	Engine string  `json:"engine"`
	URL    string  `json:"url"`
	Key    *string `json:"key"`
}

var msgNewOriginKey = locale.T("Enter the key for the new address: the saved key belongs to the previous server.",
	"Введите ключ для нового адреса: сохранённый ключ принадлежит прежнему серверу.")

var errVoiceChange = errors.New("model: auto, " + strings.Join(voiceModels[1:], ", ") + "; or engine: builtin, remote")

func voiceEngineRoutes(d deps, dictation *voice.Server) []gate.Route {
	answer := func() voiceEngineAnswer {
		a := voiceEngineAnswer{Decoder: dictation.HasDecoder(), State: "external", Models: voiceModels,
			Sizes: map[string]int64{}, Install: ffmpegInstall(), Choosable: !externalVoice(), Engine: "builtin"}
		for id, m := range engine.Models {
			a.Sizes[id] = m.Size
		}
		a.Model = savedVoiceModel(d.DataDir)
		if a.Model == "" {
			a.Model = os.Getenv("RUNTIME_VOICE_MODEL")
		}
		if _, ok := engine.Models[a.Model]; !ok {
			a.Model = "auto"
		}
		if a.Choosable {
			s := loadVoiceServer(d.DataDir)
			a.Engine = s.Engine
			if s.URL != "" {
				a.Remote = &voiceRemoteAnswer{URL: s.URL, Model: s.Model, KeyMask: engine.MaskKey(s.Key)}
			}
		}
		b := usedBuiltin(d.DataDir)
		if b == nil {
			// The runtime's whisper cannot run here (no build for the processor, macOS without whisper-cpp): said as
			// such, not as a service of the operator's; a server of the person's own is the way out.
			if a.Choosable {
				if err := builtinVoice(d.DataDir).eng.Eligible(); err != nil {
					a.State, a.Error = "failed", err.Error()
				}
			}
			return a
		}
		a.Builtin = !b.eng.UsesRemote()
		st := b.eng.Status()
		a.State, a.Stage, a.Percent, a.Error = st.State, st.Stage, st.Percent, st.Error
		return a
	}
	// change applies a PUT; the error is the person's to read.
	change := func(c voiceChange) (int, error) {
		switch c.Engine {
		case "":
			if _, known := engine.Models[c.Model]; !known && c.Model != "auto" {
				return http.StatusBadRequest, errVoiceChange
			}
			path := voiceModelFile(d.DataDir)
			err := os.MkdirAll(filepath.Dir(path), 0o755)
			if err == nil {
				err = os.WriteFile(path, []byte(c.Model+"\n"), 0o644)
			}
			if err != nil {
				return http.StatusInternalServerError, err
			}
			if b := usedBuiltin(d.DataDir); b != nil {
				b.eng.SetModel(voiceModelOf(d.DataDir))
			}
			return http.StatusOK, nil
		case "builtin", "remote":
		default:
			return http.StatusBadRequest, errVoiceChange
		}
		if externalVoice() {
			return http.StatusConflict, errors.New("this machine recognizes with a service of its own (RUNTIME_VOICE_SOCKET)")
		}
		s := loadVoiceServer(d.DataDir)
		if c.Engine == "builtin" {
			s.Engine = "builtin"
			if err := saveVoiceServer(d.DataDir, s); err != nil {
				return http.StatusInternalServerError, err
			}
			useVoiceServer(d.DataDir, nil)
			return http.StatusOK, nil
		}
		next := engine.Remote{URL: strings.TrimSpace(c.URL), Model: strings.TrimSpace(c.Model)}
		if err := next.Check(); err != nil {
			return http.StatusBadRequest, err
		}
		switch {
		case c.Key != nil:
			next.Key = strings.TrimSpace(*c.Key)
			if err := next.Check(); err != nil {
				return http.StatusBadRequest, err
			}
		case s.Key == "":
		case engine.SameOrigin(s.URL, next.URL):
			next.Key = s.Key
		default:
			// The saved key is the old server's: it never goes to another address unless it is typed again.
			return http.StatusBadRequest, errors.New(msgNewOriginKey.String())
		}
		s = voiceServer{Engine: "remote", Remote: next}
		if err := saveVoiceServer(d.DataDir, s); err != nil {
			return http.StatusInternalServerError, err
		}
		useVoiceServer(d.DataDir, &next)
		return http.StatusOK, nil
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
				var body voiceChange
				raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
				if err == nil {
					err = json.Unmarshal(raw, &body)
				}
				if err != nil {
					peerJSON(w, http.StatusBadRequest, map[string]string{"error": errVoiceChange.Error()})
					return
				}
				if status, err := change(body); err != nil {
					peerJSON(w, status, map[string]string{"error": err.Error()})
					return
				}
				peerJSON(w, http.StatusOK, answer())
			default:
				w.Header().Set("Allow", "GET, PUT")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			}
		}),
	}}
}
