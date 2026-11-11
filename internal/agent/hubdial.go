package agent

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	hubPauseMin = time.Second
	hubPauseMax = 30 * time.Second
	// hubLinkSettled is how long a link has to live before the next trouble starts from the shortest pause again: a link
	// the hub takes and drops at once is no sign that calling fast is welcome.
	hubLinkSettled = time.Minute
	// A machine whose link another one keeps taking over (two agents with one key) calls ever more rarely.
	hubReplacedPauseMax = 5 * time.Minute
	// A hub that does not know this machine's key, or whose clock is far from ours, answers 401 for as long as that
	// holds; asking every second would only fill its log.
	hubRejectedPause = time.Minute
	hubOldPause      = time.Hour
)

// dialHub keeps one signed outbound socket to the hub: the machine calls the hub, never the other way round.
func (h *hub) dialHub(ctx context.Context) {
	pause := &hubBackoff{}
	for ctx.Err() == nil {
		if h.policy.Disabled() {
			if !sleepCtx(ctx, switchPoll) {
				return
			}
			continue
		}
		retryIn, stop := h.dialOnce(ctx, pause)
		if stop {
			return
		}
		delay := retryIn
		if delay == 0 {
			delay = pause.next()
		}
		if !sleepCtx(ctx, delay) {
			return
		}
	}
}

// hubBackoff is the pause between calls to the hub. Every agent of a hub loses its link at the same moment when the
// hub restarts, so the pause is drawn at random from the whole of a window that doubles with each call in a row (full
// jitter): the calls spread out instead of arriving together.
type hubBackoff struct {
	window   time.Duration // the next pause is drawn from [0, window); 0 means hubPauseMin
	replaced int           // links in a row that another link of this machine took over
	rnd      func() float64
}

func (b *hubBackoff) random() float64 {
	if b.rnd != nil {
		return b.rnd()
	}
	return rand.Float64()
}

// next is the pause before the next call after one that failed or a link that ended.
func (b *hubBackoff) next() time.Duration {
	window := max(b.window, hubPauseMin)
	b.window = min(window*2, hubPauseMax)
	return time.Duration(b.random() * float64(window))
}

// nextReplaced is the pause after another link of this machine took this one over: it doubles up to
// hubReplacedPauseMax, and is never short, so that two agents with one key do not push each other out every second.
func (b *hubBackoff) nextReplaced() time.Duration {
	window := hubReplacedPauseMax
	if b.replaced < 16 {
		window = min(hubPauseMin<<b.replaced, hubReplacedPauseMax)
	}
	b.replaced++
	return window/2 + time.Duration(b.random()*float64(window/2))
}

// ended is told how long a link lived: only one that settled brings the pauses back to the shortest.
func (b *hubBackoff) ended(lived time.Duration) {
	if lived >= hubLinkSettled {
		b.window, b.replaced = 0, 0
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// dialOnce makes one call and serves the link until it ends. It says how long to wait before the next call (0: the
// usual backoff) or that there is to be none.
func (h *hub) dialOnce(ctx context.Context, pause *hubBackoff) (time.Duration, bool) {
	m := h.machine
	host, err := hubproto.DialHost(m.DialURL)
	if err != nil {
		Log("error", "the hub address in machine.json is not usable", F{"error", err.Error()})
		return hubOldPause, false
	}
	auth, err := hubproto.SignDial(m.SigningKey(), m.MachineID, time.Now(), host)
	if err != nil {
		Log("error", "dial not signed", F{"error", err.Error()})
		return hubRejectedPause, false
	}
	header := http.Header{}
	header.Set("Authorization", auth)
	header.Set("X-Harnessly-Protocol", "2")
	header.Set("X-Harnessly-Agent-Version", h.a.cfg.Version)
	dialCtx, cancel := context.WithTimeout(ctx, dialHandshake)
	conn, res, err := websocket.Dial(dialCtx, m.DialURL, &websocket.DialOptions{HTTPHeader: header, CompressionMode: websocket.CompressionDisabled})
	cancel()
	if err != nil {
		if res == nil || res.StatusCode == http.StatusSwitchingProtocols {
			Log("warn", "hub link error", F{"error", err.Error()})
			h.setLink(false, "could not reach the hub: "+err.Error())
			return 0, false
		}
		switch res.StatusCode {
		case http.StatusConflict:
			// Not the hub node that holds the machine: the next call lands elsewhere.
			return 300*time.Millisecond + time.Duration(rand.Float64()*700)*time.Millisecond, false
		case http.StatusUnauthorized:
			Log("warn", "the hub did not accept this machine's signature: check that this machine's clock is right and that the machine was not removed", F{"hubDate", res.Header.Get("Date")})
			h.setLink(false, "the hub did not accept this machine's signature: check the clock, or the machine was removed")
			return hubRejectedPause, false
		case http.StatusForbidden:
			// Only the hub's own refusal of a revoked machine ends the calls; a 403 of a proxy or a firewall in front of
			// the hub is passing trouble, retried with the usual backoff.
			body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
			if hubproto.IsRevokedDial(body) {
				h.revoke()
				return 0, true
			}
			Log("warn", "the hub's address answered 403 without the hub's revocation; calling again", F{"server", res.Header.Get("Server")})
			h.setLink(false, "the hub's address refused the call (HTTP 403, not a revocation): calling again")
			return 0, false
		case hubproto.StatusUpgradeAgent:
			Log("warn", "the hub needs a newer harnessly-agent than this one; update it")
			h.setLink(false, "the hub needs a newer harnessly-agent")
			return hubOldPause, false
		}
		Log("info", "the hub did not take the call", F{"status", res.StatusCode})
		h.setLink(false, "the hub did not take the call: HTTP "+strconv.Itoa(res.StatusCode))
		return 0, false
	}
	linked := time.Now()
	Log("info", "dialled the hub", F{"url", m.DialURL})

	heard := make(chan struct{}, 1)
	done := make(chan struct{})
	go keepAlive(ctx, conn, heard, done)
	select { // a relink asked before this link came up is already answered by this link's hello
	case <-h.relink:
	default:
	}
	go func() {
		select {
		case <-h.relink:
			Log("info", "calling the hub again: what this machine offers changed")
			_ = conn.Close(websocket.StatusNormalClosure, "features changed")
		case <-done:
		}
	}()
	err = h.a.serveLink(conn, heard, true)
	close(done)
	pause.ended(time.Since(linked))
	switch websocket.CloseStatus(err) {
	case hubproto.CloseRevoked:
		h.revoke()
		return 0, true
	case hubproto.CloseProtocolOld:
		Log("warn", "the hub needs a newer harnessly-agent than this one; update it")
		h.setLink(false, "the hub needs a newer harnessly-agent")
		return hubOldPause, false
	case hubproto.CloseSwitchedOff:
		return time.Minute, false
	case hubproto.CloseReplaced:
		// A newer link of this machine took over: call again, later each time it happens in a row.
		return pause.nextReplaced(), false
	}
	return 0, false
}

// revoke: the hub says this machine is no longer its own. The agent stops calling until it is restarted.
func (h *hub) revoke() {
	h.mu.Lock()
	h.revoked = true
	h.mu.Unlock()
	h.audit.Record("revoked", true, nil)
	h.setLink(false, "revoked by the hub")
	Log("error", "machine revoked by the hub; not dialling again until the agent is restarted")
}
