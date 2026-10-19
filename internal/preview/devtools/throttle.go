package devtools

import (
	"io"
	"math"
	"time"
)

// Network conditions of a slot as the proxy applies them: latency before the upstream is asked, and the slot's
// bandwidth shared by all its requests, as one connection would share it. Settings are read per slice, so a change
// applies to bodies already flowing.

func IsOffline(t Throttle) bool { return t.Preset == "offline" }

func IsThrottled(t Throttle) bool {
	return IsOffline(t) || t.LatencyMs > 0 || t.DownKbps > 0 || t.UpKbps > 0
}

// bytesPerSecond counts a kilobit as 1000 bits, as Chrome does.
func bytesPerSecond(kbps int64) float64 { return float64(kbps) * 1000 / 8 }

func rateOf(t Throttle, down bool) float64 {
	if IsOffline(t) {
		return 0
	}
	if down {
		return bytesPerSecond(t.DownKbps)
	}
	return bytesPerSecond(t.UpKbps)
}

// link is one virtual clock per slot and direction: when the link is free again (ms).
type link struct {
	freeAt float64
	rate   float64
}

// reserve is how long until bytes have crossed the link, queued behind what the slot's other requests already sent. A
// new rate forgets the old queue: a link that was slow must not hold a fast one back.
func reserve(l *link, bytes int, rate, now float64) float64 {
	if l.rate != rate {
		l.rate = rate
		l.freeAt = math.Min(l.freeAt, now)
	}
	l.freeAt = math.Max(now, l.freeAt) + float64(bytes)*1000/rate
	return l.freeAt - now
}

const (
	minSlice = 256
	maxSlice = 16 * 1024
)

// Pacer passes a body on at the slot's rate, in slices of a quarter second at most. OnSlice sees every slice that left.
type Pacer struct {
	store   *Store
	slot    string
	down    bool
	w       io.Writer
	OnSlice func(int)
	done    <-chan struct{}
}

// NewPacer writes to w at the slot's rate in that direction until done closes.
func (s *Store) NewPacer(slot string, down bool, w io.Writer, done <-chan struct{}, onSlice func(int)) *Pacer {
	return &Pacer{store: s, slot: slot, down: down, w: w, OnSlice: onSlice, done: done}
}

func (p *Pacer) Write(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		rate := rateOf(p.store.Settings(p.slot).Throttle, p.down)
		if rate == 0 {
			n, err := p.w.Write(b[written:])
			written += n
			if p.OnSlice != nil && n > 0 {
				p.OnSlice(n)
			}
			return written, err
		}
		size := int(math.Max(minSlice, math.Min(maxSlice, math.Floor(rate/4))))
		piece := b[written:min(len(b), written+size)]
		wait := p.store.reserveLink(p.slot, p.down, len(piece), rate)
		t := time.NewTimer(time.Duration(wait * float64(time.Millisecond)))
		select {
		case <-t.C:
		case <-p.done:
			t.Stop()
			return written, io.ErrClosedPipe
		}
		if p.OnSlice != nil {
			p.OnSlice(len(piece))
		}
		n, err := p.w.Write(piece)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (s *Store) reserveLink(slot string, down bool, bytes int, rate float64) float64 {
	key := slot + "\nup"
	if down {
		key = slot + "\ndown"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.links[key]
	if l == nil {
		l = &link{}
		s.links[key] = l
	}
	return reserve(l, bytes, rate, float64(s.Now().UnixNano())/1e6)
}
