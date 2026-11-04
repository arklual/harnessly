// The review fixture exposes an actual AgentNewTab without adding a test-only
// endpoint to the production runtime or depending on the MCP worker's branch.
package main

import (
	"context"
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
	defer func() {
		m.Close()
		raw, _ := os.ReadFile(filepath.Join(dir, "agent-browser", "profiles", agentbrowser.DefaultProfile, "host.pid"))
		if pid, e := strconv.Atoi(strings.TrimSpace(string(raw))); e == nil {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}()
	mux := http.NewServeMux()
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
