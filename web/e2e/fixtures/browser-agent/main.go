// The review fixture exposes an actual AgentNewTab without adding a test-only
// endpoint to the production runtime or depending on the MCP worker's branch.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	browsertools "github.com/ly-solutions/harnessly/internal/agentbrowser/tools"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "browser-host" {
		if err := host.Main(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	dir, err := os.MkdirTemp("", "browser-review-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	chrome, err := agentbrowser.FindChrome()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m := agentbrowser.New(agentbrowser.Config{DataDir: dir, Executable: exe, Chrome: chrome, Mode: "headless"})
	access := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(dir, ""), func(conv string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{Title: "Manual login task"}, conv != ""
	})
	access.Manager = m
	access.Wait = time.Millisecond
	defer func() {
		m.Close()
		raw, _ := os.ReadFile(filepath.Join(dir, "agent-browser", "profiles", agentbrowser.DefaultProfile, "host.pid"))
		if pid, e := strconv.Atoi(strings.TrimSpace(string(raw))); e == nil {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}()
	browserTools := &browsertools.Service{Manager: m}
	defer browserTools.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		answer, err := access.RequestLogin(r.Context(), r.URL.Query().Get("conv"), r.URL.Query().Get("url"), r.URL.Query().Get("why"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		_ = json.NewEncoder(w).Encode(answer)
	})
	mux.HandleFunc("/api/agent-browser/logins", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"asks": access.Logins.Pending()})
			return
		}
		var body struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := access.AnswerLogin(r.Context(), body.ID, body.Action); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		conv := r.URL.Query().Get("conv")
		i, e := m.InstanceFor(r.Context(), conv)
		if e == nil {
			_, e = i.AgentNewTab(r.Context(), conv, r.URL.Query().Get("url"))
		}
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("/evaluate", func(w http.ResponseWriter, r *http.Request) {
		conv := r.URL.Query().Get("conv")
		i, err := m.InstanceFor(r.Context(), conv)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		tabs := i.AgentTabs(conv)
		if len(tabs) == 0 {
			http.Error(w, "No agent tab", 400)
			return
		}
		set := browsertools.Tools{S: browserTools, Caller: conv}
		result := set.Call(r.Context(), "browser_evaluate", jsvalue.NewObject("tab", tabs[0].ID, "expression", "document.title"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jsvalue.Stringify(result, ""))
	})
	mux.HandleFunc("/api/agent-browser", func(w http.ResponseWriter, r *http.Request) {
		// Playwright forwards this socket from the temporary runtime's origin. This
		// unauthenticated fixture listens exclusively on loopback and is never shipped.
		r.Header.Del("Origin")
		m.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	fmt.Println("http://" + listener.Addr().String())
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = server.Close() }()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
