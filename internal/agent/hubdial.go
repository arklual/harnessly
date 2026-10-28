package agent

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	hubPauseMax = 30 * time.Second
	// A hub that does not know this machine's key, or whose clock is far from ours, answers 401 for as long as that
	// holds; asking every second would only fill its log.
	hubRejectedPause = time.Minute
	hubOldPause      = time.Hour
)

// dialHub keeps one signed outbound socket to the hub: the machine calls the hub, never the other way round.
func (h *hub) dialHub(ctx context.Context) {
	pause := time.Second
	for ctx.Err() == nil {
		if h.policy.Disabled() {
			if !sleepCtx(ctx, switchPoll) {
				return
			}
			continue
		}
		retryIn, stop := h.dialOnce(ctx, &pause)
		if stop {
			return
		}
		delay := retryIn
		if delay == 0 {
			delay = pause
			pause = min(pause*2, hubPauseMax)
		}
		if !sleepCtx(ctx, delay) {
			return
		}
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
func (h *hub) dialOnce(ctx context.Context, pause *time.Duration) (time.Duration, bool) {
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
			h.revoke()
			return 0, true
		case hubproto.StatusUpgradeAgent:
			Log("warn", "the hub needs a newer harnessly-agent than this one; update it")
			h.setLink(false, "the hub needs a newer harnessly-agent")
			return hubOldPause, false
		}
		Log("info", "the hub did not take the call", F{"status", res.StatusCode})
		h.setLink(false, "the hub did not take the call: HTTP "+strconv.Itoa(res.StatusCode))
		return 0, false
	}
	*pause = time.Second
	Log("info", "dialled the hub", F{"url", m.DialURL})

	heard := make(chan struct{}, 1)
	done := make(chan struct{})
	go keepAlive(ctx, conn, heard, done)
	err = h.a.serveLink(conn, heard, true)
	close(done)
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
		// A newer link of this machine took over: call again at once.
		*pause = time.Second
		return time.Second, false
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
