package host

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

func TestLongSocketAndStaleLock(t *testing.T) {
	dir := t.TempDir()
	p := SocketPath(filepath.Join(dir, strings.Repeat("long", 40)), "common")
	if len(p) >= 100 || p != SocketPath(filepath.Join(dir, strings.Repeat("long", 40)), "common") {
		t.Fatalf("unstable or long socket: %s", p)
	}
	lock, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), Config{ProfileDir: dir, Socket: filepath.Join(dir, "host.sock"), Chrome: "missing"})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("lock not respected: %v", err)
	}
}

func TestCappedLog(t *testing.T) {
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "host.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l := &cappedLog{f: f}
	b := make([]byte, LogLimit/2+1)
	for n := 0; n < 5; n++ {
		if _, err = l.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > LogLimit {
		t.Fatalf("unbounded log: %d", st.Size())
	}
}

func TestRelayReplacementKeepsMessageAndRequestBoundaries(t *testing.T) {
	chromeSide, hostSide := net.Pipe()
	defer chromeSide.Close()
	stream := cdp.NewStream(hostSide)
	defer stream.Close()
	relay := &relay{chrome: stream}
	go func() { _ = relay.read(t.Context()) }()
	first, firstHost := net.Pipe()
	defer first.Close()
	relay.replace(firstHost)
	peer := cdp.NewStream(chromeSide)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	send := func(conn net.Conn, raw string) {
		t.Helper()
		if err := cdp.NewStream(conn).Write(ctx, []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	send(first, `{"id":1,"method":"Browser.old"}`)
	old, err := peer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var oldMsg map[string]any
	if err = json.Unmarshal(old, &oldMsg); err != nil {
		t.Fatal(err)
	}
	// An interrupted client's unfinished record must never prefix its replacement's first command.
	if _, err = first.Write([]byte(`{"id":2,"method":`)); err != nil {
		t.Fatal(err)
	}
	second, secondHost := net.Pipe()
	defer second.Close()
	relay.replace(secondHost)
	send(second, `{"id":1,"method":"Browser.new"}`)
	fresh, err := peer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var newMsg map[string]any
	if err = json.Unmarshal(fresh, &newMsg); err != nil {
		t.Fatal(err)
	}
	if newMsg["method"] != "Browser.new" || newMsg["id"] == oldMsg["id"] {
		t.Fatalf("bad replacement: %s", fresh)
	}
	for _, msg := range []map[string]any{{"id": oldMsg["id"], "result": "old"}, {"id": newMsg["id"], "result": "new"}} {
		b, _ := json.Marshal(msg)
		if err = peer.Write(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	response, err := cdp.NewStream(second).Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(response, &result); err != nil {
		t.Fatal(err)
	}
	if result["id"] != float64(1) || result["result"] != "new" {
		t.Fatalf("late reply crossed clients: %s", response)
	}
}

func TestRelayBurstBackpressure(t *testing.T) {
	chromeSide, hostSide := net.Pipe()
	defer chromeSide.Close()
	stream := cdp.NewStream(hostSide)
	defer stream.Close()
	r := &relay{chrome: stream}
	go func() { _ = r.read(t.Context()) }()
	clientSide, relaySide := net.Pipe()
	defer clientSide.Close()
	r.replace(relaySide)
	peer := cdp.NewStream(chromeSide)
	viewer := cdp.NewStream(clientSide)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	sent := make(chan error, 1)
	go func() {
		for n := 0; n < 3000; n++ {
			raw, _ := json.Marshal(map[string]any{"method": "Network.event", "params": map[string]int{"n": n}})
			if err := peer.Write(ctx, raw); err != nil {
				sent <- err
				return
			}
		}
		sent <- nil
	}()
	// Fill the small queue before consuming, then keep a deliberately slower reader.
	time.Sleep(150 * time.Millisecond)
	for n := 0; n < 3000; n++ {
		raw, err := viewer.Read(ctx)
		if err != nil {
			t.Fatalf("event %d: %v", n, err)
		}
		var event struct {
			Params struct {
				N int `json:"n"`
			} `json:"params"`
		}
		if err = json.Unmarshal(raw, &event); err != nil || event.Params.N != n {
			t.Fatalf("event %d: %s (%v)", n, raw, err)
		}
		if n%50 == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	t.Log("3000 ordered events delivered to a slow client without disconnect")
}

func TestRelayStalledClientDeadlineAndReplacement(t *testing.T) {
	chromeSide, hostSide := net.Pipe()
	defer chromeSide.Close()
	stream := cdp.NewStream(hostSide)
	defer stream.Close()
	r := &relay{chrome: stream}
	go func() { _ = r.read(t.Context()) }()
	first, firstHost := net.Pipe()
	defer first.Close()
	r.replace(firstHost)
	r.mu.Lock()
	old := r.current
	r.mu.Unlock()
	peer := cdp.NewStream(chromeSide)
	started := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	sent := make(chan error, 1)
	go func() {
		for n := 0; n < 100; n++ {
			if err := peer.Write(ctx, []byte(`{"method":"Network.event"}`)); err != nil {
				sent <- err
				return
			}
		}
		sent <- nil
	}()
	select {
	case <-old.done:
	case <-ctx.Done():
		t.Fatal("stalled client was not closed")
	}
	elapsed := time.Since(started)
	if elapsed < 4*time.Second || elapsed > 7*time.Second {
		t.Fatalf("write deadline: %s", elapsed)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	second, secondHost := net.Pipe()
	defer second.Close()
	r.replace(secondHost)
	go func() { sent <- peer.Write(ctx, []byte(`{"method":"Browser.replacement"}`)) }()
	reader := cdp.NewStream(second)
	for {
		raw, err := reader.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "replacement") {
			break
		}
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	t.Logf("stalled client closed after %s; replacement received its event", elapsed.Round(time.Millisecond))
}
