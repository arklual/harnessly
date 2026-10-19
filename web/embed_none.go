//go:build !webui

// Package web holds the browser front-end (a Vite project in this folder) and, built with the webui tag, its compiled
// files for the runtime to serve.
package web

import "io/fs"

// Dist is nil without the webui build tag: the runtime then serves the page only from RUNTIME_WEB_DIR.
func Dist() fs.FS { return nil }
