package mediagen

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

const (
	// The agents' MCP tools.
	ToolGenerate = "generate_video"
	ToolWait     = "wait_video"
	ToolCancel   = "cancel_video"
	ToolAudio    = "generate_audio"
	// WaitMax is how long a call waits for the clip; CodexWaitMax is the most for a Codex caller, which ends a tool call at a minute.
	WaitMax      = 5 * time.Minute
	CodexWaitMax = 55 * time.Second
)

// Tools are the video tools for one calling agent.
type Tools struct {
	S *Service
	// Caller is the calling chat ("" when it could not be told), Why says why not.
	Caller, Why string
	// Cwd is the chat's folder, a relative path is taken from.
	Cwd string
	// Codex is true for a Codex caller.
	Codex bool
	// Allowed says whether a folder may receive the files (inside the workspace).
	Allowed func(path string) bool
	// Readable says whether a frame file may be read.
	Readable func(path string) bool
}

var _ mcpserver.Toolset = Tools{}

func (Tools) Instructions() string {
	return "Video: generate_video makes a clip from frames (an image → video: pass the pictures as frames) or from a prompt alone, with " +
		"Higgsfield (Seedance, Kling, MiniMax…). generate_audio makes music, speech or a sound effect with Higgsfield. Both cost the person's money: " +
		"call them only when they asked for a video, a clip or audio, or the task you work on says to; they answer with the price and refuse a job " +
		"over max_usd (default $2): tell the person the price, do not raise the cap yourself. The chat shows the result as it is made. Higgsfield " +
		"is reached by the person's «Higgsfield (MCP)» connection (their plan or free trial pays) or by an API key; without either the tool says " +
		"how to connect it; never ask the person to paste a key. For the rest of Higgsfield (presets, Marketing Studio, 3D, its models' own " +
		"settings) use the harnessly-hub tools on the higgsfield-mcp connection."
}

func (Tools) Tools() []any {
	models := []string{}
	for _, m := range extproviders.HiggsfieldCatalog {
		models = append(models, m.ID)
	}
	return []any{
		jsvalue.NewObject(
			"name", ToolGenerate,
			"description", "Make a video clip with Higgsfield (Kling, Seedance, LTX): from one or two frames (image → video; the first is the first "+
				"frame, the second the last frame for models that take it) or from the prompt alone (text → video). It SPENDS MONEY from the "+
				"person's Higgsfield balance: call it only when they asked for a video or clip, or the task says to. It first asks the provider "+
				"the price (free); a price over max_usd (default 2) is refused and nothing is sent: then tell the person the price and ask. "+
				"Set estimate_only to get the price alone. The person sees a card in the chat that fills as the clip is made (a minute or "+
				"several). Waits for the clip and answers with its file path; if it takes longer than the call may wait, the answer gives a "+
				"job_id to pass to wait_video. Beside each clip is <clip>.origin.json (provider, model, prompt, source frames, price). "+
				"Through the «Higgsfield (MCP)» connection the models are seedance (default, Seedance 2.5), kling (Kling 3.0: multi-shot, sound), "+
				"minimax (2K keyframes), marketing (ads), or an id of its catalog (harnessly-hub: higgsfield-mcp models_explore), and params "+
				"carries a model's own settings. Through an API key: kling (default, 3–15 s, optional sound), seedance (4–30 s, first+last frame), "+
				"ltx (6/8/10 s, fast and cheapest), or an endpoint ID: "+strings.Join(models, ", ")+". Hand the file over with deliver_file only "+
				"when the person asks for a file.",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject(
					"prompt", jsvalue.NewObject("type", "string", "description", "What happens: motion, camera, mood. Like a director's note, in English works best."),
					"frames", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string"), "maxItems", 2, "description", "Picture files (absolute, or relative to this chat's folder) or https addresses: the first frame, and the last one for models that take it. Omit for text → video."),
					"model", jsvalue.NewObject("type", "string", "description", "seedance, kling, minimax, ltx (API key only), a model id of the MCP's catalog, or an endpoint ID of the API."),
					"provider", jsvalue.NewObject("type", "string", "description", "higgsfield-mcp for the «Higgsfield (MCP)» connection, or an account in Settings → Providers (default: the account that holds a key, else the MCP connection)."),
					"params", jsvalue.NewObject("type", "object", "description", "MCP connection only: the model's own further settings as models_explore lists them."),
					"allowance", jsvalue.NewObject("type", "string", "enum", []any{"trial", "credits"}, "description", "MCP connection only: what pays, as the person said: their free-trial unlimited generations or their credits. Omit until the tool asks."),
					"seconds", jsvalue.NewObject("type", "integer", "description", "Length of the clip; each model takes its own range (default: about 5 s, the shortest the model takes)."),
					"ratio", jsvalue.NewObject("type", "string", "description", "Aspect ratio (16:9, 9:16, 1:1) for models that take it; an image → video clip follows its frame."),
					"resolution", jsvalue.NewObject("type", "string", "description", "720p, 1080p… as the model takes it; the provider's default when omitted."),
					"audio", jsvalue.NewObject("type", "boolean", "description", "Sound of the clip (default false: it costs more)."),
					"max_usd", jsvalue.NewObject("type", "number", "description", "The most this job may cost by the provider's price (default 2). Raise it only when the person agreed to the price."),
					"estimate_only", jsvalue.NewObject("type", "boolean", "description", "Only ask the price: nothing is made, nothing is charged."),
					"save_to", jsvalue.NewObject("type", "string", "description", "A folder to copy the clip and its origin file into: absolute, or relative to this chat's folder. Omit to keep them in harnessly's own store."),
					"name", jsvalue.NewObject("type", "string", "description", "The file's name stem in save_to (default video-<id>)."),
					"origin", jsvalue.NewObject("type", "object", "description", "What to write into the origin file besides the facts of the request: e.g. {scene, camera, outputs} when the frames come from a 3D scene."),
				),
				"required", []any{"prompt"},
			),
			"_meta", alwaysLoad(),
		),
		jsvalue.NewObject(
			"name", ToolAudio,
			"description", "Make music, speech or a sound effect with Higgsfield, through the person's «Higgsfield (MCP)» connection (their plan or "+
				"free trial pays). It SPENDS the person's credits: call it only when they asked for audio, a song, a voice-over or a sound, or "+
				"the task says to. It first asks the price (free); over max_usd (default 2) nothing is sent: tell the person the price and ask. "+
				"Waits for the file and answers with its path (or a job_id for wait_video). Models: speech (default, Seed Audio: say the text "+
				"of prompt; voice picks a preset voice, list_voices of higgsfield-mcp lists them), music (Suno: prompt describes the song, "+
				"seconds 15–360 is required; params {lyrics, style, instrumental}), sfx (a sound effect, seconds required), or an id of the catalog.",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject(
					"prompt", jsvalue.NewObject("type", "string", "description", "The text to speak, the song to make, or the sound."),
					"model", jsvalue.NewObject("type", "string", "description", "speech (default), music, sfx, or a model id of the catalog."),
					"seconds", jsvalue.NewObject("type", "integer", "description", "Length: required for music (15–360) and sfx."),
					"voice", jsvalue.NewObject("type", "string", "description", "Speech: a preset voice id."),
					"params", jsvalue.NewObject("type", "object", "description", "The model's own further settings (models_explore type audio)."),
					"allowance", jsvalue.NewObject("type", "string", "enum", []any{"trial", "credits"}, "description", "What pays, as the person said; omit until the tool asks."),
					"max_usd", jsvalue.NewObject("type", "number", "description", "The most this job may cost (default 2). Raise it only when the person agreed to the price."),
					"estimate_only", jsvalue.NewObject("type", "boolean", "description", "Only ask the price: nothing is made, nothing is charged."),
					"save_to", jsvalue.NewObject("type", "string", "description", "A folder to copy the file and its origin file into: absolute, or relative to this chat's folder."),
					"name", jsvalue.NewObject("type", "string", "description", "The file's name stem in save_to (default audio-<id>)."),
				),
				"required", []any{"prompt"},
			),
			"_meta", alwaysLoad(),
		),
		jsvalue.NewObject(
			"name", ToolWait,
			"description", "Wait for the clip of a generate_video job that was still being made, and get its file path.",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject("job_id", jsvalue.NewObject("type", "string", "description", "The job_id generate_video gave.")),
				"required", []any{"job_id"},
			),
			"_meta", alwaysLoad(),
		),
		jsvalue.NewObject(
			"name", ToolCancel,
			"description", "Cancel a generate_video job that is waiting or queued at the provider (the money reserved is refunded); one that is already being made cannot be stopped.",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject("job_id", jsvalue.NewObject("type", "string", "description", "The job_id generate_video gave.")),
				"required", []any{"job_id"},
			),
			"_meta", alwaysLoad(),
		),
	}
}

// alwaysLoad keeps the tools out of Claude Code's tool search (as imagegen's: a deferred tool had to be found first).
func alwaysLoad() *jsvalue.Object { return jsvalue.NewObject("anthropic/alwaysLoad", true) }

func (Tools) Has(name string) bool {
	return name == ToolGenerate || name == ToolWait || name == ToolCancel || name == ToolAudio
}

func (t Tools) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	if t.S == nil {
		return mcpserver.TextResult("video is not available on this machine", true)
	}
	switch name {
	case ToolWait, ToolCancel:
		id := str(args, "job_id")
		if _, ok := t.S.Get(id); !ok {
			return mcpserver.TextResult("no such job: "+id, true)
		}
		if name == ToolCancel {
			snap, err := t.S.Cancel(ctx, id)
			if err != nil {
				return mcpserver.TextResult("job_id: "+id+"\nFailed: "+err.Error(), true)
			}
			return mcpserver.TextResult(Report(snap), snap.State == Failed)
		}
		return t.wait(ctx, id)
	}
	kind := extproviders.KindVideo
	if name == ToolAudio {
		kind = extproviders.KindAudio
	}
	req, refusal := t.request(args, kind)
	if refusal != "" {
		return mcpserver.TextResult(refusal, true)
	}
	if flag(args, "estimate_only") {
		p, err := t.S.Estimate(ctx, req)
		if err != nil {
			return mcpserver.TextResult(t.explain(err), true)
		}
		if p.Credits > 0 {
			return mcpserver.TextResult(fmt.Sprintf("Estimate: %g credits (≈ $%.2f at most) for %s via %s. Nothing was made or charged. The person's balance: harnessly-hub higgsfield-mcp balance.", p.Credits, p.USD, p.Model, p.Via), false)
		}
		return mcpserver.TextResult(fmt.Sprintf("Estimate: ≈ $%.2f for %s via %s. Nothing was made or charged. The person's Higgsfield balance is in console.higgsfield.ai.", p.USD, p.Model, p.Via), false)
	}
	snap, err := t.S.Start(req)
	if err != nil {
		return mcpserver.TextResult(t.explain(err), true)
	}
	return t.wait(ctx, snap.ID)
}

// Image makes a picture with the media provider: generate_images with a provider or a model of its own.
func (t Tools) Image(ctx context.Context, args *jsvalue.Object) *jsvalue.Object {
	if t.S == nil {
		return mcpserver.TextResult("pictures by a provider are not available on this machine", true)
	}
	req, refusal := t.request(args, extproviders.KindImage)
	if refusal != "" {
		return mcpserver.TextResult(refusal, true)
	}
	switch str(args, "shape") {
	case "square":
		req.Ratio = "1:1"
	case "landscape":
		req.Ratio = "16:9"
	case "portrait":
		req.Ratio = "9:16"
	}
	snap, err := t.S.Start(req)
	if err != nil {
		return mcpserver.TextResult(t.explain(err), true)
	}
	return t.wait(ctx, snap.ID)
}

// request reads the arguments of a call into a request, or says why they are refused.
func (t Tools) request(args *jsvalue.Object, kind extproviders.MediaKind) (Request, string) {
	req := Request{
		Kind: kind, Provider: str(args, "provider"), Model: str(args, "model"), Prompt: str(args, "prompt"),
		Seconds: num(args, "seconds"), Ratio: str(args, "ratio"), Resolution: str(args, "resolution"),
		MaxUSD: fnum(args, "max_usd"), Conv: t.Caller, Name: str(args, "name"), FrameOK: t.Readable,
		Voice: str(args, "voice"), Allowance: str(args, "allowance"),
	}
	if v, ok := args.Get("params"); ok {
		if o, isObj := v.(*jsvalue.Object); isObj {
			req.Params = map[string]any{}
			for _, k := range o.Keys() {
				x, _ := o.Get(k)
				req.Params[k] = plain(x)
			}
		}
	}
	if v, ok := args.Get("audio"); ok {
		if b, isBool := v.(bool); isBool {
			req.Audio = &b
		}
	}
	if v, ok := args.Get("origin"); ok {
		if o, isObj := v.(*jsvalue.Object); isObj {
			req.Origin = map[string]any{}
			for _, k := range o.Keys() {
				x, _ := o.Get(k)
				req.Origin[k] = plain(x)
			}
		}
	}
	if v, ok := args.Get("frames"); ok {
		if list, isList := v.([]any); isList {
			for _, f := range list {
				s, _ := f.(string)
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if !isURL(s) && !filepath.IsAbs(s) {
					if t.Cwd == "" {
						return req, "refused: give frames as absolute paths (the machine could not tell this chat's folder)"
					}
					s = filepath.Join(t.Cwd, s)
				}
				if !isURL(s) {
					s = filepath.Clean(s)
				}
				req.Frames = append(req.Frames, s)
			}
		}
	}
	if to := strings.TrimSpace(str(args, "save_to")); to != "" {
		if !filepath.IsAbs(to) {
			if t.Cwd == "" {
				return req, "refused: give save_to as an absolute path (the machine could not tell this chat's folder)"
			}
			to = filepath.Join(t.Cwd, to)
		}
		to = filepath.Clean(to)
		if t.Allowed != nil && !t.Allowed(to) {
			return req, "refused: save_to must be inside the workspace"
		}
		req.CopyTo, req.SaveOK = to, t.Allowed
	}
	return req, ""
}

// explain is what the agent is told of a refusal: the way out is in the words.
func (t Tools) explain(err error) string {
	switch {
	case errors.Is(err, ErrNoKey):
		return "no key: " + NoKeyMessage
	case errors.Is(err, ErrNoMCP):
		return "not connected: " + NoMCPMessage
	case errors.Is(err, ErrBadRequest):
		return "refused: " + err.Error()
	}
	return "failed: " + err.Error()
}

func (t Tools) wait(ctx context.Context, id string) *jsvalue.Object {
	limit := WaitMax
	if t.Codex {
		limit = CodexWaitMax
	}
	wctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	snap, _ := t.S.Wait(wctx, id)
	return mcpserver.TextResult(Report(snap), snap.State == Failed || snap.State == Canceled)
}

// Report is what the agent is told of a job. Its first line carries the job's id, and «Still drawing», «Failed:», «Done:»
// and the numbered paths are what the chat's card reads (imagecard.ParseReport), the same as for pictures.
func Report(s Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "job_id: %s\n", s.ID)
	what := string(s.Kind)
	switch s.State {
	case Queued, Running:
		fmt.Fprintf(&b, "Still drawing: the %s is being made at %s (%s). Call %s with this job_id to wait for it.\n", what, s.Provider, s.Meta, ToolWait)
	case Failed:
		fmt.Fprintf(&b, "Failed: %s\n", s.Error)
	case Canceled:
		fmt.Fprintf(&b, "Failed: canceled (nothing is charged for a request canceled before it began)\n")
	default:
		fmt.Fprintf(&b, "Done: %d %s(s). %s.\n", len(s.Files), what, s.Meta)
	}
	for _, f := range s.Files {
		p := f.Path
		if f.Copy != "" {
			p = f.Copy
		}
		fmt.Fprintf(&b, "%d. %s\n", f.N, p)
	}
	if len(s.Files) > 0 {
		b.WriteString("The person already sees it in the chat; do not paste the paths back unless asked. The origin of each file is in <file>.origin.json.")
	}
	return strings.TrimRight(b.String(), "\n")
}

func str(o *jsvalue.Object, k string) string {
	v, _ := o.Get(k)
	s, _ := v.(string)
	return s
}

func num(o *jsvalue.Object, k string) int { return int(fnum(o, k)) }

func fnum(o *jsvalue.Object, k string) float64 {
	v, _ := o.Get(k)
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func flag(o *jsvalue.Object, k string) bool {
	v, _ := o.Get(k)
	b, _ := v.(bool)
	return b
}

// plain turns a value of the tool's arguments into one json.Marshal writes.
func plain(v any) any {
	switch x := v.(type) {
	case *jsvalue.Object:
		m := map[string]any{}
		for _, k := range x.Keys() {
			y, _ := x.Get(k)
			m[k] = plain(y)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = plain(y)
		}
		return out
	}
	return v
}
