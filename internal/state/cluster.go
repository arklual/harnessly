package state

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The cluster owns a pair's state once the machine agent says so in state-mode.json; it is re-read at most once a
// second.
const modeCacheFor = time.Second

type modeCache struct {
	owned bool
	at    time.Time
	read  bool
}

func (s *Store) owned() bool {
	s.modeMu.Lock()
	defer s.modeMu.Unlock()
	now := time.Now()
	if s.mode.read && now.Sub(s.mode.at) < modeCacheFor {
		return s.mode.owned
	}
	owned := false
	if raw, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "state-mode.json")); err == nil {
		if v, err := jsvalue.Parse(raw); err == nil {
			if o, ok := v.(*jsvalue.Object); ok {
				b, _ := o.Get("owned")
				owned = b == true
			}
		}
	}
	s.mode = modeCache{owned: owned, at: now, read: true}
	return owned
}

// callAgent posts to the machine agent's loopback state route and returns the answer when the cluster took it.
func (s *Store) callAgent(route string, body *jsvalue.Object) (*jsvalue.Object, bool) {
	raw, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "agent.json"))
	if err != nil {
		return nil, false
	}
	v, err := jsvalue.Parse(raw)
	info, _ := v.(*jsvalue.Object)
	if err != nil || info == nil {
		return nil, false
	}
	port := ""
	switch p, _ := info.Get("port"); x := p.(type) {
	case float64:
		if x != 0 {
			port = strconv.FormatFloat(x, 'f', -1, 64)
		}
	case string:
		port = x
	}
	token := info.String("token")
	if port == "" || token == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), commitTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+route, bytes.NewReader(jsvalue.Stringify(body, "")))
	if err != nil {
		return nil, false
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set(s.cfg.AgentTokenHeader, token)
	res, err := s.cfg.HTTP.Do(req)
	if err != nil {
		agent.Log("warn", route+" could not reach the agent", agent.F{K: "error", V: err.Error()})
		return nil, false
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	parsed, _ := jsvalue.Parse(data)
	result, _ := parsed.(*jsvalue.Object)
	if ok, _ := result.Get("ok"); res.StatusCode/100 != 2 || ok != true {
		reason := strconv.Itoa(res.StatusCode)
		if e := result.String("error"); e != "" {
			reason = e
		}
		agent.Log("warn", route+" not taken by the cluster", agent.F{K: "reason", V: reason})
		return nil, false
	}
	return result, true
}

// commit sends a whole document (nil removes it) and returns the cluster's copy of it.
func (s *Store) commit(name string, base any, content *jsvalue.Object) (*jsvalue.Object, bool) {
	body := jsvalue.NewObject("name", name, "base", base)
	if content == nil {
		body.Set("content", nil)
	} else {
		body.Set("content", content)
	}
	result, ok := s.callAgent("/state/commit", body)
	if !ok {
		return nil, false
	}
	c := result.Object("content")
	return c, c != nil
}

// patch sends the fields a tab change set and removed.
func (s *Store) patch(name, sessionName string, set *jsvalue.Object, unset []string) bool {
	_, ok := s.callAgent("/state/patch", jsvalue.NewObject("name", name, "sessionName", sessionName, "set", set, "unset", jsvalue.StringArray(unset)))
	return ok
}
