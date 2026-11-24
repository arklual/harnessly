package mediagen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/extproviders"
)

// Higgsfield MCP 2.0 (mcp.higgsfield.ai/mcp), reached through the person's connection in Settings → Connections: the
// person's subscription or free trial pays, no API key is needed, and it makes music, speech and sound effects besides
// pictures and clips. Its tools: estimate_<kind>_cost and generate_<kind> take {params: {model, prompt, duration,
// aspect_ratio, medias: [{value: media_id, role}], use_unlim, …}}; job_status {jobId}; media_upload → a presigned PUT →
// media_confirm, and media_import_url for an address. It counts in credits and has no cancel for a generation.

// MCPConnID is the catalog id of the Higgsfield MCP connection.
const MCPConnID = "higgsfield-mcp"

// USDPerCredit is what one Higgsfield credit costs at most (the Starter plan, ≈ $0.07; bigger plans pay less): the
// caps in USD hold for every plan.
const USDPerCredit = 0.07

// MCPConn is the person's Higgsfield MCP connection.
type MCPConn interface {
	// Connected says whether the connection is there and on.
	Connected() bool
	// Call calls one of its tools and answers the tools/call result as it came.
	Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error)
}

// ErrNeedsAllowance: the person holds free-trial generations that cover the model, and has not said what pays.
var ErrNeedsAllowance = errors.New("needs the person's choice of what pays")

// AllowanceMessage is what the agent is told when Higgsfield asks what pays.
const AllowanceMessage = "The person has free-trial unlimited generations on Higgsfield that cover this model. Nothing was sent. Ask " +
	"them whether to spend the trial (allowance: \"trial\") or their credits (allowance: \"credits\"), and run it again with their answer."

// NoPlanMessage is what the agent is told when Higgsfield's MCP refuses the account itself.
const NoPlanMessage = "Higgsfield's MCP refused the account: it has no active plan or free trial for MCP (the connection itself is " +
	"signed in). The person starts one at https://higgsfield.ai/mcp (Higgsfield MCP 2.0 has a free 72-hour trial), then it works " +
	"without any key."

const mcpContext = "A generation the person asked for in a harnessly chat."

var mcpModelID = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,80}$`)

// mcpModel maps a request's model (a family, a word or an id of the MCP's catalog) to the MCP's model id.
func mcpModel(kind extproviders.MediaKind, name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	alias := map[extproviders.MediaKind]map[string]string{
		extproviders.KindVideo: {"": "seedance_2_5", "seedance": "seedance_2_5", "kling": "kling3_0", "minimax": "minimax_h3",
			"marketing": "marketing_studio_video"},
		extproviders.KindImage: {"": "gpt_image_2_6", "gpt": "gpt_image_2_6", "gpt-image": "gpt_image_2_6", "soul": "soul_2",
			"marketing": "marketing_studio_image"},
		extproviders.KindAudio: {"": "seed_audio", "speech": "seed_audio", "voice": "seed_audio", "tts": "seed_audio",
			"music": "suno_music", "suno": "suno_music", "sfx": "mirelo_text_to_audio", "sound": "mirelo_text_to_audio"},
	}
	if id, ok := alias[kind][n]; ok {
		return id, nil
	}
	if strings.Contains(n, "/") || n == "ltx" {
		return "", fmt.Errorf("%w: %q is a model of Higgsfield's API key, not of its MCP connection: use seedance, kling, minimax, or an id models_explore lists", ErrBadRequest, name)
	}
	if !mcpModelID.MatchString(n) {
		return "", fmt.Errorf("%w: no model %q", ErrBadRequest, name)
	}
	return n, nil
}

// makeMCPPlan turns a request into the params of the MCP's generate_<kind>. The frames are not in the body: submit adds
// them as medias once they have media ids.
func makeMCPPlan(r Request) (plan, error) {
	id, err := mcpModel(r.Kind, r.Model)
	if err != nil {
		return plan{}, err
	}
	if r.Prompt == "" && (r.Kind == extproviders.KindAudio || len(r.Frames) == 0) {
		return plan{}, fmt.Errorf("%w: the prompt is empty", ErrBadRequest)
	}
	if r.Kind == extproviders.KindAudio && len(r.Frames) > 0 {
		return plan{}, fmt.Errorf("%w: audio is made from the prompt only", ErrBadRequest)
	}
	body := map[string]any{}
	for k, v := range r.Params {
		body[k] = v
	}
	body["model"] = id
	if r.Prompt != "" {
		body["prompt"] = r.Prompt
	}
	if r.Kind != extproviders.KindAudio {
		body["count"] = 1
	}
	if r.Seconds > 0 {
		body["duration"] = r.Seconds
	}
	if r.Ratio != "" {
		body["aspect_ratio"] = r.Ratio
	}
	if r.Resolution != "" {
		body["resolution"] = r.Resolution
	}
	if r.Voice != "" {
		body["voice_type"], body["voice_id"] = "preset", r.Voice
	}
	switch r.Allowance {
	case "trial":
		body["use_unlim"] = true
	case "credits":
		body["use_unlim"] = false
	}
	m := extproviders.MediaModel{ID: id, Label: id, Kind: r.Kind, FromImage: len(r.Frames) > 0}
	return plan{req: r, model: m, endpoint: id, body: body}, nil
}

type hfMCP struct {
	call      func(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error)
	http      *http.Client
	allowHTTP bool
}

// do calls a tool and gives its data (the structured content, else the text read as JSON, else nil) and its text.
func (m *hfMCP) do(ctx context.Context, tool string, args map[string]any, what string) (any, string, error) {
	raw, err := m.call(ctx, tool, args)
	if err != nil {
		return nil, "", mcpError(err.Error(), what)
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, "", fmt.Errorf("%w: %s: Higgsfield's answer is not readable", errFatal, what)
	}
	var texts []string
	for _, c := range res.Content {
		if c.Type == "text" && c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	text := strings.Join(texts, "\n")
	if res.IsError {
		return nil, text, mcpError(text, what)
	}
	var data any
	if len(res.Structured) > 0 && string(res.Structured) != "null" {
		_ = json.Unmarshal(res.Structured, &data)
	}
	if data == nil {
		for _, t := range texts {
			var v any
			if json.Unmarshal([]byte(strings.TrimSpace(t)), &v) == nil {
				data = v
				break
			}
		}
	}
	return data, text, nil
}

// mcpError says what the MCP answered, in words for whoever asked; a refusal that asking again does not mend is fatal.
func mcpError(text, what string) error {
	t := strings.TrimSpace(text)
	if len(t) > 400 {
		t = t[:400] + "…"
	}
	low := strings.ToLower(t)
	switch {
	case strings.Contains(low, "/workspaces") && strings.Contains(low, "403"),
		strings.Contains(low, "subscription") && (strings.Contains(low, "required") || strings.Contains(low, "active")):
		return fmt.Errorf("%w: %s: %s (%s)", errFatal, what, NoPlanMessage, t)
	case strings.Contains(low, "not enough credits"), strings.Contains(low, "insufficient"):
		return fmt.Errorf("%w: %s: Higgsfield refused: the account has too few credits (the person tops up at higgsfield.ai) — %s", errFatal, what, t)
	case strings.Contains(low, "turned off"), strings.Contains(low, "not found"), strings.Contains(low, "validation"),
		strings.Contains(low, "invalid"), strings.Contains(low, "unauthorized"), strings.Contains(low, "401"):
		return fmt.Errorf("%w: %s: Higgsfield refused — %s", errFatal, what, t)
	}
	return fmt.Errorf("%s: Higgsfield's MCP failed — %s", what, t)
}

func (m *hfMCP) estimate(ctx context.Context, pl plan) (float64, float64, error) {
	params := cloneMap(pl.body)
	// The MCP takes medias only as an array: a null (a request without frames) fails its validation as "params: Invalid input".
	if medias := standInMedias(pl); len(medias) > 0 {
		params["medias"] = medias
	}
	data, text, err := m.do(ctx, "estimate_"+string(pl.req.Kind)+"_cost", map[string]any{"params": params, "context": mcpContext}, "the price of the request")
	if err != nil {
		return 0, 0, err
	}
	credits, ok := findNum(data, "credits", "total_credits", "credit_cost", "estimated_credits", "cost", "price")
	if !ok {
		if mt := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*credits?`).FindStringSubmatch(text); mt != nil {
			credits, _ = strconv.ParseFloat(mt[1], 64)
			ok = true
		}
	}
	if !ok || credits < 0 {
		return 0, 0, fmt.Errorf("%w: Higgsfield's price is not readable", errFatal)
	}
	usd := credits * USDPerCredit
	if pl.req.Allowance == "trial" {
		usd = 0
	}
	return usd, credits, nil
}

// standInMedias holds the frames' places for a price: a frame not uploaded yet has no media id, and a price does not
// depend on which picture it is.
func standInMedias(pl plan) []any {
	if len(pl.req.Frames) == 0 {
		return nil
	}
	out := []any{}
	for i := range pl.req.Frames {
		out = append(out, map[string]any{"value": "00000000-0000-0000-0000-000000000000", "role": frameRole(i, nil)})
	}
	return out
}

// frameRole is the role of the i-th frame by the model's roles (nil: not known): the first frame, then the last one.
func frameRole(i int, roles []string) string {
	want, fallback := []string{"start", "first"}, "image"
	if i > 0 {
		want, fallback = []string{"end", "last"}, "end_image"
	}
	for _, r := range roles {
		for _, w := range want {
			if strings.Contains(strings.ToLower(r), w) {
				return r
			}
		}
	}
	if i == 0 && len(roles) > 0 {
		for _, r := range roles {
			if strings.Contains(strings.ToLower(r), "image") {
				return r
			}
		}
	}
	return fallback
}

// roles are the media roles of a model, by models_explore; nil when they cannot be read.
func (m *hfMCP) roles(ctx context.Context, model string) []string {
	data, _, err := m.do(ctx, "models_explore", map[string]any{"action": "get", "model_id": model}, "the model")
	if err != nil {
		return nil
	}
	var out []string
	walk(data, "", func(path string, key string, v any) {
		if key != "roles" {
			return
		}
		if list, ok := v.([]any); ok {
			for _, x := range list {
				switch r := x.(type) {
				case string:
					out = append(out, r)
				case map[string]any:
					if n, ok := r["role"].(string); ok {
						out = append(out, n)
					} else if n, ok := r["name"].(string); ok {
						out = append(out, n)
					}
				}
			}
		}
	})
	return out
}

func (m *hfMCP) ref(ctx context.Context, frame string) (string, error) {
	if isURL(frame) {
		data, text, err := m.do(ctx, "media_import_url", map[string]any{"url": frame, "type": "image"}, "importing the frame")
		if err != nil {
			return "", err
		}
		if id := findID(data, text, "media_id", "id"); id != "" {
			return id, nil
		}
		return "", errors.New("Higgsfield imported the frame but gave no media id")
	}
	ct := frameExt[strings.ToLower(filepath.Ext(frame))]
	if ct == "" {
		return "", errors.New("a frame is png, jpg, webp or gif")
	}
	body, err := os.ReadFile(frame)
	if err != nil {
		return "", err
	}
	data, text, err := m.do(ctx, "media_upload", map[string]any{"filename": filepath.Base(frame), "content_type": ct}, "the upload address")
	if err != nil {
		return "", err
	}
	put, _ := findStr(data, "upload_url")
	id := findID(data, text, "media_id", "id")
	if put == "" || id == "" {
		return "", errors.New("Higgsfield's upload answer is not readable")
	}
	u, err := url.Parse(put)
	if err != nil || (u.Scheme != "https" && !m.allowHTTP) {
		return "", errors.New("Higgsfield's upload address is not https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, put, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	walk(data, "", func(_ string, key string, v any) {
		if key == "upload_headers" || key == "headers" {
			if h, ok := v.(map[string]any); ok {
				for k, x := range h {
					if s, ok := x.(string); ok {
						req.Header.Set(k, s)
					}
				}
			}
		}
	})
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", ct)
	}
	res, err := m.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("the storage refused the frame: HTTP %d", res.StatusCode)
	}
	if _, _, err := m.do(ctx, "media_confirm", map[string]any{"type": "image", "media_id": id}, "confirming the frame"); err != nil {
		return "", err
	}
	return id, nil
}

// submit sends the generation once: on a lost answer the MCP may have started it, so it is not sent again.
func (m *hfMCP) submit(ctx context.Context, pl plan, refs []string, _ string) (string, error) {
	params := cloneMap(pl.body)
	if len(refs) > 0 {
		roles := m.roles(ctx, pl.endpoint)
		medias := []any{}
		for i, r := range refs {
			medias = append(medias, map[string]any{"value": r, "role": frameRole(i, roles)})
		}
		params["medias"] = medias
	}
	data, text, err := m.do(ctx, "generate_"+string(pl.req.Kind), map[string]any{"params": params, "context": mcpContext}, "the request")
	if err != nil {
		return "", err
	}
	if hasKey(data, "unlim_choice") || strings.Contains(text, "unlim_choice") {
		return "", fmt.Errorf("%w: %w: %s", errFatal, ErrNeedsAllowance, AllowanceMessage)
	}
	id := findID(data, text, "job_id", "jobId", "job_ids", "jobs", "id")
	if id == "" {
		return "", fmt.Errorf("%w: Higgsfield answered without a job id", errFatal)
	}
	return id, nil
}

func (m *hfMCP) status(ctx context.Context, rid string) (hfStatus, error) {
	data, _, err := m.do(ctx, "job_status", map[string]any{"jobId": rid}, "the status of the request")
	if err != nil {
		return hfStatus{}, err
	}
	raw, _ := findStr(data, "status")
	st := hfStatus{Status: mcpStatus(raw)}
	switch st.Status {
	case "completed":
		st.URLs = resultURLs(data, m.allowHTTP)
	case "failed", "nsfw":
		st.Error, _ = findStr(data, "error", "error_message", "failure_reason", "message")
		if strings.EqualFold(raw, "ip_detected") {
			st.Error = "Higgsfield held the result back as possibly protected content (ip_detected): the person may confirm their rights in Higgsfield"
		}
	}
	return st, nil
}

func (m *hfMCP) cancel(context.Context, string) error {
	return errors.New("Higgsfield's MCP cannot cancel a generation it has taken: it will end by itself")
}

// mcpStatus maps the MCP's words to the REST API's: completed, failed, nsfw, canceled, or the word itself while it runs.
func mcpStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "completed", "complete", "succeeded", "success", "done", "finished":
		return "completed"
	case "failed", "error", "errored":
		return "failed"
	case "nsfw", "moderated", "ip_detected", "rejected":
		return "nsfw"
	case "canceled", "cancelled":
		return "canceled"
	case "":
		return "in_progress"
	}
	return strings.ToLower(s)
}

var mediaExt = map[string]bool{".mp4": true, ".webm": true, ".mov": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
	".gif": true, ".svg": true, ".mp3": true, ".wav": true, ".m4a": true, ".ogg": true, ".flac": true, ".aac": true, ".glb": true}

// resultURLs are the files of a finished job: https addresses of media, not of its inputs, thumbnails or previews.
func resultURLs(data any, allowHTTP bool) []string {
	var withExt, byKey []string
	seen := map[string]bool{}
	walk(data, "", func(path, key string, v any) {
		s, ok := v.(string)
		if !ok || !(strings.HasPrefix(s, "https://") || (allowHTTP && strings.HasPrefix(s, "http://"))) || seen[s] {
			return
		}
		p := strings.ToLower(path + "." + key)
		// minUrl is the small webp copy of a picture Higgsfield gives beside its rawUrl (seen live).
		for _, skip := range []string{"input", "medias", "reference", "thumb", "preview", "poster", "avatar", "cover", "upload", "minurl", "min_url"} {
			if strings.Contains(p, skip) {
				return
			}
		}
		seen[s] = true
		u, err := url.Parse(s)
		if err == nil && mediaExt[strings.ToLower(filepath.Ext(u.Path))] {
			withExt = append(withExt, s)
		} else if strings.Contains(strings.ToLower(key), "url") || key == "raw" {
			byKey = append(byKey, s)
		}
	})
	if len(withExt) > 0 {
		return withExt
	}
	return byKey
}

// walk visits every value of decoded JSON, with the path of its parent and its own key ("" in a list).
func walk(v any, path string, fn func(path, key string, v any)) {
	switch x := v.(type) {
	case map[string]any:
		for k, y := range x {
			fn(path, k, y)
			walk(y, path+"."+k, fn)
		}
	case []any:
		for _, y := range x {
			fn(path, "", y)
			walk(y, path+"[]", fn)
		}
	}
}

func hasKey(data any, key string) bool {
	found := false
	walk(data, "", func(_ string, k string, v any) {
		if k == key && v != nil {
			found = true
		}
	})
	return found
}

// findStr is the first string under one of the keys, by the order of the keys.
func findStr(data any, keys ...string) (string, bool) {
	for _, key := range keys {
		out := ""
		walk(data, "", func(_ string, k string, v any) {
			if s, ok := v.(string); ok && k == key && out == "" && s != "" {
				out = s
			}
		})
		if out != "" {
			return out, true
		}
	}
	return "", false
}

func findNum(data any, keys ...string) (float64, bool) {
	for _, key := range keys {
		var out float64
		found := false
		walk(data, "", func(_ string, k string, v any) {
			if k != key || found {
				return
			}
			switch n := v.(type) {
			case float64:
				out, found = n, true
			case string:
				if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
					out, found = f, true
				}
			}
		})
		if found {
			return out, true
		}
	}
	return 0, false
}

var uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// findID is the first id under one of the keys (a string, a list of strings, or a list of objects with an id), else the
// first uuid of the text.
func findID(data any, text string, keys ...string) string {
	for _, key := range keys {
		out := ""
		walk(data, "", func(_ string, k string, v any) {
			if k != key || out != "" {
				return
			}
			switch x := v.(type) {
			case string:
				out = x
			case []any:
				for _, y := range x {
					if s, ok := y.(string); ok && s != "" {
						out = s
						return
					}
					if o, ok := y.(map[string]any); ok {
						for _, n := range []string{"id", "job_id"} {
							if s, ok := o[n].(string); ok && s != "" {
								out = s
								return
							}
						}
					}
				}
			}
		})
		if out != "" {
			return out
		}
	}
	return uuidRe.FindString(text)
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}
