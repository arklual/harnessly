package wire_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// jsonKeys is the top-level JSON keys of a struct type.
func jsonKeys(v any) []string {
	t := reflect.TypeOf(v)
	var out []string
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Every key of every settings document has an owner in wire.Settings and the table names nothing else: a setting
// added to a document without a line in the table fails here.
func TestEverySettingHasAnOwner(t *testing.T) {
	docs := map[string]any{
		wire.SettingsDocMe:      wire.PersonSettings{},
		wire.SettingsDocRuntime: wire.RuntimeSettings{},
		wire.SettingsDocAgent:   wire.AgentSettings{},
	}
	listed := map[string][]string{}
	owners := map[string]wire.SettingOwner{}
	seen := map[string]bool{}
	for _, s := range wire.Settings {
		if _, ok := docs[s.Doc]; !ok {
			t.Errorf("%s.%s: no such settings document", s.Doc, s.Key)
		}
		if s.Owner != wire.OwnerPerson && s.Owner != wire.OwnerMachine {
			t.Errorf("%s.%s: owner %q", s.Doc, s.Key, s.Owner)
		}
		if seen[s.Doc+"."+s.Key] {
			t.Errorf("%s.%s is listed twice", s.Doc, s.Key)
		}
		seen[s.Doc+"."+s.Key] = true
		// A document is answered by one side, so it has one owner.
		if o, ok := owners[s.Doc]; ok && o != s.Owner {
			t.Errorf("%s: keys of two owners", s.Doc)
		}
		owners[s.Doc] = s.Owner
		if s.Runtime && s.Owner != wire.OwnerPerson {
			t.Errorf("%s.%s: only a personal setting is handed to a runtime", s.Doc, s.Key)
		}
		listed[s.Doc] = append(listed[s.Doc], s.Key)
	}
	for doc, v := range docs {
		got := listed[doc]
		sort.Strings(got)
		if want := jsonKeys(v); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the table lists %v, the document has %v", doc, got, want)
		}
	}
	// What a runtime is handed is exactly the personal settings marked so.
	var handed []string
	for _, s := range wire.Settings {
		if s.Runtime {
			handed = append(handed, s.Key)
		}
	}
	sort.Strings(handed)
	if want := jsonKeys(wire.RuntimePerson{}); !reflect.DeepEqual(handed, want) {
		t.Errorf("handed to a runtime: the table says %v, RuntimePerson has %v", handed, want)
	}
	// Every personal key is one a page may write, and the defaults carry each of them.
	raw, _ := json.Marshal(wire.DefaultSettings())
	if _, err := wire.ApplySettingsPatch(wire.DefaultSettings(), raw); err != nil {
		t.Errorf("the defaults are not a patch of every personal key: %v", err)
	}
}

func TestSettingsDocOf(t *testing.T) {
	for path, want := range map[string]wire.SettingOwner{
		"me": wire.OwnerPerson, "me/": wire.OwnerPerson, "me/settings": wire.OwnerPerson,
		"runtime-settings": wire.OwnerMachine, "agent-settings/claude": wire.OwnerMachine,
	} {
		if _, owner, ok := wire.SettingsDocOf(path); !ok || owner != want {
			t.Errorf("%s: %q %v, want %q", path, owner, ok, want)
		}
	}
	for _, path := range []string{"", "machines", "me/other", "agent-settings", "voice", "runtime-settings/x"} {
		if doc, _, ok := wire.SettingsDocOf(path); ok {
			t.Errorf("%s is read as the settings document %s", path, doc)
		}
	}
}

// The runtime recognizes with the settings the hub handed for the asking person; without them (a runtime on its own)
// with its own record; a value that is not settings is as none.
func TestVoiceWorksByTheHandedPersonSettings(t *testing.T) {
	own := &wire.FileMe{Path: filepath.Join(t.TempDir(), "me.json")}
	if _, err := own.Patch(1, json.RawMessage(`{"voice":{"engine":"local","words":[{"word":"Own","heard":["слово"]}]}}`)); err != nil {
		t.Fatal(err)
	}
	var engine string
	srv := &wire.Server{Me: own, Voice: func(_ context.Context, r io.Reader, _, e string) (string, error) {
		_, _ = io.ReadAll(r)
		engine = e
		return "скажи слово", nil
	}}
	say := func(header string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v2/voice", bytes.NewReader([]byte("a recording")))
		if header != "" {
			req.Header.Set(wire.PersonHeader, header)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var got wire.Transcript
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("voice: %d %s", rec.Code, rec.Body)
		}
		return got.Text
	}
	if text := say(""); text != "скажи Own" || engine != "local" {
		t.Errorf("on its own: %q by %q", text, engine)
	}
	handed := wire.EncodePersonHeader(wire.RuntimePerson{Voice: wire.VoiceSettings{Engine: wire.VoiceEngineOpenAI,
		Words: []wire.VoiceWord{{Word: "Theirs", Heard: []string{"слово"}}}}})
	if text := say(handed); text != "скажи Theirs" || engine != wire.VoiceEngineOpenAI {
		t.Errorf("handed: %q by %q", text, engine)
	}
	if text := say("not-settings"); text != "скажи Own" || engine != "local" {
		t.Errorf("a broken header: %q by %q", text, engine)
	}
}

func TestPersonHeaderStaysWithinItsLimit(t *testing.T) {
	p := wire.RuntimePerson{Voice: wire.VoiceSettings{Engine: wire.VoiceEngineLocal, Words: []wire.VoiceWord{}}}
	for i := 0; i < wire.MaxVoiceWords; i++ {
		heard := make([]string, wire.MaxVoiceHeard)
		for j := range heard {
			heard[j] = strings.Repeat("ж", 64)
		}
		p.Voice.Words = append(p.Voice.Words, wire.VoiceWord{Word: strings.Repeat("щ", wire.MaxVoiceWordLen), Heard: heard})
	}
	v := wire.EncodePersonHeader(p)
	if v == "" || len(v) > wire.MaxPersonHeader {
		t.Fatalf("the header is %d bytes", len(v))
	}
}
