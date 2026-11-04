package agentbrowser

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

type Frame struct {
	Width, Height int
	JPEG          []byte
}

func (f Frame) Encode(gen uint32) []byte {
	b := make([]byte, 12+len(f.JPEG))
	binary.BigEndian.PutUint32(b, uint32(max(0, f.Width)))
	binary.BigEndian.PutUint32(b[4:], uint32(max(0, f.Height)))
	binary.BigEndian.PutUint32(b[8:], gen)
	copy(b[12:], f.JPEG)
	return b
}

type Viewer struct {
	frames   chan Frame
	instance *Instance
	tab      string
	once     sync.Once
}

func (v *Viewer) Frames() <-chan Frame { return v.frames }
func (v *Viewer) Close() {
	v.once.Do(func() {
		i := v.instance
		i.op.Lock()
		defer i.op.Unlock()
		i.mu.Lock()
		p := i.pages[v.tab]
		stop := false
		if p != nil {
			delete(p.viewers, v)
			stop = len(p.viewers) == 0
		}
		i.mu.Unlock()
		if stop {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = i.conn.Call(ctx, "Page.stopScreencast", nil, p.session)
		}
	})
}
func (i *Instance) Watch(ctx context.Context, id string) (*Viewer, error) {
	i.op.Lock()
	defer i.op.Unlock()
	i.mu.Lock()
	p := i.pages[id]
	if p == nil {
		i.mu.Unlock()
		return nil, errors.New("unknown browser tab")
	}
	v := &Viewer{frames: make(chan Frame, 1), instance: i, tab: id}
	first := len(p.viewers) == 0
	p.viewers[v] = true
	if p.last != nil {
		latest(v.frames, *p.last)
	}
	i.mu.Unlock()
	if first {
		if err := i.startCast(ctx, p); err != nil {
			i.mu.Lock()
			delete(p.viewers, v)
			i.mu.Unlock()
			return nil, err
		}
	}
	return v, nil
}
func (i *Instance) startCast(ctx context.Context, p *page) error {
	if _, err := i.conn.Call(ctx, "Page.enable", nil, p.session); err != nil {
		return err
	}
	_, err := i.conn.Call(ctx, "Page.startScreencast", map[string]any{"format": "jpeg", "quality": 75}, p.session)
	return err
}
