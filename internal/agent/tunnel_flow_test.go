package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	hublink "github.com/ly-solutions/harnessly/internal/hub/link"
)

// byteSource is a loopback server that sends total bytes to whoever connects and closes. It returns its port and the
// hash of what it sends.
func byteSource(t *testing.T, total int) (port int, sum []byte) {
	t.Helper()
	block := make([]byte, 1<<20)
	_, _ = rand.Read(block)
	h := sha256.New()
	for left := total; left > 0; left -= min(left, len(block)) {
		h.Write(block[:min(left, len(block))])
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				for left := total; left > 0; {
					n, err := c.Write(block[:min(left, len(block))])
					if err != nil {
						return
					}
					left -= n
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, h.Sum(nil)
}

// bareLink is a link nobody writes out: what the tunnels send waits in its queue for the test.
func bareLink() *link {
	return &link{control: make(chan []byte, 16), data: make(chan []byte, 8192), done: make(chan struct{})}
}

// sent reads what a tunnel put on the link until it has been quiet for a moment, and says how many bytes of data
// that was and whether the tunnel was closed after them.
func sent(l *link, id string) (n int, closed bool) {
	for {
		select {
		case f := <-l.data:
			kind, fid, payload, ok := DecodeFrame(f)
			if !ok || fid != id {
				continue
			}
			if kind == FrameClose {
				return n, true
			}
			n += len(payload)
		case <-time.After(300 * time.Millisecond):
			return n, false
		}
	}
}

func windowFrame(id string, n uint32) []byte {
	return EncodeFrame(FrameWindow, id, binary.BigEndian.AppendUint32(nil, n))
}

func TestTunnelSendsNoMoreThanItsWindow(t *testing.T) {
	port, _ := byteSource(t, 1<<20)
	tun := newTunnels(port, 0)
	l := bareLink()
	id := strings.Repeat("ab", 16)
	spec := TunnelOpen{TunnelID: id, Target: "runtime", Window: 100_000}
	if got := tunnelOpened(spec); !got["opened"] || !got["flow"] {
		t.Fatalf("the answer to an opening with a window: %v", got)
	}
	start, err := tun.Open(l, spec)
	if err != nil {
		t.Fatal(err)
	}
	start()
	if n, closed := sent(l, id); n != 100_000 || closed {
		t.Fatalf("sent %d (closed %v) on a window of 100000", n, closed)
	}
	// Each grant lets exactly that much more through; a frame that is not a grant of four bytes lets nothing.
	tun.Frame(windowFrame(id, 50_000))
	tun.Frame(EncodeFrame(FrameWindow, id, []byte{1}))
	tun.Frame(windowFrame(strings.Repeat("cd", 16), 50_000))
	if n, closed := sent(l, id); n != 50_000 || closed {
		t.Fatalf("sent %d (closed %v) on a grant of 50000", n, closed)
	}
	// With the rest granted the socket's end reaches the other side.
	tun.Frame(windowFrame(id, 1<<20))
	if n, closed := sent(l, id); n != 1<<20-150_000 || !closed {
		t.Fatalf("sent %d (closed %v) of the rest", n, closed)
	}
}

func TestTunnelWithoutWindowSendsAsBefore(t *testing.T) {
	// What a hub that knows no window opens: nothing is counted, nothing waits for a grant.
	port, _ := byteSource(t, 1<<20)
	tun := newTunnels(port, 0)
	l := bareLink()
	id := strings.Repeat("ab", 16)
	var spec TunnelOpen
	if err := json.Unmarshal([]byte(`{"tunnelId":"`+id+`","target":"runtime"}`), &spec); err != nil {
		t.Fatal(err)
	}
	if got := tunnelOpened(spec); len(got) != 1 || !got["opened"] {
		t.Fatalf("the answer to an opening without a window: %v", got)
	}
	start, err := tun.Open(l, spec)
	if err != nil {
		t.Fatal(err)
	}
	start()
	if n, closed := sent(l, id); n != 1<<20 || !closed {
		t.Fatalf("sent %d (closed %v)", n, closed)
	}
}

// hubOver joins the agent's tunnels to a hub's end of a link over a real WebSocket. old: the agent's side behaves as
// one that has never heard of a window.
func hubOver(t *testing.T, tun *tunnels, old bool) *hublink.Link {
	t.Helper()
	hubSide := make(chan *hublink.Link, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		hl := hublink.New(conn, "abcdefghijklmnopqrst", nil)
		hubSide <- hl
		_ = hl.Serve(context.Background(), nil)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		t.Fatal(err)
	}
	conn.SetReadLimit(MaxPayload)
	l := newLink(conn)
	t.Cleanup(func() {
		l.close(websocket.StatusNormalClosure, "")
		tun.DropLink(l)
		conn.CloseNow()
	})
	go func() {
		for {
			typ, data, err := conn.Read(context.Background())
			if err != nil {
				l.close(websocket.StatusNormalClosure, "")
				return
			}
			if typ == websocket.MessageBinary {
				if old && len(data) > 0 && data[0] == FrameWindow {
					t.Error("a window frame went to an agent that knows none")
				}
				tun.Frame(data)
				continue
			}
			var req map[string]json.RawMessage
			if json.Unmarshal(data, &req) != nil {
				continue
			}
			spec, _ := field[TunnelOpen](req, "spec")
			var result any = map[string]bool{"opened": true}
			if old {
				spec.Window = 0
			} else {
				result = tunnelOpened(spec)
			}
			answer := map[string]any{"id": req["id"], "ok": true, "result": result}
			start, err := tun.Open(l, spec)
			if err != nil {
				answer = map[string]any{"id": req["id"], "ok": false, "error": err.Error()}
			}
			b, _ := json.Marshal(answer)
			l.sendText(b)
			if start != nil {
				start()
			}
		}
	}()
	select {
	case hl := <-hubSide:
		t.Cleanup(func() { hl.Close(websocket.StatusNormalClosure, "") })
		return hl
	case <-time.After(10 * time.Second):
		t.Fatal("the hub's side never came up")
		return nil
	}
}

// readWhole reads the tunnel to its end, pausing every few reads when slow, and returns the hash of what came, how
// much it was and the most the hub's tunnels held meanwhile.
func readWhole(t *testing.T, tn net.Conn, slow bool) (sum []byte, total int, peak int64) {
	t.Helper()
	_ = tn.SetReadDeadline(time.Now().Add(3 * time.Minute))
	h := sha256.New()
	buf := make([]byte, 256*1024)
	for reads := 1; ; reads++ {
		n, err := tn.Read(buf)
		h.Write(buf[:n])
		total += n
		peak = max(peak, hublink.TunnelMemory().Used)
		if errors.Is(err, io.EOF) {
			return h.Sum(nil), total, peak
		}
		if err != nil {
			t.Fatalf("after %d bytes: %v", total, err)
		}
		if slow && reads%8 == 0 {
			time.Sleep(time.Millisecond)
		}
	}
}

func TestBigAnswerReachesASlowReaderWhole(t *testing.T) {
	const total = 100 << 20
	port, want := byteSource(t, total)
	tun := newTunnels(port, 0)
	hl := hubOver(t, tun, false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	before := hublink.TunnelMemory()
	tn, err := hl.OpenTunnel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tn.Close()
	// Nobody reads for a while: the machine's side stops at the window instead of filling the hub.
	time.Sleep(500 * time.Millisecond)
	if held := hublink.TunnelMemory().Used - before.Used; held > 64*1024 {
		t.Fatalf("the hub holds %d bytes for a reader that has not started", held)
	}
	got, n, peak := readWhole(t, tn, true)
	if n != total || !bytes.Equal(got, want) {
		t.Fatalf("%d of %d bytes came, the same: %v", n, total, bytes.Equal(got, want))
	}
	// The widest window of one tunnel is a megabyte.
	if peak-before.Used > 1<<20 {
		t.Errorf("the hub held up to %d bytes for one tunnel", peak-before.Used)
	}
	if cut := hublink.TunnelMemory().Cut - before.Cut; cut != 0 {
		t.Errorf("%d tunnels were cut", cut)
	}
}

func TestAgentWithoutWindowStillCarriesAnAnswer(t *testing.T) {
	const total = 4 << 20
	port, want := byteSource(t, total)
	tun := newTunnels(port, 0)
	hl := hubOver(t, tun, true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tn, err := hl.OpenTunnel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tn.Close()
	got, n, _ := readWhole(t, tn, false)
	if n != total || !bytes.Equal(got, want) {
		t.Fatalf("%d of %d bytes came, the same: %v", n, total, bytes.Equal(got, want))
	}
}
