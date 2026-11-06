package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

func agentViewport() Viewport { return Viewport{Mode: "fixed", W: 1280, H: 800, DPR: 1} }

type dimensions struct {
	W      int `json:"w"`
	H      int `json:"h"`
	OuterW int `json:"outerW"`
	OuterH int `json:"outerH"`
}

func (i *Instance) dimensions(ctx context.Context, b binding) (dimensions, error) {
	if err := i.dialogError(b.target, "Runtime.evaluate"); err != nil {
		return dimensions{}, err
	}
	raw, err := b.conn.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "({w:innerWidth,h:innerHeight,outerW:outerWidth,outerH:outerHeight})",
		"returnByValue": true,
	}, b.session)
	var r struct {
		Result struct {
			Value dimensions `json:"value"`
		} `json:"result"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &r)
	}
	return r.Result.Value, err
}

// SetViewport changes the content size without holding the profile registry lock.
func (i *Instance) SetViewport(ctx context.Context, id string, v Viewport) error {
	return i.setViewport(ctx, id, v, func() error { return i.dialogError(id, "Runtime.evaluate") })
}

// AgentSetViewport rechecks ownership and control after acquiring the tab queue.
func (i *Instance) AgentSetViewport(ctx context.Context, conv, id string, v Viewport) error {
	return i.setViewport(ctx, id, v, func() error { return i.agentAllowed(conv, id, "Runtime.evaluate") })
}

func (i *Instance) setViewport(ctx context.Context, id string, v Viewport, allowed func() error) error {
	if err := i.beginOperation(); err != nil {
		return err
	}
	defer i.endOperation()
	if v.Mode != "fit" && v.Mode != "fixed" && v.Mode != "mobile" {
		return errors.New("invalid viewport mode")
	}
	if v.W < 1 || v.H < 1 || v.W > 7680 || v.H > 4320 || v.DPR < 0 || v.DPR > 4 {
		return errors.New("invalid viewport dimensions")
	}
	if v.DPR == 0 {
		v.DPR = 1
	}
	ctx, cancel := cdp.WithTimeout(ctx)
	defer cancel()
	if err := allowed(); err != nil {
		return err
	}
	p, c, err := i.lookup(id)
	if err != nil {
		return err
	}
	if err := allowed(); err != nil {
		return err
	}
	if err = lockPage(ctx, p); err != nil {
		return err
	}
	defer unlockPage(p)
	b, err := i.bind(ctx, p, c)
	if err != nil {
		return err
	}
	if err = allowed(); err != nil {
		return err
	}
	if err = i.viewport(ctx, b, v); err != nil {
		return err
	}
	i.mu.Lock()
	p.Viewport = v
	i.changedLocked()
	i.mu.Unlock()
	return nil
}

func (i *Instance) viewport(ctx context.Context, b binding, v Viewport) error {
	if err := i.dialogError(b.target, "Runtime.evaluate"); err != nil {
		return err
	}
	// Shared-window targets keep stale inner bounds while hidden, even after the native window resizes.
	if _, err := b.conn.Call(ctx, "Target.activateTarget", map[string]any{"targetId": b.target}, ""); err != nil {
		return err
	}
	if _, err := b.conn.Call(ctx, "Emulation.clearDeviceMetricsOverride", nil, b.session); err != nil {
		return err
	}
	_, err := b.conn.Call(ctx, "Browser.setWindowBounds", map[string]any{
		"windowId": b.window, "bounds": map[string]any{"windowState": "normal"},
	}, "")
	if err != nil {
		return err
	}
	// A timer also runs for hidden pages; requestAnimationFrame can wait forever there.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}
	d, err := i.dimensions(ctx, b)
	if err != nil {
		return err
	}
	emulated := v.W < 500 || v.Mode == "mobile" || v.DPR != 1
	width := v.W
	if emulated {
		width = max(width, 500)
	}
	if _, err = b.conn.Call(ctx, "Browser.setWindowBounds", map[string]any{
		"windowId": b.window, "bounds": map[string]any{
			"width": width + max(0, d.OuterW-d.W), "height": v.H + max(0, d.OuterH-d.H),
		},
	}, ""); err != nil {
		return err
	}
	if emulated {
		_, err = b.conn.Call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
			"width": v.W, "height": v.H, "deviceScaleFactor": v.DPR,
			"mobile": v.Mode == "mobile", "screenWidth": v.W, "screenHeight": v.H,
		}, b.session)
		return err
	}
	// Window managers resize asynchronously. Report success only after Chrome sees the requested content bounds.
	for n := 0; n < 40; n++ {
		d, err = i.dimensions(ctx, b)
		if err != nil {
			return err
		}
		if d.W == v.W && d.H == v.H {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return fmt.Errorf("viewport is %dx%d, requested %dx%d", d.W, d.H, v.W, v.H)
}
