package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/wire"
	"github.com/ly-solutions/harnessly/internal/wire/wirefake"
)

// v2FakeFeatures are what the fake runtime announces: what it plays.
var v2FakeFeatures = []wire.Feature{wire.FeatureQueue, wire.FeatureSteer, wire.FeatureQuestions, wire.FeaturePermissions, wire.FeatureBackground, wire.FeatureModel, wire.FeatureTerminal, wire.FeatureVoice, wire.FeatureDesignly, wire.FeatureMachine, wire.FeatureApps, wire.FeatureAppsHub, wire.FeatureSettings, wire.FeatureQueueMove, wire.FeatureTerminalStop, wire.FeatureFork, wire.FeatureHandoff, wire.FeatureAutoContinue}

// v2FakeTranscript is what the fake runtime hears in any recording.
const v2FakeTranscript = "Проверь, что очередь не теряет сообщения при перезапуске"

// v2Fake is the fake v2 of RUNTIME_V2_FAKE=1: the shared fixtures and a scripted agent (internal/wire/wirefake), paced
// by RUNTIME_V2_FAKE_STEP_MS, steered on /api/v2/fake/*. nil when the variable is not set.
func v2Fake(d deps) (*wire.Server, func(http.Handler) http.Handler) {
	if os.Getenv("RUNTIME_V2_FAKE") != "1" {
		return nil, nil
	}
	step, _ := strconv.Atoi(os.Getenv("RUNTIME_V2_FAKE_STEP_MS"))
	version := env("AGENT_VERSION", agent.BuildVersion)
	machine, err := wirefake.NewMachine(v2Machine, version, v2FakeFeatures)
	if err != nil {
		agent.Log("error", "v2 fake: no machine", agent.F{K: "error", V: err.Error()})
		return nil, nil
	}
	hub := wire.NewHub(machine)
	srv := v2Server(d, hub, nil, machine, v2FakeFeatures, false)
	svc, err := wirefake.New(wirefake.Options{Hub: hub, Machine: v2Machine, Uploads: srv.Uploads, Limits: srv.Limits,
		Step: time.Duration(step) * time.Millisecond})
	if err != nil {
		agent.Log("error", "v2 fake: fixtures unreadable", agent.F{K: "error", V: err.Error()})
		return nil, nil
	}
	srv.Service = svc
	srv.Terminals = svc
	srv.MachineTool = machine.Tool()
	srv.Apps = machine.Apps()
	open := v2FakeOpen()
	srv.Open = open.Open
	srv.OpenAll = open.OpenAll
	srv.AgentSettings = machine.AgentSettings()
	srv.RuntimeSettings = &wire.FileRuntimeSettings{Path: filepath.Join(d.DataDir, "v2", "runtime-settings.json")}
	srv.Designly = svc.Designly()
	srv.SideChat = svc.SideChat
	srv.HandoffDraft = svc.HandoffDraft
	srv.Files = svc.Files()
	srv.Tasks = svc.Tasks
	srv.Voice = func(_ context.Context, audio io.Reader, _ string) (string, error) {
		_, err := io.Copy(io.Discard, audio)
		return v2FakeTranscript, err
	}
	srv.Sessions = func(context.Context) ([]wire.AgentSession, error) { return svc.Sessions(), nil }
	control := svc.Control(machine)
	agent.Log("warn", "v2 fake: /api/v2 serves the fixtures and a scripted agent", agent.F{K: "stepMs", V: step})
	return srv, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p := strings.TrimPrefix(r.URL.Path, "/agents"); strings.HasPrefix(p, wirefake.ControlPrefix) {
				if p == wirefake.ControlPrefix+"reset" {
					srv.Ops.Clear()
				}
				control.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// v2FakeOpen is the fake's opens: a chat in a folder of two checkouts, harnessly and model-gateway, on the GitLab of the
// deployment (gitlab.example.com when it names none), where harnessly has the merge request !80 and model-gateway !104.
func v2FakeOpen() v2Open {
	targets := panelTargets()
	if len(targets.GitLabHosts) == 0 {
		targets.GitLabHosts = []string{"gitlab.example.com"}
	}
	host := targets.GitLabHosts[0]
	merge := map[string]string{"group/harnessly!80": "Open the merge requests of a message", "group/model-gateway!104": "Queue the models"}
	return v2Open{
		record:  func(string) (wire.Conversation, bool) { return wire.Conversation{Cwd: "/fake/workspace"}, true },
		targets: targets,
		remotes: func(_ context.Context, dir string) []string {
			if name := path.Base(dir); dir != "/fake/workspace" {
				return []string{"git@" + host + ":group/" + name + ".git"}
			}
			return nil
		},
		repos: func(string) []string { return []string{"harnessly", "model-gateway"} },
		mr: func(_ context.Context, h, project string, iid int) (mrInfo, bool) {
			title, ok := merge[project+"!"+strconv.Itoa(iid)]
			return mrInfo{url: "https://" + h + "/" + project + "/-/merge_requests/" + strconv.Itoa(iid), title: title}, ok && h == host
		},
	}
}
