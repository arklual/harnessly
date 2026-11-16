package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/journal"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/voice/quickedit"
)

// The voice's quick edits (internal/voice/quickedit): a small change said out loud over an open board, slide
// or file is made by one short run of a fast model on that file only, in seconds, not by the chat's agent in a turn.
// Only the person's own page gets in.
//
//	POST <platform>/voice/edit        {file, selection?, ask, chat?} -> {said, changed, ms}
//	POST <platform>/voice/edit/undo   {file}                         -> {changed}
//	POST <platform>/voice/edit/warm   {}                             -> {ok}  (starts the fast model ahead)

// The fast way is one answer of a Claude Code started ahead (voice_edit_warm.go), applied here; then one of the ChatGPT
// plan's lightest model (RUNTIME_VOICE_EDIT_GPT names another); when neither applies, Claude Code with Read and Edit
// makes the edit.

// voiceEditModel is the fast model of the quick edits; RUNTIME_VOICE_EDIT_MODEL names another.
func voiceEditModel() string {
	if m := strings.TrimSpace(os.Getenv("RUNTIME_VOICE_EDIT_MODEL")); m != "" && agents.ModelIDRe.MatchString(m) {
		return m
	}
	return "haiku"
}

// voiceEditRunner runs Claude Code once on a copy of the file under a fixed name in an empty folder, allowed to read
// and edit that copy only (every other use of a tool is refused, not asked), with no hooks, MCP servers or session
// kept; the copy goes back over the file when it changed. No path of the person's goes into a permission rule.
func voiceEditRunner(d deps) quickedit.Runner {
	return func(ctx context.Context, file, prompt string) (string, error) {
		empty, err := os.MkdirTemp("", "voice-edit-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(empty)
		before, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		name := "target" + strings.ToLower(filepath.Ext(file))
		if strings.ContainsAny(name, "*?[]{}(),\\ \t") {
			name = "target.txt"
		}
		copyPath := filepath.Join(empty, name)
		if err := os.WriteFile(copyPath, before, 0o600); err != nil {
			return "", err
		}
		rt := sidechat.ReadRuntime(os.Getenv("RUNTIME_SIDE_CHAT_CONFIG"))
		cmd := exec.CommandContext(ctx, rt.Binary, "--print", "--model", voiceEditModel(), "--tools", "Read,Edit",
			"--allowedTools", "Read(./"+name+")", "Edit(./"+name+")", "--permission-mode", "dontAsk",
			"--strict-mcp-config", "--disable-slash-commands", "--settings", `{"disableAllHooks":true}`,
			"--no-session-persistence")
		cmd.Env = sidechat.Env(withAgentEnv(os.Environ(), agentEnvDir(d.DataDir), agents.Claude), rt)
		cmd.Stdin = strings.NewReader(strings.ReplaceAll(prompt, file, copyPath))
		cmd.Dir = empty
		out, runErr := cmd.Output()
		after, err := os.ReadFile(copyPath)
		if err == nil && !bytes.Equal(before, after) {
			info, statErr := os.Stat(file)
			if statErr != nil {
				return string(out), statErr
			}
			if err := os.WriteFile(file, after, info.Mode().Perm()); err != nil {
				return string(out), err
			}
		}
		return string(out), runErr
	}
}

// voiceEditHolder names another chat that holds a board of the design library; "" for anything else.
func voiceEditHolder(lib *design.Library) func(file, chat string) string {
	return func(file, chat string) string {
		root, err := filepath.EvalSymlinks(lib.Root())
		if err != nil || root == "" {
			return ""
		}
		rel, err := filepath.Rel(root, file)
		if err != nil || strings.HasPrefix(rel, "..") {
			return ""
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		name, ok := strings.CutSuffix(parts[len(parts)-1], ".dc.html")
		if len(parts) < 2 || !ok {
			return ""
		}
		project := parts[0]
		key, err := design.ResolveBoardKey(lib.ReadArtifact(project), name)
		if err != nil {
			key = name
		}
		for _, c := range lib.LiveClaims(project, time.Now().UnixMilli()) {
			if (c.Board == key || c.Board == name) && c.Owner.TabID != chat {
				if c.Owner.ChatTitle != "" {
					return "«" + c.Owner.ChatTitle + "»"
				}
				return c.Owner.User
			}
		}
		return ""
	}
}

var (
	voiceEditMu   sync.Mutex
	voiceEditFor  = map[string]*quickedit.Service{}
	voiceEditWarm = map[string]*warmClaude{}
)

// voiceEditWarmer is the Claude Code kept started ahead for the quick edits of this data dir.
func voiceEditWarmer(d deps) *warmClaude {
	voiceEditMu.Lock()
	defer voiceEditMu.Unlock()
	w := voiceEditWarm[d.DataDir]
	if w == nil {
		w = &warmClaude{d: d}
		voiceEditWarm[d.DataDir] = w
	}
	return w
}

func voiceEditService(d deps) *quickedit.Service {
	voiceEditMu.Lock()
	defer voiceEditMu.Unlock()
	if s := voiceEditFor[d.DataDir]; s != nil {
		return s
	}
	lib := designAPI(d).L
	roots := []string{d.Home}
	if r := lib.Root(); r != "" {
		roots = append(roots, r)
	}
	gpt := chatgptService(d)
	plan := func(ctx context.Context, prompt string) (string, error) {
		return gpt.Complete(ctx, strings.TrimSpace(os.Getenv("RUNTIME_VOICE_EDIT_GPT")), "", prompt)
	}
	warm := voiceEditWarm[d.DataDir]
	if warm == nil {
		warm = &warmClaude{d: d}
		voiceEditWarm[d.DataDir] = warm
	}
	s := quickedit.New(quickedit.Config{Roots: roots, Exclude: []string{d.DataDir},
		Plans: []quickedit.Planner{warm.Plan, plan}, PlanNames: []string{"claude", "chatgpt"}, Run: voiceEditRunner(d),
		Step:   voiceEditStep,
		Holder: voiceEditHolder(lib)})
	voiceEditFor[d.DataDir] = s
	return s
}

// voiceEditStep writes each way a quick edit tried into the journal, with its time: «реплика → правка» is read there.
func voiceEditStep(step string, took time.Duration, ok bool, note string) {
	level, text := journal.Info, "ok"
	if !ok {
		level, text = journal.Warn, "no edit"
	}
	if note != "" {
		text += ": " + clip(note, 160)
	}
	journal.Add(journal.Entry{At: time.Now().UnixMilli(), Level: level, Source: journal.SourceRuntime,
		Event: "voice.edit", Tool: step, Text: text, Dur: took.Milliseconds()})
}

func voiceEditRoutes(d deps) []gate.Route {
	var routes []gate.Route
	for _, base := range []string{platformAPI + "/voice/edit", "/agents" + platformAPI + "/voice/edit"} {
		routes = append(routes, gate.Route{
			Match: func(r *http.Request) bool {
				return r.URL.Path == base || r.URL.Path == base+"/undo" || r.URL.Path == base+"/warm"
			},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if sameorigin.Refuse(w, r) {
					return
				}
				if !d.SessionOK(r) {
					secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
					return
				}
				if r.Method != http.MethodPost {
					secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST"})
					return
				}
				// The page says a voice began over something it may edit: the fast model starts ahead.
				if strings.HasSuffix(r.URL.Path, "/warm") {
					voiceEditWarmer(d).Warm()
					secretJSON(w, http.StatusOK, map[string]bool{"ok": true})
					return
				}
				var req quickedit.Request
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
					secretJSON(w, http.StatusBadRequest, map[string]string{"error": quickedit.CodeBadRequest})
					return
				}
				s := voiceEditService(d)
				var res quickedit.Result
				var err error
				if strings.HasSuffix(r.URL.Path, "/undo") {
					res, err = s.Undo(req.File)
				} else {
					res, err = s.Edit(r.Context(), req)
				}
				if err != nil {
					status, body := quickedit.Describe(err)
					secretJSON(w, status, body)
					return
				}
				secretJSON(w, http.StatusOK, res)
			}),
		})
	}
	return routes
}
