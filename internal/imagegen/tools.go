package imagegen

import (
	"context"
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
}

var _ mcpserver.Toolset = Tools{}

func (Tools) Instructions() string {
	return "Pictures: generate_images draws raster images (illustrations, photos, icons, covers) with Codex's image model; the " +
		"chat shows them as they are drawn. Ask for several variants with count. The answer lists the files; save_to copies them " +
		"into a folder of the project. To change an existing picture, pass it as source."
}

func (Tools) Tools() []any {
	return []any{
		jsvalue.NewObject(
			"name", ToolGenerate,
			"description", "Draw raster pictures from a brief with Codex's built-in image model (gpt-image). The person sees them in the "+
				"chat while they are drawn (a blurred card per picture, then the picture). count asks for that many distinct variants "+
				"(1–8). Waits for the pictures and answers with their file paths; if they take longer than the call may wait, the answer "+
				"gives a job_id to pass to wait_images. Write the brief like for an illustrator: subject, style, composition, colours, "+
				"text if any (quote it exactly).",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject(
					"prompt", jsvalue.NewObject("type", "string", "description", "The brief: what to draw and how."),
					"count", jsvalue.NewObject("type", "integer", "minimum", 1, "maximum", MaxCount, "description", "How many variants (default 1)."),
					"shape", jsvalue.NewObject("type", "string", "enum", []any{"square", "landscape", "portrait"}, "description", "The frame (default: the model's choice)."),
					"transparent", jsvalue.NewObject("type", "boolean", "description", "Transparent background (icons, stickers)."),
					"save_to", jsvalue.NewObject("type", "string", "description", "A folder to copy the pictures into: absolute, or relative to this chat's folder. Omit to keep them in harnessly's own store."),
					"name", jsvalue.NewObject("type", "string", "description", "The files' name stem in save_to (default image-<id>); they get -1, -2 … ."),
					"source", jsvalue.NewObject("type", "string", "description", "A picture file to change instead of drawing a new one: absolute, or relative to this chat's folder. The prompt then says what to change; the original stays as it is."),
				),
				"required", []any{"prompt"},
			),
		),
		jsvalue.NewObject(
			"name", ToolWait,
			"description", "Wait for the pictures of a generate_images job that was still drawing, and get their file paths.",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject("job_id", jsvalue.NewObject("type", "string", "description", "The job_id generate_images gave.")),
				"required", []any{"job_id"},
			),
		),
	}
}

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
	req := Request{
		Prompt: str(args, "prompt"), Count: num(args, "count"), Shape: Shape(str(args, "shape")),
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
	snap, err := t.S.Start(req)
	if err != nil {
		return mcpserver.TextResult("refused: "+err.Error(), true)
	}
	return t.wait(ctx, snap.ID)
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
