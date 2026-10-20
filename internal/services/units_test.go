package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The previous runtime's pure functions on generated inputs (testdata/gen/units.mjs). SERVICES_UNITS names a larger
// generated file to check instead of the committed sample.

type unitOutput struct {
	OK     json.RawMessage `json:"ok"`
	Throws bool            `json:"throws"`
}

func unitService() *Service {
	return New(Config{
		Home: "/home/agent", GitLabHosts: []string{"gitlab.example.com", "gitlab.com"},
		TrackerURL: "https://tracker.example.com", TrackerHosts: []string{"tracker.example.com", "youtrack.example.com"},
		DocsHost: "docs.example.com", ReaderHost: "platform.example.com", ReaderPath: "/agents/docs/",
		TaskKey: regexp.MustCompile(`^(ACME|DEMO|APP)-[0-9]{1,6}$`), AvatarPath: "/api/platform/link-meta/avatar",
	})
}

// plain turns a value into what encoding/json reads back, so outputs compare as data.
func plain(t *testing.T, v any) any {
	var raw []byte
	switch x := v.(type) {
	case *jsvalue.Object:
		raw = jsvalue.Stringify(x, "")
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = plain(t, e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(t, e)
		}
		return out
	default:
		var err error
		if raw, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return out
}

func strOrNil(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestGoldenUnits(t *testing.T) {
	file := os.Getenv("SERVICES_UNITS")
	if file == "" {
		file = filepath.Join("testdata", "units.json")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var units map[string][][2]json.RawMessage
	if err := json.Unmarshal(raw, &units); err != nil {
		t.Fatal(err)
	}
	s := unitService()
	base, _ := parseWebURL("https://icons.example.org/dir/page.html", nil)
	str := func(in json.RawMessage) string {
		var x string
		_ = json.Unmarshal(in, &x)
		return x
	}
	value := func(in json.RawMessage) any {
		v, err := jsvalue.Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	run := map[string]func(in json.RawMessage) (any, bool){
		"url": func(in json.RawMessage) (any, bool) {
			u, ok := parseWebURL(str(in), nil)
			if !ok {
				return nil, false
			}
			if !u.special {
				return map[string]any{"protocol": u.Protocol()}, true
			}
			return map[string]any{"protocol": u.Protocol(), "username": u.Username, "password": u.Password, "hostname": u.Host, "port": u.Port,
				"pathname": u.Pathname(), "search": u.Search(), "href": u.Href(), "origin": u.Origin(), "k": strOrNil(u.Param("k"))}, true
		},
		"resolve": func(in json.RawMessage) (any, bool) {
			u, ok := parseWebURL(str(in), base)
			if !ok {
				return nil, false
			}
			return u.Href(), true
		},
		"linkTarget": func(in json.RawMessage) (any, bool) {
			lt := s.parseLinkTarget(str(in))
			switch {
			case lt == nil:
				return nil, true
			case lt.kind == "issue":
				return map[string]any{"kind": "issue", "key": lt.key}, true
			case lt.kind == "mr":
				return map[string]any{"kind": "mr", "host": lt.host, "project": lt.project, "iid": lt.iid}, true
			}
			return map[string]any{"kind": "artifact", "url": lt.url}, true
		},
		"gitlabRef": func(in json.RawMessage) (any, bool) {
			out := map[string]any{"mr": nil, "ref": nil}
			if r := s.mrRefOf(str(in)); r != nil {
				out["mr"] = map[string]any{"host": r.host, "project": r.project, "iid": r.iid}
			}
			if r := s.gitlabRefOf(str(in)); r != nil {
				if r.sha != "" {
					out["ref"] = map[string]any{"kind": "commit", "host": r.host, "project": r.project, "sha": r.sha}
				} else {
					out["ref"] = map[string]any{"kind": "mr", "host": r.host, "project": r.project, "iid": r.iid}
				}
			}
			return out, true
		},
		"remote": func(in json.RawMessage) (any, bool) {
			host, project, ok := s.gitlabProjectOf(str(in))
			if !ok {
				return nil, true
			}
			return map[string]any{"host": host, "project": project}, true
		},
		"artifactHtml": func(in json.RawMessage) (any, bool) {
			title, desc := parseArtifactHTML(str(in))
			return map[string]any{"title": strOrNil(title), "description": strOrNil(desc)}, true
		},
		"icons": func(in json.RawMessage) (any, bool) {
			out := []any{}
			for _, c := range iconCandidates(str(in), base.Href()) {
				out = append(out, map[string]any{"href": c.href, "rank": c.rank})
			}
			return out, true
		},
		"parentHost": func(in json.RawMessage) (any, bool) {
			if p := parentHost(str(in)); p != "" {
				return p, true
			}
			return nil, true
		},
		"task": func(in json.RawMessage) (any, bool) {
			task := s.parseTask(value(in), func(login string, source any) any { return "avatar:" + login + ":" + str2(source) })
			if task == nil {
				return nil, true
			}
			return task, true
		},
		"taskText": func(in json.RawMessage) (any, bool) {
			v := value(in)
			return []any{cleanTaskText(v, 4096), cleanTaskText(v, 10)}, true
		},
		"diff": func(in json.RawMessage) (any, bool) {
			d, err := unifiedDiffOf(orUndefined(value(in)))
			return d, err == nil
		},
		"sessionStart": func(in json.RawMessage) (any, bool) {
			if ts, ok := parseSessionStart(str(in)); ok {
				return ts, true
			}
			return nil, true
		},
		"reflog": func(in json.RawMessage) (any, bool) {
			shas := parseReflogCommits(str(in), 1792320100000)
			if shas == nil {
				shas = []string{}
			}
			return shas, true
		},
		"organizer": func(in json.RawMessage) (any, bool) {
			ops, refusal := ParseOrganizerOps(value(in))
			if refusal != nil {
				return map[string]any{"error": map[string]any{"status": refusal.Status, "body": refusal.Body}}, true
			}
			first, _ := ParseOrganizerOps(value([]byte(`{"ops":[{"op":"project.create","id":"prj-abcdef","name":"Alpha","color":"blue"},{"op":"pin","ref":"agent:tab-2"}]}`)))
			current, _, _ := applyOrganizerOps(emptyOrganizer(), first, 1000)
			next, changed, refusal := applyOrganizerOps(current, ops, 2000)
			if refusal != nil {
				return map[string]any{"error": map[string]any{"status": refusal.Status, "body": refusal.Body}}, true
			}
			return map[string]any{"organizer": next, "changed": changed}, true
		},
		"isOrganizer": func(in json.RawMessage) (any, bool) { return isOrganizer(value(in)), true },
	}
	failures := 0
	for name, cases := range units {
		fn := run[name]
		if fn == nil {
			t.Errorf("no Go side for %s", name)
			continue
		}
		for i, c := range cases {
			var want unitOutput
			if err := json.Unmarshal(c[1], &want); err != nil {
				t.Fatal(err)
			}
			got, ok := fn(c[0])
			var wantV any
			_ = json.Unmarshal(want.OK, &wantV)
			if name == "url" && known(str(c[0]), wantV, got, ok) {
				continue
			}
			same := ok == !want.Throws && (want.Throws || reflect.DeepEqual(plain(t, got), wantV))
			if !same {
				failures++
				if failures <= 40 {
					t.Errorf("%s #%d %s:\n got %v %s\nwant %s %s", name, i, clip(string(c[0])), ok, clip(fmt.Sprint(string(jsonOf(t, got)))), clip(string(want.OK)), map[bool]string{true: "(throws)"}[want.Throws])
				}
			}
		}
	}
	if failures > 0 {
		t.Errorf("%d cases differ", failures)
	}
}

// known are the URL parser's accepted differences: a scheme that is not special is only told apart from https (the
// previous runtime may parse it further or refuse it), and an internationalized host (a punycode label, a name that
// is not ASCII) is refused rather than checked and converted.
func known(input string, want any, got any, ok bool) bool {
	w, _ := want.(map[string]any)
	if host, _ := w["hostname"].(string); strings.Contains(strings.ToLower(input), "xn--") || strings.Contains(host, "xn--") {
		return true
	}
	g, _ := got.(map[string]any)
	if ok && len(g) == 1 {
		return w == nil || w["protocol"] == g["protocol"]
	}
	return false
}

func str2(v any) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprint(v)
}

// orUndefined: null from the generator stands for both null and a value it could not write.
func orUndefined(v any) any { return v }

func jsonOf(t *testing.T, v any) []byte {
	if o, ok := v.(*jsvalue.Object); ok {
		return jsvalue.Stringify(o, "")
	}
	b, _ := json.Marshal(plain(t, v))
	return b
}

func clip(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}
