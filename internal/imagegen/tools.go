package imagegen

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

const (
	// ToolGenerate and ToolWait are the agents' MCP tools.
	ToolGenerate = "generate_images"
	ToolWait     = "wait_images"
	// WaitMax is how long a call waits for the pictures; CodexWaitMax is the most for a Codex caller, which ends a
	// tool call at a minute.
	WaitMax      = 5 * time.Minute
	CodexWaitMax = 55 * time.Second
)

// Tools are the picture tools for one calling agent.
type Tools struct {
	S *Service
	// Caller is the calling chat ("" when it could not be told), Why says why not.
	Caller, Why string
	// Cwd is the chat's folder, a relative save_to is taken from.
	Cwd string
	// Codex is true for a Codex caller.
	Codex bool
	// Allowed says whether a folder may receive the pictures (inside the workspace).
	Allowed func(path string) bool
	// Readable says whether a picture may be changed (source).
	Readable func(path string) bool
	// Media draws a picture with a media provider (Higgsfield) when a call names a provider or a model of its own; nil: the
	// provider and model arguments are refused.
	Media interface {
		Image(ctx context.Context, args *jsvalue.Object) *jsvalue.Object
	}
}

var _ mcpserver.Toolset = Tools{}

func (Tools) Instructions() string {
	return "Pictures: when the person asks to draw, generate or make a picture, photo, poster, icon, logo, cover or illustration " +
		"(«нарисуй», «сгенерируй картинку», «сделай фото / плакат / иконку»), call generate_images at once, in the same turn: it is " +
		"your only way to draw. Do not write code (python, PIL, SVG, canvas) to draw it and do not ask first unless the brief is empty. " +
		"The chat shows the pictures as they are drawn; do not deliver them as files or paste their paths unless the person asks for a file " +
		"(then deliver_file). Ask for several variants with count; save_to copies them into a folder of the project; to change an existing " +
		"picture, pass it as source. The generator is Codex by default; pass provider=antigravity only when the person names Antigravity (experimental, no transparent background)."
}

func (Tools) Tools() []any {
	return []any{
		jsvalue.NewObject(
			"name", ToolGenerate,
			"description", "Draw raster pictures (photos, illustrations, posters, icons, logos, covers) from a brief with Codex's built-in "+
				"image model (gpt-image), or select provider=antigravity for the experimental official Antigravity CLI (no transparent output). This is the way to answer «нарисуй / сгенерируй / сделай картинку, фото, плакат, иконку»: call it in the "+
				"first turn, not code that draws (python, PIL, SVG). The person sees the pictures in the "+
				"chat while they are drawn (a blurred card per picture, then the picture). count asks for that many distinct variants "+
				"(1–8). Waits for the pictures and answers with their file paths; if they take longer than the call may wait, the answer "+
				"gives a job_id to pass to wait_images. Write the brief like for an illustrator: subject, style, composition, colours, "+
				"text if any (quote it exactly). Hand the files over with deliver_file only when the person asks for a file.",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject(
					"provider", jsvalue.NewObject("type", "string", "enum", []any{"codex", "antigravity"}, "description", "Image provider. Omit for the runtime default (normally codex). antigravity is experimental, uses the official agy ACP login and cannot produce transparent backgrounds."),
					"prompt", jsvalue.NewObject("type", "string", "description", "The brief: what to draw and how."),
					"count", jsvalue.NewObject("type", "integer", "minimum", 1, "maximum", MaxCount, "description", "How many variants (default 1)."),
					"shape", jsvalue.NewObject("type", "string", "enum", []any{"square", "landscape", "portrait"}, "description", "The frame (default: the model's choice)."),
					"transparent", jsvalue.NewObject("type", "boolean", "description", "Transparent background (icons, stickers)."),
					"save_to", jsvalue.NewObject("type", "string", "description", "A folder to copy the pictures into: absolute, or relative to this chat's folder. Omit to keep them in harnessly's own store."),
					"name", jsvalue.NewObject("type", "string", "description", "The files' name stem in save_to (default image-<id>); they get -1, -2 … ."),
					"provider", jsvalue.NewObject("type", "string", "description", "Draw with a media provider of Settings → Providers (Higgsfield) instead of Codex: it spends the person's balance there, so only when they asked for it or named it. Omit for the default (Codex, free of charge)."),
					"model", jsvalue.NewObject("type", "string", "description", "With provider: a model id of the provider. Through the «Higgsfield (MCP)» connection: gpt_image_2_6 (default), nano_banana_pro, nano_banana_2_1, seedream_v5_pro, flux_3_image, soul_2, ideogram_4_6, z_image, … (harnessly-hub: higgsfield-mcp models_explore lists them all). Through an API key: soul (default), z-image or an endpoint ID."),
					"source", jsvalue.NewObject("type", "string", "description", "A picture file to change instead of drawing a new one: absolute, or relative to this chat's folder. The prompt then says what to change; the original stays as it is."),
				),
				"required", []any{"prompt"},
			),
			"_meta", alwaysLoad(),
		),
		jsvalue.NewObject(
			"name", ToolWait,
			"description", "Wait for the pictures of a generate_images job that was still drawing, and get their file paths.",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject("job_id", jsvalue.NewObject("type", "string", "description", "The job_id generate_images gave.")),
				"required", []any{"job_id"},
			),
			"_meta", alwaysLoad(),
		),
	}
}

// alwaysLoad keeps the tools out of Claude Code's tool search: behind a non-Anthropic model (ENABLE_TOOL_SEARCH) a deferred
// generate_images had to be found with ToolSearch first, and models drew with python instead or asked again.
func alwaysLoad() *jsvalue.Object { return jsvalue.NewObject("anthropic/alwaysLoad", true) }

func (Tools) Has(name string) bool { return name == ToolGenerate || name == ToolWait }

func (t Tools) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	if t.S == nil {
		return mcpserver.TextResult("pictures are not available on this machine", true)
	}
	if name == ToolWait {
		id := str(args, "job_id")
		if _, ok := t.S.Get(id); !ok {
			return mcpserver.TextResult("no such job: "+id, true)
		}
		return t.wait(ctx, id)
	}
	if prov, model := strings.TrimSpace(str(args, "provider")), strings.TrimSpace(str(args, "model")); prov != "" || model != "" {
		if strings.EqualFold(prov, "codex") && model == "" {
			// Codex is the default: naming it changes nothing.
		} else if t.Media == nil {
			return mcpserver.TextResult("refused: pictures by a provider are not available on this machine", true)
		} else if strings.TrimSpace(str(args, "source")) != "" {
			return mcpserver.TextResult("refused: a picture is changed by Codex only: leave provider and model out when you pass source", true)
		} else {
			if strings.EqualFold(prov, "higgsfield") {
				args.Set("provider", "")
			}
			return t.Media.Image(ctx, args)
		}
	}
	req := Request{
		Provider: Provider(str(args, "provider")),
		Prompt:   str(args, "prompt"), Count: num(args, "count"), Shape: Shape(str(args, "shape")),
		Transparent: flag(args, "transparent"), Conv: t.Caller, Name: str(args, "name"),
	}
	if from := strings.TrimSpace(str(args, "source")); from != "" {
		if !filepath.IsAbs(from) {
			if t.Cwd == "" {
				return mcpserver.TextResult("refused: give source as an absolute path (the machine could not tell this chat's folder)", true)
			}
			from = filepath.Join(t.Cwd, from)
		}
		req.Source, req.Mode, req.SourceOK = filepath.Clean(from), ModeText, t.Readable
	}
	if to := strings.TrimSpace(str(args, "save_to")); to != "" {
		if !filepath.IsAbs(to) {
			if t.Cwd == "" {
				return mcpserver.TextResult("refused: give save_to as an absolute path (the machine could not tell this chat's folder)", true)
			}
			to = filepath.Join(t.Cwd, to)
		}
		to = filepath.Clean(to)
		if t.Allowed != nil && !t.Allowed(to) {
			return mcpserver.TextResult("refused: save_to must be inside the workspace", true)
		}
		req.CopyTo, req.SaveOK = to, t.Allowed
	}
	snap, err := t.start(ctx, req)
	if errors.Is(err, ErrBusy) {
		return mcpserver.TextResult("busy: other pictures of this machine are being drawn and no slot freed up in time. "+
			"Call generate_images again with the same brief in a minute; do not draw it with code instead.", true)
	}
	if err != nil {
		return mcpserver.TextResult("refused: "+err.Error(), true)
	}
	return t.wait(ctx, snap.ID)
}

// BusyPoll is how often a call that found every slot drawing tries again.
var BusyPoll = 2 * time.Second

// start starts the job, waiting for a free slot while the machine draws MaxRunning jobs for others: a busy refusal
// sent agents to retry in a loop or to draw with code. It waits at most half of what the call may wait,
// so the drawing itself still has time.
func (t Tools) start(ctx context.Context, req Request) (Snapshot, error) {
	limit := WaitMax
	if t.Codex {
		limit = CodexWaitMax
	}
	deadline := time.Now().Add(limit / 2)
	for {
		snap, err := t.S.Start(req)
		if !errors.Is(err, ErrBusy) || time.Now().Add(BusyPoll).After(deadline) {
			return snap, err
		}
		select {
		case <-ctx.Done():
			return snap, err
		case <-time.After(BusyPoll):
		}
	}
}

func (t Tools) wait(ctx context.Context, id string) *jsvalue.Object {
	limit := WaitMax
	if t.Codex {
		limit = CodexWaitMax
	}
	wctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	snap, _ := t.S.Wait(wctx, id)
	return mcpserver.TextResult(Report(snap), snap.State == Failed)
}

// Report is what the agent is told of a job. Its first line carries the job's id, the chat's card finds the job by.
func Report(s Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "job_id: %s\n", s.ID)
	switch s.State {
	case Running:
		fmt.Fprintf(&b, "Still drawing: %d of %d ready. Call %s with this job_id to wait for the rest.\n", len(s.Images), s.Count, ToolWait)
	case Failed:
		fmt.Fprintf(&b, "Failed: %s\n", s.Error)
	default:
		fmt.Fprintf(&b, "Done: %d picture(s).\n", len(s.Images))
		if s.Error != "" {
			b.WriteString(s.Error + "\n")
		}
	}
	for _, im := range s.Images {
		p := im.Path
		if im.Copy != "" {
			p = im.Copy
		}
		fmt.Fprintf(&b, "%d. %s\n", im.N, p)
	}
	if len(s.Images) > 0 {
		b.WriteString("The person already sees these pictures in the chat; do not paste the paths back unless asked.")
	}
	return strings.TrimRight(b.String(), "\n")
}

func str(o *jsvalue.Object, k string) string {
	v, _ := o.Get(k)
	s, _ := v.(string)
	return s
}

func num(o *jsvalue.Object, k string) int {
	v, _ := o.Get(k)
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func flag(o *jsvalue.Object, k string) bool {
	v, _ := o.Get(k)
	b, _ := v.(bool)
	return b
}
