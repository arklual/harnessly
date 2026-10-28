//go:build hubui

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist-hub
var hubDist embed.FS

// HubDist is the built hub page (web/dist-hub: index.html and assets/), compiled in with the hubui build tag.
func HubDist() fs.FS {
	sub, err := fs.Sub(hubDist, "dist-hub")
	if err != nil {
		return nil
	}
	return sub
}
