//go:build darwin && !cgo

package main

const hasTray = false

// runTray without a tray (a macOS build without cgo): the window alone, and the link in the background.
func runTray(a *app) {
	w, err := startWindow(a)
	if err != nil {
		a.log.Fatalf("окно: %v", err)
	}
	if _, err := load(); err == nil {
		_ = a.start()
	}
	openURL(w.url)
	select {}
}
