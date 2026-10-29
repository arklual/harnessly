package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/voice"
)

// The voice slice ("voice"): dictation over /api/voice (ffmpeg to the machine's recognition service) and the in-tab
// engine's model files, /api/voice/assets/…. The assets are not shadowed: a file not yet cached is downloaded.

func voiceLog(level, msg string, kv ...any) {
	fs := make([]agent.F, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		fs = append(fs, agent.F{K: k, V: kv[i+1]})
	}
	agent.Log(level, "voice: "+msg, fs...)
}

// voiceAssetSegments are the segments after /api/voice/assets/, false for another path or method.
func voiceAssetSegments(r *http.Request) ([]string, bool) {
	if r.Method != http.MethodGet {
		return nil, false
	}
	parts, _, ok := files.APIParts(r)
	if !ok || len(parts) < 4 || parts[1] != "voice" || parts[2] != "assets" {
		return nil, false
	}
	return parts[3:], true
}

// newVoice is the machine's dictation: ffmpeg and the recognition service it is set up with, or, when it names none, the
// engine the runtime brings itself (voice_engine.go).
func newVoice(dataDir string) *voice.Server {
	srv := voice.New(voice.Config{Socket: voiceSocket(dataDir), FFmpeg: os.Getenv("RUNTIME_VOICE_FFMPEG_BIN"), Lang: os.Getenv("RUNTIME_VOICE_LANG"), Log: voiceLog})
	noteVoiceDecoder(srv)
	return srv
}

func voiceRoutes(d deps) []gate.Route {
	dictation := newVoice(d.DataDir)
	assets := &voice.Assets{CacheDir: filepath.Join(d.DataDir, "voice-assets"), ModelHost: "https://huggingface.co",
		RuntimeHost: "https://cdn.jsdelivr.net", Log: voiceLog}
	return append([]gate.Route{
		{
			Slice: "voice",
			Match: func(r *http.Request) bool { return pathIs(r, "/api/voice") && isUpgrade(r) },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !d.SessionOK(r) {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				dictation.ServeHTTP(w, r)
			}),
		},
		{
			Slice: "voice",
			Match: func(r *http.Request) bool { _, ok := voiceAssetSegments(r); return ok },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, _, _, refused := d.APIRefusal(r); refused {
					files.WriteUnauthorized(w)
					return
				}
				segments, _ := voiceAssetSegments(r)
				assets.Serve(w, r, segments)
			}),
		},
	}, voiceEngineRoutes(d, dictation)...)
}
