package hlcli

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hookspool"
	"github.com/ly-solutions/harnessly/internal/safego"
	"github.com/ly-solutions/harnessly/internal/screen"
)

const (
	// hookDeliverWait is how long the status hook waits for the runtime: the agent's turn waits on its hooks, and an
	// event the runtime did not take in time is in the spool for it.
	hookDeliverWait = time.Second
	// hookInputWait is how long the hook reads the agent's JSON: a stdin that nobody closes must not hold it.
	hookInputWait = 500 * time.Millisecond
)

// hookStatus is Claude's lifecycle hook (`harnessly hook status <event>`, the agent's JSON on stdin): the event is
// written to the spool of the data folder and then told to the runtime. A runtime that is away, restarting or slow
// finds it in the spool, so the end of a turn is not lost with one request. It prints nothing and never fails: what a
// hook prints is the agent's to read.
func (c *cli) hookStatus(event string) {
	var in struct {
		SessionID        string `json:"session_id"`
		NotificationType string `json:"notification_type"`
		Prompt           string `json:"prompt"`
	}
	_ = json.Unmarshal(c.hookInput(), &in)
	now := time.Now
	if c.env.Now != nil {
		now = c.env.Now
	}
	session := strings.TrimSpace(c.env.Getenv("HARNESSLY_SESSION"))
	if session == "" && c.env.Session != nil {
		session = c.env.Session()
	}
	body := map[string]any{"event": event, "session": session, "at": now().UnixMilli()}
	if in.SessionID != "" {
		// The agent's own session: a Claude started from the tab's terminal shares the tab's tmux session, not this.
		body["agentSessionId"] = in.SessionID
	}
	if event == "notification" && in.NotificationType != "" {
		body["notificationType"] = in.NotificationType
	}
	if hash := screen.MessageHash(in.Prompt); event == "prompt-submit" && hash != "" {
		// Which prompt the agent took, not its words: the queue confirms the message it typed by this, and a prompt the
		// person typed in the terminal is not taken for it.
		body["promptHash"] = hash
	}
	if id := hookspool.NewID(); id != "" && session != "" {
		body["id"] = id
		if line, err := json.Marshal(body); err == nil {
			// A data folder the agent cannot write (a walled chat) keeps no spool: the delivery below is all there is.
			_ = hookspool.Append(filepath.Join(c.dataDir(), hookspool.File), line)
		}
	}
	client := http.Client{Timeout: hookDeliverWait}
	if c.env.Client != nil {
		client = *c.env.Client
		client.Timeout = hookDeliverWait
	}
	deliver := *c
	deliver.env.Client = &client
	_, _, _ = deliver.call(http.MethodPost, "/api/status/hook", nil, body)
}

// hookInput is the agent's JSON on stdin; nothing when the hook is run by hand from a terminal.
func (c *cli) hookInput() []byte {
	in := c.env.Stdin
	if in == nil {
		return nil
	}
	if f, ok := in.(*os.File); ok {
		if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice != 0 {
			return nil
		}
	}
	read := make(chan []byte, 1)
	safego.Go("hook input", func() {
		raw, _ := io.ReadAll(io.LimitReader(in, 1<<20))
		read <- raw
	})
	select {
	case raw := <-read:
		return raw
	case <-time.After(hookInputWait):
		return nil
	}
}
