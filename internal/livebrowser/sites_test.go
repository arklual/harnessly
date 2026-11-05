package livebrowser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealSites opens outside sites in the real browser and writes what it sees: a check by eye, run by hand with
// LIVEBROWSER_SITES=<dir> (the pictures go there). Skipped otherwise: it needs the network and Chrome.
func TestRealSites(t *testing.T) {
	dir := os.Getenv("LIVEBROWSER_SITES")
	if dir == "" {
		t.Skip("LIVEBROWSER_SITES is not set")
	}
	s := chromeOrSkip(t)
	// The real rule and resolver: these sites are outside.
	s.resolve = s.resolveHost
	for _, site := range []string{
		"https://www.google.com/",
		"https://duckduckgo.com/",
		"https://html.duckduckgo.com/html/?q=harnessly",
		"https://example.com/",
		"https://ru.wikipedia.org/wiki/Браузер",
	} {
		tab, err := s.Open(t.Context(), site, 1280, 800, 1)
		if err != nil {
			t.Fatalf("%s: %v", site, err)
		}
		if err := tab.StartScreencast(); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if err := tab.Navigate(site); err != nil {
			t.Errorf("%s: navigate: %v", site, err)
			tab.Close()
			continue
		}
		var info Info
		for time.Since(start) < 20*time.Second {
			info, _ = tab.Info()
			if info.Title != "" && !info.Loading {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		// The last picture after the page settled.
		var frame Frame
		settle := time.After(2 * time.Second)
	wait:
		for {
			select {
			case frame = <-tab.Frames():
			case <-settle:
				break wait
			}
		}
		name := strings.NewReplacer("https://", "", "/", "_", "?", "_", "=", "_").Replace(site)
		if len(frame.Data) > 0 {
			_ = os.WriteFile(filepath.Join(dir, name+".jpg"), frame.Data, 0o600)
		}
		t.Logf("%s → %q at %s in %s, frame %d bytes %dx%d", site, info.Title, info.URL, time.Since(start).Round(100*time.Millisecond), len(frame.Data), frame.Width, frame.Height)
		tab.Close()
	}
}
