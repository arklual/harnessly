//go:build webui

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist is the built page (web/dist), compiled in with the webui build tag.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}
