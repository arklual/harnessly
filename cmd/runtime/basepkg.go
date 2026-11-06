package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ly-solutions/harnessly/internal/basepkg"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The base package (docs/hub/machine.md, «Base package»). The system part is installed by the root service
// harnessly-basepkg where the installer put one (HARNESSLY_BASEPKG_STATE names its folder); the runtime only reads its
// state and writes the inbox: the person's «Пропустить», the proxy, and «Повторить». The personal part — the speech
// model — is the runtime's own: it is fetched in the background, without waiting for the first dictation.

func basepkgInbox(dataDir string) string { return filepath.Join(dataDir, "basepkg") }

func basepkgView(dataDir string) basepkg.View {
	stateDir := os.Getenv("HARNESSLY_BASEPKG_STATE")
	// The installer put the service here when it set the folder; before its first run there is no state yet.
	has := stateDir != ""
	st := basepkg.State{}
	if has {
		st, _ = basepkg.ReadState(stateDir)
	}
	return basepkg.MakeView(basepkg.Look{
		LookPath:   exec.LookPath,
		Manager:    basepkg.Detect(exec.LookPath, runtime.GOOS),
		State:      st,
		HasService: has,
		Inbox:      basepkg.ReadInbox(basepkgInbox(dataDir)),
		Voice:      voiceOf(dataDir),
	})
}

func voiceOf(dataDir string) basepkg.Voice {
	if externalVoice() {
		return basepkg.Voice{Off: true}
	}
	eng := builtinVoice(dataDir).eng
	if eng.Eligible() != nil || eng.UsesRemote() {
		return basepkg.Voice{Off: true}
	}
	s := eng.Status()
	return basepkg.Voice{Ready: s.State == "ready", Preparing: s.State == "preparing", Error: s.Error}
}

// prefetchVoice gets the speech model ready when nothing stops it: not skipped, room on the disk, the machine not busy.
func prefetchVoice(dataDir string) {
	if os.Getenv("RUNTIME_VOICE_DOWNLOAD") == "off" || basepkg.ReadInbox(basepkgInbox(dataDir)).Skip[basepkg.VoiceID] {
		return
	}
	v := voiceOf(dataDir)
	if v.Off || v.Ready || v.Preparing {
		return
	}
	it, _ := basepkg.ByID(basepkg.VoiceID)
	if free, ok := freeBytes(dataDir); !ok || free < uint64(it.SizeMB)<<20+uint64(2)<<30 {
		return
	}
	if loadBusy() {
		return
	}
	builtinVoice(dataDir).eng.Prepare()
}

// watchBasepkg looks after the personal part: a few minutes after start, then every half an hour.
func watchBasepkg(ctx context.Context, dataDir string) {
	t := time.NewTimer(3 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			prefetchVoice(dataDir)
			t.Reset(30 * time.Minute)
		}
	}
}

func loadBusy() bool {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return false
	}
	var l float64
	if _, err := fmt.Sscan(string(b), &l); err != nil {
		return false
	}
	return l > float64(runtime.NumCPU()*2)
}

// askBasepkg asks for the base package now: the root service where there is one, and the speech model.
func askBasepkg(dataDir string) error {
	go prefetchVoice(dataDir)
	if os.Getenv("HARNESSLY_BASEPKG_STATE") == "" {
		return nil
	}
	return basepkg.Ask(basepkgInbox(dataDir), time.Now())
}

func basepkgRoutes(d deps) []gate.Route {
	base := platformAPI + "/basepkg"
	ok := func(w http.ResponseWriter, r *http.Request, method string) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		if r.Method != method {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	inbox := basepkgInbox(d.DataDir)
	view := func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r, http.MethodGet) {
			secretJSON(w, http.StatusOK, basepkgView(d.DataDir))
		}
	}
	ask := func() error { return askBasepkg(d.DataDir) }
	run := func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r, http.MethodPost) {
			return
		}
		if err := ask(); err != nil {
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		secretJSON(w, http.StatusOK, basepkgView(d.DataDir))
	}
	skip := func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r, http.MethodPost) {
			return
		}
		var body struct {
			ID   string `json:"id"`
			Skip bool   `json:"skip"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := basepkg.WriteSkip(inbox, body.ID, body.Skip); err != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown-item"})
			return
		}
		if !body.Skip {
			_ = ask()
		}
		secretJSON(w, http.StatusOK, basepkgView(d.DataDir))
	}
	proxy := func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r, http.MethodPost) {
			return
		}
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := basepkg.WriteProxy(inbox, body.URL); err != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-proxy", "message": err.Error()})
			return
		}
		_ = ask()
		secretJSON(w, http.StatusOK, basepkgView(d.DataDir))
	}
	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(view)},
		{Match: func(r *http.Request) bool { return pathIs(r, base+"/run") }, Handler: http.HandlerFunc(run)},
		{Match: func(r *http.Request) bool { return pathIs(r, base+"/skip") }, Handler: http.HandlerFunc(skip)},
		{Match: func(r *http.Request) bool { return pathIs(r, base+"/proxy") }, Handler: http.HandlerFunc(proxy)},
	}
}
