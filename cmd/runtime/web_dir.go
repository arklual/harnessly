package main

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/webrelease"
	"github.com/ly-solutions/harnessly/web"
)

// RUNTIME_WEB_DIR is a page built apart from this runtime. When it carries its contract (web-release.json,
// tools/web-compat.mjs), it is served only if this runtime answers every call it makes: the routes are those of the page
// compiled into this runtime, which came from the same tree. A page that calls a route this runtime does not have
// (a page newer than the runtime) gives way to the compiled-in one, and the log names the missing calls. A directory
// without the contract (a developer's `pnpm build`) is served as it always was.

var webDirChecks sync.Map // RUNTIME_WEB_DIR -> bool (served)

// servedWebDir is RUNTIME_WEB_DIR when this runtime serves the page there, "" when it serves the compiled-in page.
func servedWebDir() string {
	dir := os.Getenv("RUNTIME_WEB_DIR")
	if dir == "" {
		return ""
	}
	if ok, done := webDirChecks.Load(dir); done {
		if ok.(bool) {
			return dir
		}
		return ""
	}
	ok := webDirServed(os.DirFS(dir), web.Dist(), dir)
	webDirChecks.Store(dir, ok)
	if ok {
		return dir
	}
	return ""
}

// webDirServed decides for one directory against the compiled-in page (nil without the webui tag).
func webDirServed(dir, embedded fs.FS, name string) bool {
	page, err := webrelease.Read(dir)
	if errors.Is(err, webrelease.ErrNone) {
		return true
	}
	if embedded == nil {
		// Nothing else to serve: the directory is the only page this runtime has.
		return true
	}
	if err != nil {
		agent.Log("warn", "web: RUNTIME_WEB_DIR has an unreadable contract; the compiled-in page is served",
			agent.F{K: "dir", V: name}, agent.F{K: "err", V: err.Error()})
		return false
	}
	own, err := webrelease.Read(embedded)
	if err != nil {
		agent.Log("warn", "web: this runtime's page carries no contract to check RUNTIME_WEB_DIR against; the compiled-in page is served",
			agent.F{K: "dir", V: name}, agent.F{K: "page", V: page.Version}, agent.F{K: "err", V: err.Error()})
		return false
	}
	if missing := webrelease.Missing(page, own); len(missing) > 0 {
		shown := missing
		if len(shown) > 5 {
			shown = shown[:5]
		}
		agent.Log("warn", "web: the page in RUNTIME_WEB_DIR calls routes this runtime does not have; the compiled-in page is served",
			agent.F{K: "dir", V: name}, agent.F{K: "page", V: page.Version}, agent.F{K: "runtime_page", V: own.Version},
			agent.F{K: "missing", V: len(missing)}, agent.F{K: "first", V: strings.Join(shown, " ")})
		return false
	}
	agent.Log("info", "web: serving the page of RUNTIME_WEB_DIR",
		agent.F{K: "dir", V: name}, agent.F{K: "page", V: page.Version}, agent.F{K: "calls", V: len(page.Calls)})
	return true
}

// webFiles are the built front-end the runtime serves: RUNTIME_WEB_DIR when it is served (servedWebDir), else the files
// compiled in (nil without either).
func webFiles() fs.FS {
	if dir := servedWebDir(); dir != "" {
		return os.DirFS(dir)
	}
	return web.Dist()
}
