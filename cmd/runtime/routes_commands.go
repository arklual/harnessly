package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/catalog"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
)

func init() { routeGroups = append(routeGroups, commandRoutes) }

// nodeQuery parses a query string the way the previous runtime did: "+" is a space, a broken escape stays as typed,
// bytes that are not UTF-8 become U+FFFD, ";" is an ordinary character, a repeated key collects its values.
func nodeQuery(raw string) map[string][]string {
	out := map[string][]string{}
	unescape := func(s string) string {
		s = strings.ReplaceAll(s, "+", " ")
		var b []byte
		for i := 0; i < len(s); i++ {
			if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
				b = append(b, unhex(s[i+1])<<4|unhex(s[i+2]))
				i += 2
				continue
			}
			b = append(b, s[i])
		}
		return strings.ToValidUTF8(string(b), "\uFFFD")
	}
	pairs := 0
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		if pairs++; pairs > 1000 {
			break
		}
		k, v, _ := strings.Cut(part, "=")
		key := unescape(k)
		out[key] = append(out[key], unescape(v))
	}
	return out
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func unhex(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

var noCache = regexp.MustCompile(`(?:^|,)\s*no-cache\s*(?:,|$)`)

func joinedHeader(r *http.Request, name string) string {
	return strings.Join(r.Header.Values(name), ", ")
}

// freshFor: the caller already holds the answer with this tag (If-None-Match, unless it asks for no-cache).
func freshFor(r *http.Request, etag string) bool {
	match := joinedHeader(r, "If-None-Match")
	if match == "" || noCache.MatchString(joinedHeader(r, "Cache-Control")) {
		return false
	}
	if match == "*" {
		return true
	}
	for _, tag := range strings.Split(match, ",") {
		if strings.TrimPrefix(strings.TrimSpace(tag), "W/") == etag {
			return true
		}
	}
	return false
}

// tagged is how the previous runtime's API sent every JSON answer of a handler: an ETag hashing the body, and 304
// with no body when the caller already holds it.
func tagged(r *http.Request, status int, v any, h http.Header) (int, any, http.Header) {
	if h == nil {
		h = http.Header{}
	}
	body := gate.Encode(v)
	sum := sha1.Sum(body)
	etag := `"` + base64.RawURLEncoding.EncodeToString(sum[:])[:27] + `"`
	h.Set("ETag", etag)
	if freshFor(r, etag) {
		return http.StatusNotModified, nil, h
	}
	return status, json.RawMessage(body), h
}

func errorBody(text string) map[string]string { return map[string]string{"error": text} }

// commandRoutes: the slash commands of Claude Code or Codex on this machine for a folder, and the models and efforts
// they can be started with (slice fsread). Both are a person's: a cli-token alone is refused.
func commandRoutes(d deps) []gate.JSONRoute {
	claudeDir := env("CLAUDE_CONFIG_DIR", filepath.Join(d.Home, ".claude"))
	codexDir := env("CODEX_HOME", filepath.Join(d.Home, ".codex"))
	commands := catalog.New(catalog.Config{
		ClaudeDir:        claudeDir,
		CodexDir:         codexDir,
		WorkspaceRoot:    d.WorkspaceRoot,
		QuickPromptsFile: filepath.Join(d.DataDir, "quick-prompts.json"),
		WorkspacesFile:   filepath.Join(d.DataDir, "workspaces.json"),
	}, gate.Encode)
	commandCatalog.Store(commands)
	launch := catalog.LaunchConfig{
		ClaudeDir:     claudeDir,
		CodexDir:      codexDir,
		RuntimeConfig: d.ConfigFile,
		DefaultsKey:   env("RUNTIME_LAUNCH_DEFAULTS_KEY", "launchDefaults"),
	}

	// guard is what both handlers check before their own work; done is false when it already answered.
	guard := func(r *http.Request) (status int, v any, h http.Header, done bool) {
		if status, v, h, refused := d.APIRefusal(r); refused {
			return status, v, h, true
		}
		if !d.SessionOK(r) {
			status, v, h = tagged(r, http.StatusForbidden, errorBody("Forbidden"), nil)
			return status, v, h, true
		}
		h = http.Header{"Cache-Control": {"no-store"}}
		if r.Method != http.MethodGet {
			h.Set("Allow", "GET")
			status, v, h = tagged(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h)
			return status, v, h, true
		}
		return 0, nil, h, false
	}
	provider := func(q map[string][]string) (string, bool) {
		if p := q["provider"]; len(p) == 1 && catalog.IsProvider(p[0]) {
			return p[0], true
		}
		return "", false
	}
	unknownAgent := locale.T("Unknown agent", "Неизвестный агент")

	return []gate.JSONRoute{
		{
			Slice:    "fsread",
			Match:    func(r *http.Request) bool { return pathIs(r, platformAPI+"/commands") },
			Volatile: []string{"generatedAt"},
			Compute: func(r *http.Request) (int, any, http.Header) {
				status, v, h, done := guard(r)
				if done {
					return status, v, h
				}
				q := nodeQuery(r.URL.RawQuery)
				p, ok := provider(q)
				if !ok {
					return tagged(r, http.StatusBadRequest, errorBody(unknownAgent.String()), h)
				}
				var cwd string
				switch vals := q["cwd"]; len(vals) {
				case 0:
				case 1:
					cwd = vals[0]
				default:
					return tagged(r, http.StatusBadRequest, errorBody(catalog.ErrOutside.String()), h)
				}
				dir, root, errText := commands.ResolveCwd(cwd)
				if errText != "" {
					return tagged(r, http.StatusBadRequest, errorBody(errText), h)
				}
				res := commands.Get(p, dir, root)
				h.Set("ETag", res.Tag)
				if joinedHeader(r, "If-None-Match") == res.Tag {
					return http.StatusNotModified, nil, h
				}
				return tagged(r, http.StatusOK, res.Body, h)
			},
		},
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/launch-options") },
			Compute: func(r *http.Request) (int, any, http.Header) {
				status, v, h, done := guard(r)
				if done {
					return status, v, h
				}
				p, ok := provider(nodeQuery(r.URL.RawQuery))
				if !ok {
					return tagged(r, http.StatusBadRequest, errorBody(unknownAgent.String()), h)
				}
				return tagged(r, http.StatusOK, launch.Launch(p), h)
			},
		},
	}
}
