//go:build !hubui

package web

import "io/fs"

// HubDist is nil without the hubui build tag: the hub then serves its page only from HUB_WEB_DIR.
func HubDist() fs.FS { return nil }
