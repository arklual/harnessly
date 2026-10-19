package timeline

import (
	"time"

	"harnessly/internal/agents"
)

// The reply a working Claude has printed but not yet written, read off the screen once a second for the sessions
// somebody watches. One loop for all of them; it stops when nobody is left.

func (s *Server) ensureLivePoller() {
	if s.liveStop != nil {
		return
	}
	stop := make(chan struct{})
	s.liveStop = stop
	go func() {
		t := time.NewTicker(s.d.Timing.LiveReplyEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
			}
			s.pollLiveReplies(stop)
		}
	}()
}

type liveTarget struct {
	session string
	conns   []*conn
	busy    bool
}

func (s *Server) pollLiveReplies(stop chan struct{}) {
	s.mu.Lock()
	if s.liveBusy {
		s.mu.Unlock()
		return
	}
	var targets []*liveTarget
	index := map[string]*liveTarget{}
	for _, cn := range s.order {
		if cn.cleaned || cn.provider.ID != agents.Claude {
			continue
		}
		t := index[cn.session]
		if t == nil {
			t = &liveTarget{session: cn.session}
			index[cn.session] = t
			targets = append(targets, t)
		}
		t.conns = append(t.conns, cn)
	}
	if len(targets) == 0 {
		if s.liveStop == stop {
			close(stop)
			s.liveStop = nil
		}
		s.mu.Unlock()
		return
	}
	s.liveBusy = true
	k := s.keeper()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.liveBusy = false
		s.mu.Unlock()
	}()
	for _, t := range targets {
		t.busy = k != nil && k.IsSessionBusy(t.session)
		s.mu.Lock()
		needed := t.busy
		for _, cn := range t.conns {
			if cn.liveSent != "" {
				needed = true
			}
		}
		s.mu.Unlock()
		if !needed {
			continue
		}
		text := ""
		if t.busy {
			if scr, ok := s.d.Tmux.Capture(t.session); ok {
				text = readLiveReply(scr)
			}
		}
		raw := marshal(msg("type", "timeline:live-text", "text", text))
		s.mu.Lock()
		for _, cn := range t.conns {
			if cn.liveSent == text || !cn.c.open() {
				continue
			}
			cn.liveSent = text
			cn.c.send(raw)
		}
		s.mu.Unlock()
	}
}
