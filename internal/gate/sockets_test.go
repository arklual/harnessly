package gate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestSwitchingASliceOnClosesTheSocketsItTakesOver(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	}))
	defer old.Close()
	f, dir := newGate(t, strings.TrimPrefix(old.URL, "http://"))
	f.HandOverSockets("core", "/api/status")
	switched := make(chan bool, 2)
	f.OnSliceChange("core", func(on bool) { switched <- on })
	srv := httptest.NewUnstartedServer(f)
	srv.Config.ConnContext = ConnContext
	srv.Start()
	defer srv.Close()
	f.checkSlices() // the first look only learns the state

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	status, _, err := websocket.Dial(ctx, ws+"/agents/api/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer status.CloseNow()
	terminal, _, err := websocket.Dial(ctx, ws+"/api/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.CloseNow()
	for f.openCount("core") != 1 {
		time.Sleep(10 * time.Millisecond)
	}

	if err := os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["core"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.slices.at = time.Time{}
	f.checkSlices()
	if on := <-switched; !on {
		t.Fatal("hook saw the slice off")
	}
	if _, _, err := status.Read(ctx); err == nil {
		t.Fatal("the status socket stayed open")
	}
	if err := terminal.Write(ctx, websocket.MessageText, []byte("x")); err != nil {
		t.Fatalf("a socket the slice does not take was closed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.slices.at = time.Time{}
	f.checkSlices()
	if on := <-switched; on {
		t.Fatal("hook saw the slice on")
	}
}

func (f *Gate) openCount(slice string) int {
	f.handover.mu.Lock()
	defer f.handover.mu.Unlock()
	return len(f.handover.open[slice])
}
