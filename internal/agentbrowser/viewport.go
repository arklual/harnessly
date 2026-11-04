package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type dimensions struct {
	W      int `json:"w"`
	H      int `json:"h"`
	OuterW int `json:"outerW"`
	OuterH int `json:"outerH"`
}

func (i *Instance) dimensions(ctx context.Context, p *page) (dimensions, error) {
	raw, err := i.conn.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "({w:innerWidth,h:innerHeight,outerW:outerWidth,outerH:outerHeight})", "returnByValue": true}, p.session)
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
func (i *Instance) SetViewport(ctx context.Context, id string, v Viewport) error {
	if v.Mode != "fit" && v.Mode != "fixed" && v.Mode != "mobile" {
		return errors.New("invalid viewport mode")
	}
	if v.W < 1 || v.H < 1 || v.W > 7680 || v.H > 4320 || v.DPR < 0 || v.DPR > 4 {
		return errors.New("invalid viewport dimensions")
	}
	if v.DPR == 0 {
		v.DPR = 1
	}
	i.op.Lock()
	defer i.op.Unlock()
	i.mu.Lock()
	p := i.pages[id]
	i.mu.Unlock()
	if p == nil {
		return errors.New("unknown browser tab")
	}
	if err := i.viewport(ctx, p, v); err != nil {
		return err
	}
	i.mu.Lock()
	p.Viewport = v
	i.notifyLocked()
	i.mu.Unlock()
	return i.save()
}
func (i *Instance) viewport(ctx context.Context, p *page, v Viewport) error {
	if _, err := i.conn.Call(ctx, "Emulation.clearDeviceMetricsOverride", nil, p.session); err != nil {
		return err
	}
	raw, err := i.conn.Call(ctx, "Browser.getWindowForTarget", map[string]any{"targetId": p.ID}, "")
	if err != nil {
		return err
	}
	var r struct {
		ID int `json:"windowId"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return err
	}
	if _, err = i.conn.Call(ctx, "Browser.setWindowBounds", map[string]any{"windowId": r.ID, "bounds": map[string]any{"windowState": "normal"}}, ""); err != nil {
		return err
	}
	// Clearing emulation and restoring window state both schedule a layout; measure after it commits.
	if _, err = i.conn.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))", "awaitPromise": true}, p.session); err != nil {
		return err
	}
	d, err := i.dimensions(ctx, p)
	if err != nil {
		return err
	}
	emulated := v.W < 500 || v.Mode == "mobile" || v.DPR != 1
	width := v.W
	if emulated {
		width = max(width, 500)
	}
	if _, err = i.conn.Call(ctx, "Browser.setWindowBounds", map[string]any{"windowId": r.ID, "bounds": map[string]any{"width": width + max(0, d.OuterW-d.W), "height": v.H + max(0, d.OuterH-d.H)}}, ""); err != nil {
		return err
	}
	if emulated {
		_, err = i.conn.Call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": v.W, "height": v.H, "deviceScaleFactor": v.DPR, "mobile": v.Mode == "mobile", "screenWidth": v.W, "screenHeight": v.H}, p.session)
		return err
	}
	// Window managers resize asynchronously. Report success only after Chrome sees the requested content bounds.
	for n := 0; n < 40; n++ {
		d, err = i.dimensions(ctx, p)
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
