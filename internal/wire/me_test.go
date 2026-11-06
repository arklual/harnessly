package wire_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func meCall(t *testing.T, srv *wire.Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// GET /api/v2/me and PATCH /api/v2/me/settings (spec §5) of a runtime on its own: defaults at revision 1, a patch at the
// current revision applied and kept in the file, another revision refused as a conflict, a setting out of its type
// refused whole, and no store means no such page.
func TestMeOfARuntimeOnItsOwn(t *testing.T) {
	file := filepath.Join(t.TempDir(), "v2", "me.json")
	srv := &wire.Server{Me: &wire.FileMe{Path: file, Name: "Ada"}}

	code, me := meCall(t, srv, "GET", "/api/v2/me", "")
	settings, _ := me["settings"].(map[string]any)
	if code != 200 || me["rev"] != float64(1) || me["email"] != nil || me["name"] != "Ada" || settings["uiVersion"] != "next" ||
		settings["density"] != "compact" || settings["defaultMachine"] != nil || len(settings["keys"].(map[string]any)) != 0 {
		t.Fatalf("me: %d %v", code, me)
	}

	code, res := meCall(t, srv, "PATCH", "/api/v2/me/settings", `{"rev":1,"patch":{"density":"compact","enterSends":false,"defaultMachine":"local","keys":{"deck.next":["Alt+KeyN"],"global.help":[]}}}`)
	got, _ := res["me"].(map[string]any)
	if code != 200 || res["r"] != "accepted" || got["rev"] != float64(2) {
		t.Fatalf("patch: %d %v", code, res)
	}
	_, me = meCall(t, srv, "GET", "/api/v2/me", "")
	settings, _ = me["settings"].(map[string]any)
	keys, _ := settings["keys"].(map[string]any)
	if settings["density"] != "compact" || settings["enterSends"] != false || settings["defaultMachine"] != "local" || settings["locale"] != "ru" ||
		len(keys) != 2 || len(keys["global.help"].([]any)) != 0 {
		t.Fatalf("after the patch: %v", me)
	}
	// Kept in the file: another Server on it reads the same.
	other := &wire.Server{Me: &wire.FileMe{Path: file}}
	if _, again := meCall(t, other, "GET", "/api/v2/me", ""); again["rev"] != float64(2) {
		t.Fatalf("a restart forgot the settings: %v", again)
	}

	if code, res := meCall(t, srv, "PATCH", "/api/v2/me/settings", `{"rev":1,"patch":{"density":"full"}}`); code != http.StatusConflict || res["code"] != "conflict" {
		t.Fatalf("a stale revision: %d %v", code, res)
	}
	for _, bad := range []string{
		`{"rev":2,"patch":{"density":"huge"}}`, `{"rev":2,"patch":{"nope":1}}`, `{"rev":2,"patch":{"quickPrompts":[""]}}`,
		`{"rev":2,"patch":{"defaultMachine":"a b/c"}}`, `{"rev":2,"patch":{"keys":{"deck/next":["KeyN"]}}}`,
		`{"rev":2,"patch":{"keys":{"deck.next":["Alt KeyN"]}}}`, `{"rev":2,"patch":{"keys":{"deck.next":null}}}`, `{"rev":2,"patch":{"keys":null}}`, `{"rev":2,"patch":[]}`, `{"patch":{}}`, `{"rev":2}`, `nonsense`,
	} {
		if code, res := meCall(t, srv, "PATCH", "/api/v2/me/settings", bad); code != http.StatusBadRequest || res["code"] != "bad-request" {
			t.Fatalf("%s: %d %v", bad, code, res)
		}
	}
	if _, me := meCall(t, srv, "GET", "/api/v2/me", ""); me["rev"] != float64(2) {
		t.Fatalf("a refused patch changed the revision: %v", me)
	}
	if code, _ := meCall(t, srv, "POST", "/api/v2/me", ""); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /me: %d", code)
	}

	none := &wire.Server{}
	if code, res := meCall(t, none, "GET", "/api/v2/me", ""); code != http.StatusNotFound || res["code"] != "not-found" {
		t.Fatalf("no store: %d %v", code, res)
	}
}

// The dictation's settings: a model and the person's words, replaced whole; an unknown model or a bad word refuses the patch.
func TestMeVoiceSettings(t *testing.T) {
	srv := &wire.Server{Me: &wire.FileMe{Path: filepath.Join(t.TempDir(), "v2", "me.json")}}
	_, me := meCall(t, srv, "GET", "/api/v2/me", "")
	voice, _ := me["settings"].(map[string]any)["voice"].(map[string]any)
	if voice["engine"] != "local" || len(voice["words"].([]any)) != 0 {
		t.Fatalf("default voice: %v", voice)
	}
	good := `{"rev":1,"patch":{"voice":{"engine":"openai","words":[{"word":"Sinopia","heard":["синопия"]},{"word":"harnessly","heard":[]}]}}}`
	if code, res := meCall(t, srv, "PATCH", "/api/v2/me/settings", good); code != 200 || res["r"] != "accepted" {
		t.Fatalf("voice patch: %d %v", code, res)
	}
	_, me = meCall(t, srv, "GET", "/api/v2/me", "")
	voice, _ = me["settings"].(map[string]any)["voice"].(map[string]any)
	if voice["engine"] != "openai" || len(voice["words"].([]any)) != 2 {
		t.Fatalf("saved voice: %v", voice)
	}
	// Every model Settings offers can be picked: the page's list and this one must not part.
	for i, engine := range []string{"local", "chatgpt", "openai", "yandex"} {
		pick := fmt.Sprintf(`{"rev":%d,"patch":{"voice":{"engine":%q,"words":[]}}}`, 2+i, engine)
		if code, res := meCall(t, srv, "PATCH", "/api/v2/me/settings", pick); code != 200 || res["r"] != "accepted" {
			t.Fatalf("pick %s: %d %v", engine, code, res)
		}
	}
	for _, bad := range []string{
		`{"rev":6,"patch":{"voice":{"engine":"whisper","words":[]}}}`,
		`{"rev":6,"patch":{"voice":{"engine":"local","words":[{"word":"","heard":[]}]}}}`,
		`{"rev":6,"patch":{"voice":{"engine":"local","words":[{"word":"a","heard":[""]}]}}}`,
		`{"rev":6,"patch":{"voice":{"engine":"local"}}}`,
		`{"rev":6,"patch":{"voice":{"engine":"local","words":[],"extra":1}}}`,
	} {
		if code, res := meCall(t, srv, "PATCH", "/api/v2/me/settings", bad); code != http.StatusBadRequest || res["code"] != "bad-request" {
			t.Fatalf("%s: %d %v", bad, code, res)
		}
	}
}
