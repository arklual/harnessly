package mediagen

import (
	"context"
	"errors"
)

// backend is one way to Higgsfield: its REST API by a key (hf), or its MCP server through the person's connection
// (hfMCP, Higgsfield MCP 2.0: a subscription or a free trial instead of a key, and music, speech and sound too).
type backend interface {
	// estimate is the price of a plan, before anything is sent: in USD, and in the provider's credits when it counts in
	// them (0 otherwise).
	estimate(ctx context.Context, pl plan) (usd, credits float64, err error)
	// ref puts a frame (a file or an address) where the provider reads it and gives what the request names it by.
	ref(ctx context.Context, frame string) (string, error)
	// submit sends the plan with its frames' refs; idem is the job's id, so a retry after a lost answer is the same request.
	submit(ctx context.Context, pl plan, refs []string, idem string) (string, error)
	status(ctx context.Context, rid string) (hfStatus, error)
	cancel(ctx context.Context, rid string) error
}

// standIn is the address a frame is priced by before it is uploaded.
const standIn = "https://example.com/frame.jpg"

// rest is the REST API as a backend.
type rest struct{ c *hf }

func (r rest) estimate(ctx context.Context, pl plan) (float64, float64, error) {
	usd, err := r.c.estimate(ctx, pl.endpoint, fillFrames(pl.body, func(int) string { return standIn }))
	return usd, 0, err
}

func (r rest) ref(ctx context.Context, frame string) (string, error) {
	if isURL(frame) {
		return frame, nil
	}
	return r.c.upload(ctx, frame)
}

func (r rest) submit(ctx context.Context, pl plan, refs []string, idem string) (string, error) {
	return r.c.submit(ctx, pl.endpoint, fillFrames(pl.body, func(i int) string { return refs[i] }), idem)
}

func (r rest) status(ctx context.Context, rid string) (hfStatus, error) { return r.c.status(ctx, rid) }
func (r rest) cancel(ctx context.Context, rid string) error             { return r.c.cancel(ctx, rid) }

// backendFor is the way to a target.
func (s *Service) backendFor(tg target) (backend, error) {
	if tg.mcp {
		if s.MCP == nil {
			return nil, errors.New("the Higgsfield MCP connection is not reachable on this machine")
		}
		return &hfMCP{call: s.MCP.Call, http: s.client(), allowHTTP: s.AllowHTTP}, nil
	}
	return rest{c: &hf{base: tg.base, key: tg.key, http: s.client(), allowHTTP: s.AllowHTTP}}, nil
}
