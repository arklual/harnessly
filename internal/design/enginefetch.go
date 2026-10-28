package design

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Fetching the board engine with the owner's own account. The engine and the format's rules are published by the
// design tool's vendor as an artifact type on claude.ai; they are not ours to redistribute, so the runtime never ships
// them. Instead the person's own Claude Code, logged in to their own account, reads them: one non-interactive session
// on a small model whose only tool is Artifact, kept to reading by a PreToolUse hook (EngineHook), first lists the
// type's current release and files, then saves them from a canvas of the person's (an empty artifact made from the
// type, created once on their account and remembered) into a staging folder, byte for byte, with the checksums the
// tool reports. The files are checked, kept as a release of the engine folder (a releases mirror, releases.go) and
// listed in its releases.json, so projects pin and upgrade releases as they do with any mirror.

const (
	// DefaultEngineTypeURL is the public address of the design artifact type, the same for every account.
	DefaultEngineTypeURL = "https://claude.ai/artifact/QKN21svewxgyPb6SYRqWnd"
	// engineCanvasFile remembers the person's canvas in the engine folder.
	engineCanvasFile = "canvas.json"
	// EngineHookCreateEnv lets the hook allow the one call that makes the canvas.
	EngineHookCreateEnv = "HARNESSLY_ENGINE_HOOK_CREATE"
)

// engineExclude are the editor's own bundles on a canvas: not the engine or its rules, never fetched.
var engineExclude = regexp.MustCompile(`^(index\.html|artifact-type/app\.js|artifact-type/app\.css|artifact-type/fast-edit-worker\.js)$`)

// engineFileRe: a file of the type as a relative path of plain segments.
var engineFileRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

// FetchOptions say how to fetch.
type FetchOptions struct {
	// Dir is the engine folder (DefaultEngineDir of the data dir).
	Dir string
	// Claude is the Claude Code binary ("claude" on PATH when empty); Model its model ("haiku" when empty).
	Claude, Model string
	// TypeURL is the artifact type (DefaultEngineTypeURL when empty); Canvas a canvas of it to read from (the remembered
	// one, else a new one made on the account, when empty).
	TypeURL, Canvas string
	// Hook is the command the session's PreToolUse hook runs (`<cli> design engine hook`).
	Hook string
	// Budget caps one session's spend in USD ("0.50" when empty).
	Budget string
	// Log hears the steps.
	Log func(string)
	// session runs one session and returns its last message (tests).
	session func(ctx context.Context, prompt string, env []string) (string, error)
}

// FetchResult is what was fetched.
type FetchResult struct {
	Release string `json:"release"`
	New     bool   `json:"new"`
	Engine  string `json:"engine"`
	Files   int    `json:"files"`
}

// FetchEngine fetches the type's current release into the engine folder, unless it is there already.
func FetchEngine(ctx context.Context, o FetchOptions) (FetchResult, error) {
	if o.Dir == "" {
		return FetchResult{}, errors.New("no engine folder")
	}
	if o.TypeURL == "" {
		o.TypeURL = DefaultEngineTypeURL
	}
	if o.Model == "" {
		o.Model = "haiku"
	}
	if o.Budget == "" {
		o.Budget = "0.50"
	}
	if o.Log == nil {
		o.Log = func(string) {}
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return FetchResult{}, err
	}
	work, err := os.MkdirTemp("", "design-engine-")
	if err != nil {
		return FetchResult{}, err
	}
	defer os.RemoveAll(work)
	if o.session == nil {
		if o.Claude == "" {
			o.Claude = "claude"
		}
		bin, err := exec.LookPath(o.Claude)
		if err != nil {
			return FetchResult{}, fmt.Errorf("Claude Code is not installed here (%s): install it and log in with your account first", o.Claude)
		}
		if o.Hook == "" {
			return FetchResult{}, errors.New("no hook command")
		}
		settings := filepath.Join(work, "settings.json")
		hooks := map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "*",
			"hooks": []any{map[string]any{"type": "command", "command": o.Hook}}}}}}
		raw, _ := json.Marshal(hooks)
		if err := os.WriteFile(settings, raw, 0o600); err != nil {
			return FetchResult{}, err
		}
		o.session = claudeSession(bin, o.Model, o.Budget, settings, work)
	}

	o.Log("Reading the current release of the design type with your Claude account")
	release, files, err := engineCheck(ctx, o)
	if err != nil {
		return FetchResult{}, err
	}
	dest := filepath.Join(o.Dir, "upstream", releaseKind, release)
	if have, ok := releaseEntry(o.Dir, release); ok {
		o.Log("Release " + release + " is already here")
		return FetchResult{Release: release, Engine: have, Files: len(files)}, nil
	}

	canvas := o.Canvas
	if canvas == "" {
		canvas = readCanvas(o.Dir)
	}
	if canvas == "" {
		o.Log("Making a private canvas of the design type on your account (once)")
		if canvas, err = engineCreateCanvas(ctx, o); err != nil {
			return FetchResult{}, err
		}
		writeCanvas(o.Dir, canvas)
	}

	o.Log(fmt.Sprintf("Downloading release %s (%d files)", release, len(files)))
	staging := filepath.Join(work, "staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return FetchResult{}, err
	}
	sums, version, err := engineDownload(ctx, o, canvas, staging, files)
	if err != nil {
		return FetchResult{}, err
	}
	if after, _, err := engineCheck(ctx, o); err != nil {
		return FetchResult{}, err
	} else if after != release {
		return FetchResult{}, fmt.Errorf("the design type moved from %s to %s during the download; run the command again", release, after)
	}

	engine := ""
	for _, f := range []string{"artifact-type/dc-runtime.js", "dc-runtime.js", EngineFile} {
		if _, ok := sums[f]; ok {
			engine = f
			break
		}
	}
	if engine == "" {
		return FetchResult{}, errors.New("the release has no board engine (dc-runtime.js)")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return FetchResult{}, err
	}
	if err := os.Rename(staging, dest); err != nil {
		if err := copyEngineTree(staging, dest); err != nil {
			return FetchResult{}, err
		}
	}
	source := map[string]any{"type": releaseKind, "release": release, "canvasVersion": version, "typeUrl": o.TypeURL,
		"fetchedAt": time.Now().UTC().Format(time.RFC3339), "files": sums}
	if raw, err := json.MarshalIndent(source, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dest, "SOURCE.json"), append(raw, '\n'), 0o644)
	}
	rel := filepath.ToSlash(filepath.Join("upstream", releaseKind, release, engine))
	if err := addRelease(o.Dir, release, rel, sums[engine]); err != nil {
		return FetchResult{}, err
	}
	o.Log("Release " + release + " is ready")
	return FetchResult{Release: release, New: true, Engine: rel, Files: len(sums)}, nil
}

// claudeSession runs one non-interactive Claude Code session whose only tool is Artifact.
func claudeSession(bin, model, budget, settings, dir string) func(context.Context, string, []string) (string, error) {
	return func(ctx context.Context, prompt string, env []string) (string, error) {
		cmd := exec.CommandContext(ctx, bin, "-p", "--model", model, "--tools", "Artifact", "--permission-mode", "dontAsk",
			"--setting-sources", "project", "--settings", settings, "--strict-mcp-config", "--no-session-persistence",
			"--max-budget-usd", budget, "--output-format", "json", prompt)
		cmd.Dir = dir
		cmd.Env = append(append(os.Environ(), "CLAUDE_CODE_ARTIFACT=1"), env...)
		cmd.Stdin = nil
		out, err := cmd.Output()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			var ee *exec.ExitError
			if errors.As(err, &ee) && len(ee.Stderr) > 0 {
				msg += " " + strings.TrimSpace(string(ee.Stderr))
			}
			return "", fmt.Errorf("the Claude Code session failed (is it logged in to a claude.ai account?): %s", clip(msg, 400))
		}
		var res struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return "", fmt.Errorf("the Claude Code session answered something else than JSON: %s", clip(string(out), 300))
		}
		return res.Result, nil
	}
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// lines are the answer's lines that start with tag, split into fields after it.
func lines(answer, tag string) [][]string {
	var out [][]string
	sc := bufio.NewScanner(strings.NewReader(answer))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) > 0 && f[0] == tag {
			out = append(out, f[1:])
		}
	}
	return out
}

func answerError(answer string) error {
	if e := lines(answer, "ERROR"); len(e) > 0 {
		return fmt.Errorf("the Artifact tool failed: %s", strings.Join(e[0], " "))
	}
	return fmt.Errorf("unexpected answer: %s", clip(answer, 300))
}

// engineCheck is the type's current release and its files, the editor's bundles left out.
func engineCheck(ctx context.Context, o FetchOptions) (string, []string, error) {
	answer, err := o.session(ctx, `Call the Artifact tool exactly once with {"action":"read","type_url":"`+o.TypeURL+`"}. Then reply with plain lines only, no other text: first "RELEASE <the release id named in the result's first line>", then one line "FILE <name>" for each file name in the result's Files list, in order. If the tool is missing or fails, reply with one line "ERROR <reason>".`, nil)
	if err != nil {
		return "", nil, err
	}
	rel := lines(answer, "RELEASE")
	if len(rel) == 0 || len(rel[0]) != 1 || !IsReleaseID(rel[0][0]) {
		return "", nil, answerError(answer)
	}
	var files []string
	for _, f := range lines(answer, "FILE") {
		if len(f) == 1 && engineFileRe.MatchString(f[0]) && !strings.Contains(f[0], "..") && !engineExclude.MatchString(f[0]) {
			files = append(files, f[0])
		}
	}
	if len(files) == 0 {
		return "", nil, errors.New("the design type lists no files")
	}
	return rel[0][0], files, nil
}

var canvasURLRe = regexp.MustCompile(`^https://claude\.ai/(code/)?artifact/[A-Za-z0-9-]+$`)

// engineCreateCanvas makes an empty private artifact from the type on the person's account: the place the type's files
// are read from. The hook allows this one publish only in this session.
func engineCreateCanvas(ctx context.Context, o FetchOptions) (string, error) {
	answer, err := o.session(ctx, `Call the Artifact tool exactly once with {"action":"publish","type_url":"`+o.TypeURL+`","title":"Design engine (do not delete)"}. Do not write any file and call nothing else. Then reply with one line "CANVAS <the url of the new artifact>". If the tool fails, reply with one line "ERROR <reason>".`,
		[]string{EngineHookCreateEnv + "=1"})
	if err != nil {
		return "", err
	}
	c := lines(answer, "CANVAS")
	if len(c) == 0 || len(c[0]) != 1 || !canvasURLRe.MatchString(c[0][0]) {
		return "", answerError(answer)
	}
	return c[0][0], nil
}

// engineDownload saves the files from the canvas into staging and checks each against the checksum the tool reported:
// the model only relays, the bytes are the tool's.
func engineDownload(ctx context.Context, o FetchOptions, canvas, staging string, files []string) (map[string]string, string, error) {
	paths, _ := json.Marshal(files)
	answer, err := o.session(ctx, `Call the Artifact tool exactly once with {"action":"read","url":"`+canvas+`","out_dir":"`+staging+`","paths":`+string(paths)+`}. Do not open or read the saved files and call nothing else. Then reply with plain lines only: first "VERSION <the version named in the result>", then one line "FILE <path> <bytes> <sha256>" for each saved file as the result reports it. If the tool fails, reply with one line "ERROR <reason>".`, nil)
	if err != nil {
		return nil, "", err
	}
	v := lines(answer, "VERSION")
	if len(v) == 0 || len(v[0]) != 1 {
		return nil, "", answerError(answer)
	}
	reported := map[string]string{}
	for _, f := range lines(answer, "FILE") {
		if len(f) == 3 {
			reported[f[0]] = strings.ToLower(f[2])
		}
	}
	sums := map[string]string{}
	for _, p := range files {
		file := filepath.Join(staging, filepath.FromSlash(p))
		got, err := sha256File(file)
		if err != nil {
			return nil, "", fmt.Errorf("%s was not saved", p)
		}
		if reported[p] != got {
			return nil, "", fmt.Errorf("%s: sha256 %s, the tool reported %q", p, got, reported[p])
		}
		sums[p] = got
	}
	return sums, v[0][0], nil
}

func sha256File(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyEngineTree(from, to string) error {
	return filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

func readCanvas(dir string) string {
	var c struct {
		URL string `json:"url"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, engineCanvasFile))
	if err != nil || json.Unmarshal(raw, &c) != nil || !canvasURLRe.MatchString(c.URL) {
		return ""
	}
	return c.URL
}

func writeCanvas(dir, url string) {
	raw, _ := json.Marshal(map[string]string{"url": url})
	_ = os.WriteFile(filepath.Join(dir, engineCanvasFile), append(raw, '\n'), 0o600)
}

type releasesDoc struct {
	V     int                     `json:"v"`
	Types map[string]releasesKind `json:"types"`
}

type releasesKind struct {
	Releases []releaseDoc `json:"releases"`
}

type releaseDoc struct {
	Release      string `json:"release"`
	Date         string `json:"date"`
	Status       string `json:"status"`
	Engine       string `json:"engine"`
	EngineSha256 string `json:"engineSha256"`
	Skill        string `json:"skill,omitempty"`
}

func readReleasesDoc(dir string) releasesDoc {
	doc := releasesDoc{V: 1, Types: map[string]releasesKind{}}
	if raw, err := os.ReadFile(filepath.Join(dir, ReleasesFile)); err == nil {
		_ = json.Unmarshal(raw, &doc)
	}
	if doc.Types == nil {
		doc.Types = map[string]releasesKind{}
	}
	return doc
}

// releaseEntry is the engine path of a release the folder lists and still has, false when it has not.
func releaseEntry(dir, release string) (string, bool) {
	for _, r := range readReleasesDoc(dir).Types[releaseKind].Releases {
		if r.Release == release {
			if got, err := sha256File(filepath.Join(dir, filepath.FromSlash(r.Engine))); err == nil && got == r.EngineSha256 {
				return r.Engine, true
			}
		}
	}
	return "", false
}

// addRelease lists a fetched release as stable in the folder's releases.json, oldest first, written whole and renamed.
func addRelease(dir, release, engine, sum string) error {
	doc := readReleasesDoc(dir)
	kind := doc.Types[releaseKind]
	out := kind.Releases[:0]
	for _, r := range kind.Releases {
		if r.Release != release {
			out = append(out, r)
		}
	}
	out = append(out, releaseDoc{Release: release, Date: time.Now().UTC().Format("2006-01-02"), Status: ReleaseStable, Engine: engine, EngineSha256: sum})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	kind.Releases = out
	doc.Types[releaseKind] = kind
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, ReleasesFile+".tmp")
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, ReleasesFile))
}

// EngineHook is the PreToolUse hook of the fetch's sessions: the Artifact tool may read and list; the one publish that
// makes the canvas (a type_url and nothing else) only when create is set; anything else is denied. It answers the
// hook's JSON.
func EngineHook(input []byte, create bool) []byte {
	var in struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	decide := func(d, reason string) []byte {
		out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse",
			"permissionDecision": d, "permissionDecisionReason": reason}})
		return out
	}
	if json.Unmarshal(input, &in) != nil {
		return decide("deny", "unreadable tool call")
	}
	if in.ToolName != "Artifact" {
		return decide("deny", "the engine fetch may only use the Artifact tool")
	}
	action, _ := in.ToolInput["action"].(string)
	if action == "" {
		action = "publish"
	}
	switch action {
	case "read", "list":
		return decide("allow", "read-only engine fetch")
	case "publish":
		_, typed := in.ToolInput["type_url"].(string)
		extra := false
		for k := range in.ToolInput {
			if k != "action" && k != "type_url" && k != "title" {
				extra = true
			}
		}
		if create && typed && !extra {
			return decide("allow", "the engine's canvas is made once")
		}
	}
	return decide("deny", "the engine fetch only reads: action '"+action+"' is not allowed")
}
