package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/imagegen"
	"github.com/ly-solutions/harnessly/internal/mediagen"
)

// mediaPictures draws the pictures of imagegen's provider Higgsfield as jobs of the media service: its price checks, its
// cap per job and per day, its download of the result. The media jobs are owned by "images", so no chat shows them as
// cards of their own: the picture job is the card.
type mediaPictures struct{ svc *mediagen.Service }

func (m mediaPictures) Available() bool { return m.svc.ImageVia() != "" }

// mediaRequest is the media service's request for one picture of a panel.
func (m mediaPictures) mediaRequest(d imagegen.MediaDraw) mediagen.Request {
	req := mediagen.Request{Kind: extproviders.KindImage, Model: d.Model, Prompt: d.Prompt, Ratio: d.Ratio, Owner: "images"}
	if m.svc.ImageVia() == "mcp" {
		// The person picked the model and saw the price on the button: the plan's credits pay.
		req.Provider, req.Allowance = mediagen.MCPConnID, "credits"
	}
	if d.Reference != "" {
		ref := d.Reference
		req.Frames = []string{ref}
		req.FrameOK = func(p string) bool { return p == ref }
	}
	return req
}

func (m mediaPictures) Draw(ctx context.Context, d imagegen.MediaDraw) (string, error) {
	snap, err := m.svc.Start(m.mediaRequest(d))
	if err != nil {
		return "", mediaRefusal(err)
	}
	end, _ := m.svc.Wait(ctx, snap.ID)
	if !end.State.Ended() {
		cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, _ = m.svc.Cancel(cctx, snap.ID)
		cancel()
		return "", errors.New("the picture was not made in time")
	}
	if end.State != mediagen.Done || len(end.Files) == 0 {
		if end.Error != "" {
			return "", errors.New(end.Error)
		}
		return "", fmt.Errorf("Higgsfield made no picture (%s)", end.State)
	}
	return end.Files[0].Path, nil
}

// mediaRefusal is a media refusal in the words a page shows.
func mediaRefusal(err error) error {
	switch {
	case errors.Is(err, mediagen.ErrNoMCP):
		return errors.New(mediagen.NoMCPMessage)
	case errors.Is(err, mediagen.ErrNoKey):
		return errors.New(mediagen.NoKeyMessage)
	}
	return err
}

// imageProvider is a generator a page may offer in its list.
type imageProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Paid: each picture spends the person's balance there; a page shows the price before it draws.
	Paid bool `json:"paid,omitempty"`
	// Experimental: may fail more often.
	Experimental bool `json:"experimental,omitempty"`
	// Transparent: draws a transparent background and removes one.
	Transparent bool `json:"transparent,omitempty"`
	// Edit: changes a picture too, not only draws a new one (with a model that takes a reference, for one with models).
	Edit bool `json:"edit"`
	// Models are its models, the default first (none: it has no choice of model).
	Models []mediagen.ImageModel `json:"models,omitempty"`
}

// imageProviders lists the generators of this machine: Codex and Antigravity always, Higgsfield when it is connected.
func imageProviders(ctx context.Context, media *mediagen.Service) []imageProvider {
	out := []imageProvider{
		{ID: string(imagegen.Codex), Name: "Codex", Transparent: true, Edit: true},
		{ID: string(imagegen.Antigravity), Name: "Antigravity", Experimental: true, Edit: true},
	}
	if via := media.ImageVia(); via != "" {
		ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		out = append(out, imageProvider{
			ID: string(imagegen.Higgsfield), Name: "Higgsfield", Paid: true,
			// The API key's picture models draw from words only.
			Edit:   via == "mcp",
			Models: media.ImageModels(ctx),
		})
	}
	return out
}

// estimatePictures is the price of a picture request of provider Higgsfield: one picture's, times the count.
func estimatePictures(ctx context.Context, media *mediagen.Service, req imagegen.Request) (map[string]any, error) {
	if req.Provider != imagegen.Higgsfield {
		return nil, fmt.Errorf("%w: only a paid provider has a price", mediagen.ErrBadRequest)
	}
	count := req.Count
	if count <= 0 {
		count = 1
	}
	ref := ""
	if req.Source != "" {
		if req.SourceOK == nil || !req.SourceOK(req.Source) {
			return nil, fmt.Errorf("%w: this picture cannot be changed from here", mediagen.ErrBadRequest)
		}
		ref = req.Source
	}
	ratio := map[imagegen.Shape]string{imagegen.Square: "1:1", imagegen.Landscape: "16:9", imagegen.Portrait: "9:16"}[req.Shape]
	mr := mediaPictures{svc: media}.mediaRequest(imagegen.MediaDraw{Model: req.Model, Prompt: req.Prompt, Ratio: ratio, Reference: ref})
	if mr.Prompt == "" {
		mr.Prompt = "a picture"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p, err := media.Estimate(ctx, mr)
	if err != nil {
		return nil, mediaRefusal(err)
	}
	return map[string]any{
		"usd": p.USD * float64(count), "credits": p.Credits * float64(count), "model": p.Model, "via": p.Via, "count": count,
	}, nil
}
