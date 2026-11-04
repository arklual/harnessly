package agentbrowser

import (
	"context"
	"encoding/binary"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

// Frame contains a JPEG and the page size in CSS pixels, independent of JPEG scaling.
type Frame struct {
	Width, Height int
	JPEG          []byte
}

// Encode prefixes a frame with dimensions and the panel's selection generation.
func (f Frame) Encode(gen uint32) []byte {
	b := make([]byte, 12+len(f.JPEG))
	binary.BigEndian.PutUint32(b, uint32(max(0, f.Width)))
	binary.BigEndian.PutUint32(b[4:], uint32(max(0, f.Height)))
	binary.BigEndian.PutUint32(b[8:], gen)
	copy(b[12:], f.JPEG)
	return b
}

// Viewer holds only the most recent frame for one subscriber.
type Viewer struct {
	frames   chan Frame
	instance *Instance
	tab      string
	once     sync.Once
}

// Frames returns the latest-frame mailbox, which remains open until the viewer is discarded.
func (v *Viewer) Frames() <-chan Frame { return v.frames }

// Close removes the viewer immediately and schedules stopping an unused screencast.
func (v *Viewer) Close() {
	v.once.Do(func() {
		i := v.instance
		i.mu.Lock()
		p := i.pages[v.tab]
		if p != nil {
			delete(p.viewers, v)
		}
		i.mu.Unlock()
		if p == nil {
			return
		}
		i.task(func() {
			ctx, cancel := cdp.WithTimeout(i.manager.ctx)
			defer cancel()
			if lockPage(ctx, p) != nil {
				return
			}
			defer unlockPage(p)
			i.mu.Lock()
			stop, b := len(p.viewers) == 0, p.binding
			i.mu.Unlock()
			if stop && b.conn != nil {
				_, _ = b.conn.Call(ctx, "Page.stopScreencast", nil, b.session)
			}
		})
	})
}

// Watch activates the target and subscribes to its screencast.
func (i *Instance) Watch(ctx context.Context, id string) (*Viewer, error) {
	if err := i.beginOperation(); err != nil {
		return nil, err
	}
	defer i.tasks.Done()
	ctx, cancel := cdp.WithTimeout(ctx)
	defer cancel()
	p, c, err := i.lookup(id)
	if err != nil {
		return nil, err
	}
	if err = lockPage(ctx, p); err != nil {
		return nil, err
	}
	defer unlockPage(p)
	b, err := i.bind(ctx, p, c)
	if err != nil {
		return nil, err
	}
	// Site-created tabs may share a window. Activation makes a hidden target paint again.
	if _, err = c.Call(ctx, "Target.activateTarget", map[string]any{"targetId": id}, ""); err != nil {
		return nil, err
	}
	i.mu.Lock()
	first := len(p.viewers) == 0
	i.mu.Unlock()
	if first {
		if err = i.startCast(ctx, id, b); err != nil {
			return nil, err
		}
	}
	v := &Viewer{frames: make(chan Frame, 1), instance: i, tab: id}
	i.mu.Lock()
	p.viewers[v] = true
	if p.last != nil {
		latest(v.frames, *p.last)
	}
	i.mu.Unlock()
	return v, nil
}

func (i *Instance) startCast(ctx context.Context, id string, b binding) error {
	if _, err := b.conn.Call(ctx, "Target.activateTarget", map[string]any{"targetId": id}, ""); err != nil {
		return err
	}
	_, err := b.conn.Call(ctx, "Page.startScreencast", map[string]any{
		"format": "jpeg", "quality": 75, "maxWidth": 1920, "maxHeight": 1400,
	}, b.session)
	return err
}

type ack struct {
	conn    *cdp.Conn
	session string
	id      int64
}

func (i *Instance) acknowledge(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			i.mu.Lock()
			// Flush the last change even if the page becomes static between acknowledgement intervals.
			for _, p := range i.pages {
				if p.framePending && p.last != nil && now.Sub(p.delivered) >= 50*time.Millisecond {
					p.framePending = false
					p.delivered = now
					for v := range p.viewers {
						latest(v.frames, *p.last)
					}
				}
			}
			ready := []ack{}
			pending := i.ackQueue[:0]
			for _, a := range i.ackQueue {
				if !now.Before(i.ackNext[a.session]) {
					ready = append(ready, a)
					i.ackNext[a.session] = now.Add(50 * time.Millisecond)
				} else {
					pending = append(pending, a)
				}
			}
			i.ackQueue = pending
			i.mu.Unlock()
			for _, a := range ready {
				if a.conn != nil {
					_ = a.conn.Send(ctx, "Page.screencastFrameAck", map[string]any{"sessionId": a.id}, a.session)
				}
			}
		}
	}
}
