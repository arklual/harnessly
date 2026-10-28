package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/voice"
	"github.com/ly-solutions/harnessly/internal/voice/engine"
)

// A machine that names no recognition service (RUNTIME_VOICE_SOCKET, or one already listening at the usual place) gets
// the engine of internal/voice/engine: a pinned whisper.cpp server the runtime fetches on the first dictation and runs
// as its child, behind a Unix socket of its own in the data folder. The page's microphone is announced as soon as
// ffmpeg is there; the first press shows that the model is being downloaded.
//
//	RUNTIME_VOICE_ENGINE=off          never use the built-in engine
//	RUNTIME_VOICE_MODEL=tiny|base|small   (default: small from 4 processors up, base below)
//	RUNTIME_VOICE_MODEL_FILE=…        a ggml model of your own (no download, no checksum)
//	RUNTIME_VOICE_WHISPER_BIN=…       a whisper-server of your own
//	RUNTIME_VOICE_THREADS=N           processors the recognition may use (default: up to 4)
//	RUNTIME_VOICE_DOWNLOAD=off        never touch the network: only files that are already there
//	RUNTIME_VOICE_PREFETCH=1          get the model at start instead of at the first dictation

const defaultVoiceSocket = "/run/harnessly-voice/asr.sock"

var (
	voiceEnginesMu sync.Mutex
	voiceEngines   = map[string]*voiceBuiltin{}
)

// voiceBuiltin is the engine of one data folder and the socket it listens on ("" when it cannot be used here).
type voiceBuiltin struct {
	eng    *engine.Engine
	socket string
	ln     net.Listener
}

func isSocket(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

// voiceSocket is the recognition service's socket this runtime's dictation uses.
func voiceSocket(dataDir string) string {
	if s := os.Getenv("RUNTIME_VOICE_SOCKET"); s != "" {
		return s
	}
	if env("RUNTIME_VOICE_ENGINE", "") == "off" || isSocket(defaultVoiceSocket) {
		return defaultVoiceSocket
	}
	if b := builtinVoice(dataDir); b.socket != "" {
		return b.socket
	}
	return defaultVoiceSocket
}

func builtinVoice(dataDir string) *voiceBuiltin {
	voiceEnginesMu.Lock()
	defer voiceEnginesMu.Unlock()
	if b, ok := voiceEngines[dataDir]; ok {
		return b
	}
	b := &voiceBuiltin{}
	voiceEngines[dataDir] = b
	threads, _ := strconv.Atoi(os.Getenv("RUNTIME_VOICE_THREADS"))
	b.eng = engine.New(engine.Config{
		Dir: filepath.Join(dataDir, "voice"), Bin: os.Getenv("RUNTIME_VOICE_WHISPER_BIN"), ModelFile: os.Getenv("RUNTIME_VOICE_MODEL_FILE"),
		Model: os.Getenv("RUNTIME_VOICE_MODEL"), Threads: threads, NoDownload: os.Getenv("RUNTIME_VOICE_DOWNLOAD") == "off", Log: voiceLog,
	})
	if err := b.eng.Eligible(); err != nil {
		voiceLog("info", "dictation is off: "+err.Error())
		return b
	}
	sock := filepath.Join(dataDir, "voice", "asr.sock")
	if len(sock) > 100 { // a Unix socket's path is short
		sock = filepath.Join(os.TempDir(), fmt.Sprintf("harnessly-voice-%d", os.Getuid()), "asr.sock")
	}
	ln, err := b.eng.Listen(sock)
	if err != nil {
		voiceLog("warn", "dictation is off: the socket "+sock+" cannot be opened: "+err.Error())
		return b
	}
	b.ln, b.socket = ln, sock
	if os.Getenv("RUNTIME_VOICE_PREFETCH") == "1" {
		b.eng.Prepare()
	}
	return b
}

var noteDecoderOnce sync.Once

// noteVoiceDecoder says once why the microphone is not offered when the only thing missing is ffmpeg.
func noteVoiceDecoder(s *voice.Server) {
	if s.HasDecoder() {
		return
	}
	noteDecoderOnce.Do(func() {
		voiceLog("info", locale.Pick("dictation is off: ffmpeg was not found (install it: apt install ffmpeg / brew install ffmpeg)",
			"голосовой ввод выключен: не найден ffmpeg (установите: apt install ffmpeg / brew install ffmpeg)"))
	})
}

// closeVoiceEngines stops the recognition servers of the built-in engines (the runtime is going away).
func closeVoiceEngines() {
	voiceEnginesMu.Lock()
	defer voiceEnginesMu.Unlock()
	for _, b := range voiceEngines {
		if b.ln != nil {
			_ = b.ln.Close()
		}
		if b.eng != nil {
			b.eng.Close()
		}
	}
}
