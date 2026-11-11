package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// SetHeaderWait shortens the wait for a runtime's answer; the returned func puts it back.
func SetHeaderWait(d time.Duration) (restore func()) {
	old := headerWait
	headerWait = d
	return func() { headerWait = old }
}

// SetMaxPerPerson changes how many requests of a machine a person holds; the returned func puts it back.
func SetMaxPerPerson(n int) (restore func()) {
	old := maxPerPerson
	maxPerPerson = n
	return func() { maxPerPerson = old }
}

func TestHeaderLimit(t *testing.T) {
	get := func(target string, headers ...string) *http.Request {
		r := httptest.NewRequest("GET", target, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		return r
	}
	for _, c := range []struct {
		name string
		r    *http.Request
		want time.Duration
	}{
		{"an ordinary request", get("/m/x/api/files"), headerWait},
		{"a websocket handshake", get("/m/x/ws", "Upgrade", "websocket"), headerWait},
		{"an event stream", get("/m/x/api/events", "Accept", "text/event-stream"), 0},
		{"an MCP call", get("/m/x/mcp", "Accept", "application/json, text/event-stream"), 0},
		{"a long poll of 300 s", get("/m/x/changes?since=4&wait=300"), 300*time.Second + longPollMargin},
		{"a short poll", get("/m/x/changes?wait=0"), headerWait},
		{"a poll with a timeout", get("/m/x/asks/1/wait?timeout=600"), 600*time.Second + longPollMargin},
		{"a wait that names no time", get("/m/x/asks/1/wait"), 0},
		{"a wait that is not a number", get("/m/x/stats?wait=forever"), 0},
	} {
		if got := headerLimit(c.r); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
