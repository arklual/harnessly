package extproviders

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memSecrets map[string]string

func (m memSecrets) Put(name, value string) error { m[name] = value; return nil }
func (m memSecrets) Reveal(name string) (string, error) {
	v, ok := m[name]
	if !ok {
		return "", errors.New("no secret")
	}
	return v, nil
}
func (m memSecrets) Remove(name string) error { delete(m, name); return nil }
func (m memSecrets) Has(name string) bool     { _, ok := m[name]; return ok }

func newService(t *testing.T) (*Service, memSecrets) {
	t.Helper()
	sec := memSecrets{}
	return &Service{Store: &Store{Dir: t.TempDir()}, Secrets: sec}, sec
}

const testKey = "the-test-key"

func TestCreateKeepsTheKeyOutOfTheFileAndTheAnswers(t *testing.T) {
	svc, sec := newService(t)
	view, err := svc.Create(Input{Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1/", API: APIOpenAI, Preset: "deepseek", Key: testKey})
	if err != nil {
		t.Fatal(err)
	}
	if view.ID != "deepseek" || view.BaseURL != "https://api.deepseek.com/v1" || view.Host != "api.deepseek.com" || !view.HasKey || !view.ClaudeCLI || !view.Proxied {
		t.Fatalf("view: %+v", view)
	}
	if sec["provider-deepseek-key"] != testKey {
		t.Fatalf("the key is not in the safe: %v", sec)
	}
	file, err := os.ReadFile(filepath.Join(svc.Store.Dir, "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := json.Marshal(view)
	list, _ := svc.List()
	listed, _ := json.Marshal(list)
	for name, raw := range map[string][]byte{"file": file, "answer": answer, "list": listed} {
		if strings.Contains(string(raw), testKey) {
			t.Errorf("the %s holds the key", name)
		}
	}
	if strings.Contains(string(answer), "provider-deepseek-key") {
		t.Error("the answer names the secret")
	}

	// A second provider of the same name gets an id of its own.
	again, err := svc.Create(Input{Name: "DeepSeek", BaseURL: "https://api.deepseek.com/anthropic", API: APIAnthropic})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == view.ID || !strings.HasPrefix(again.ID, "deepseek-") || again.HasKey || !again.ClaudeCLI {
		t.Fatalf("second: %+v", again)
	}
	// A name with no Latin letters still makes an id.
	ru, err := svc.Create(Input{Name: "Мой сервер", BaseURL: "http://localhost:11434/v1", API: APIOpenAI})
	if err != nil || !strings.HasPrefix(ru.ID, "provider-") {
		t.Fatalf("named in Russian: %+v, %v", ru, err)
	}
}

func TestCreateRefusesABadForm(t *testing.T) {
	svc, _ := newService(t)
	for want, in := range map[string]Input{
		ReasonBadName: {Name: "  ", BaseURL: "https://a.example", API: APIOpenAI},
		ReasonBadURL:  {Name: "A", BaseURL: "ftp://a.example", API: APIOpenAI},
		ReasonBadAPI:  {Name: "A", BaseURL: "https://a.example", API: "grpc"},
		ReasonBadKey:  {Name: "A", BaseURL: "https://a.example", API: APIOpenAI, Key: "a\nb"},
	} {
		if _, err := svc.Create(in); ReasonOf(err) != want {
			t.Errorf("%s: got %v", want, err)
		}
	}
	for _, bad := range []string{"https://user:pass@a.example", "https://a.example?key=1", "a.example", ""} {
		if _, err := CleanURL(bad); ReasonOf(err) != ReasonBadURL {
			t.Errorf("address %q was taken", bad)
		}
	}
}

func TestUpdateAndRemove(t *testing.T) {
	svc, sec := newService(t)
	var seen []string
	svc.OnChange = func(what string, _, _ *Provider) { seen = append(seen, what) }
	view, err := svc.Create(Input{Name: "Groq", BaseURL: "https://api.groq.com/openai/v1", API: APIOpenAI, Key: testKey,
		Models: []Model{{ID: "llama"}, {ID: "llama"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Models) != 1 {
		t.Fatalf("models: %+v", view.Models)
	}
	name, base := "Groq 2", "https://api.groq.com/v2"
	got, err := svc.Update(view.ID, Patch{Name: &name, BaseURL: &base})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != name || got.BaseURL != base || len(got.Models) != 0 || !got.HasKey {
		t.Fatalf("after the move: %+v", got)
	}
	empty := ""
	if got, err = svc.Update(view.ID, Patch{Key: &empty}); err != nil || got.HasKey || len(sec) != 0 {
		t.Fatalf("the key stayed: %+v, %v, %v", got, err, sec)
	}
	fresh := "a-new-key"
	if got, err = svc.Update(view.ID, Patch{Key: &fresh}); err != nil || !got.HasKey || sec["provider-groq-key"] != fresh {
		t.Fatalf("the new key: %+v, %v, %v", got, err, sec)
	}
	if _, err := svc.Update("nobody", Patch{}); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("an unknown provider: %v", err)
	}
	if err := svc.Remove(view.ID); err != nil || len(sec) != 0 {
		t.Fatalf("removed: %v, %v", err, sec)
	}
	if err := svc.Remove(view.ID); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("removed twice: %v", err)
	}
	if strings.Join(seen, ",") != "created,updated,updated,updated,removed" {
		t.Fatalf("changes: %v", seen)
	}
}

func TestCheckSaysWhatIsWrong(t *testing.T) {
	openai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"b-model"},{"id":"a-model","name":"A"}]}`))
	}))
	defer openai.Close()
	// An Anthropic-compatible vendor with no list of models: it only takes messages.
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"not_found_error","message":"model: harnessly-check"}}`))
	}))
	defer anthropic.Close()
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>hello</html>"))
	}))
	defer html.Close()
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	p := &Prober{}
	ctx := context.Background()
	got := p.Check(ctx, openai.URL+"/v1", APIOpenAI, testKey)
	if !got.OK || len(got.Models) != 2 || got.Models[0].ID != "a-model" || got.Models[0].Label != "A" || got.Message == "" {
		t.Fatalf("a good provider: %+v", got)
	}
	for name, c := range map[string]struct {
		check Check
		want  string
	}{
		"a wrong key":            {p.Check(ctx, openai.URL+"/v1", APIOpenAI, "another-key"), CheckUnauthorized},
		"a dead address":         {p.Check(ctx, deadURL, APIOpenAI, testKey), CheckUnreachable},
		"the other kind of API":  {p.Check(ctx, anthropic.URL, APIOpenAI, testKey), CheckWrongAPI},
		"a path that is not it":  {p.Check(ctx, openai.URL, APIOpenAI, testKey), CheckWrongAPI},
		"a page, not an API":     {p.Check(ctx, html.URL, APIOpenAI, testKey), CheckWrongAPI},
		"anthropic, a wrong key": {p.Check(ctx, anthropic.URL, APIAnthropic, "another-key"), CheckUnauthorized},
	} {
		if c.check.OK || c.check.Reason != c.want || c.check.Message == "" || c.check.Models == nil {
			t.Errorf("%s: %+v, want %s", name, c.check, c.want)
		}
	}
	if got := p.Check(ctx, anthropic.URL, APIAnthropic, testKey); !got.OK || len(got.Models) != 0 {
		t.Fatalf("anthropic without a list of models: %+v", got)
	}
	raw, _ := json.Marshal(p.Check(ctx, openai.URL+"/v1", APIOpenAI, testKey))
	if strings.Contains(string(raw), testKey) {
		t.Fatal("the check holds the key")
	}

	// A saved provider keeps the models its check brought, and a form checks with the saved key.
	svc, _ := newService(t)
	view, err := svc.Create(Input{Name: "Local", BaseURL: openai.URL + "/v1", API: APIOpenAI, Key: testKey})
	if err != nil {
		t.Fatal(err)
	}
	list, err := svc.Models(ctx, view.ID, false)
	if err != nil || len(list.Models) != 2 || list.At == 0 || list.Check == nil || !list.Check.OK {
		t.Fatalf("models: %+v, %v", list, err)
	}
	if list, err = svc.Models(ctx, view.ID, false); err != nil || list.Check != nil || len(list.Models) != 2 {
		t.Fatalf("the kept models: %+v, %v", list, err)
	}
	if got, err := svc.CheckDraft(ctx, Draft{ID: view.ID, BaseURL: openai.URL + "/v1", API: APIOpenAI}); err != nil || !got.OK {
		t.Fatalf("a form with the saved key: %+v, %v", got, err)
	}
	if got, err := svc.CheckDraft(ctx, Draft{BaseURL: openai.URL + "/v1", API: APIOpenAI}); err != nil || got.Reason != CheckUnauthorized {
		t.Fatalf("a form with no key: %+v, %v", got, err)
	}

	// The saved key never goes to another server: neither with a form that names one, nor after the address moved.
	var leaked []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = append(leaked, r.Header.Get("Authorization")+r.Header.Get("x-api-key"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer other.Close()
	if got, err := svc.CheckDraft(ctx, Draft{ID: view.ID, BaseURL: other.URL + "/v1", API: APIOpenAI}); err != nil || got.Reason != CheckUnauthorized {
		t.Fatalf("a form that names another server: %+v, %v", got, err)
	}
	moved := other.URL + "/v1"
	after, err := svc.Update(view.ID, Patch{BaseURL: &moved})
	if err != nil || after.HasKey {
		t.Fatalf("the key stayed with an address of another server: %+v, %v", after, err)
	}
	if _, err := svc.Check(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	if len(leaked) != 2 || strings.Join(leaked, "") != "" {
		t.Fatalf("the saved key went to another server: %d requests, %q", len(leaked), leaked)
	}
}

func TestEndpoint(t *testing.T) {
	for _, c := range [][4]string{
		{"https://api.deepseek.com/v1", string(APIOpenAI), "models", "https://api.deepseek.com/v1/models"},
		{"https://api.z.ai/api/anthropic", string(APIAnthropic), "messages", "https://api.z.ai/api/anthropic/v1/messages"},
		{"https://api.anthropic.com/v1/", string(APIAnthropic), "models", "https://api.anthropic.com/v1/models"},
	} {
		if got := Endpoint(c[0], API(c[1]), c[2]); got != c[3] {
			t.Errorf("%s: %s", c[0], got)
		}
	}
}

// What the person did to the models survives a refresh from the provider: a model they added stays, a name and efforts
// they gave stay, and the provider's other models follow its list.
func TestARefreshKeepsWhatThePersonDidToTheModels(t *testing.T) {
	kept := []Model{
		{ID: "a", Label: "Мой A", Efforts: []string{"low", "high"}, Edited: true},
		{ID: "b", Label: "B old"},
		{ID: "mine", Label: "Своя", Manual: true},
		{ID: "gone"},
	}
	fresh := []Model{{ID: "a", Label: "A"}, {ID: "b", Label: "B new"}, {ID: "c"}}
	got := MergeModels(kept, fresh)
	ids := []string{}
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "a,b,c,mine" {
		t.Fatalf("ids: %v", ids)
	}
	if got[0].Label != "Мой A" || len(got[0].Efforts) != 2 || !got[0].Edited || got[1].Label != "B new" || !got[3].Manual {
		t.Fatalf("merged: %+v", got)
	}
	// A model the person added that the provider now lists is the provider's, with what the person gave it.
	if again := MergeModels([]Model{{ID: "c", Label: "Моя C", Manual: true}}, fresh); again[2].Label != "Моя C" || again[2].Manual || !again[2].Edited {
		t.Fatalf("listed now: %+v", again)
	}
}

func TestAModelsEffortsAreTheWiresInTheirOrder(t *testing.T) {
	got, err := cleanModels([]Model{{ID: "m", Efforts: []string{"high", "low", "high"}, DefaultEffort: "low"}})
	if err != nil || strings.Join(got[0].Efforts, ",") != "low,high" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []Model{{ID: "m", Efforts: []string{"turbo"}}, {ID: "m", Efforts: []string{"low"}, DefaultEffort: "high"}, {ID: "m", DefaultEffort: "low"}} {
		if _, err := cleanModels([]Model{bad}); ReasonOf(err) != ReasonBadModel {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

// A model the person typed and the name they gave a listed one are kept when the provider is asked again.
func TestACheckKeepsTheModelsThePersonTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"listed"}]}`))
	}))
	defer srv.Close()
	svc, _ := newService(t)
	v, err := svc.Create(Input{Name: "Local", BaseURL: srv.URL + "/v1", API: APIOpenAI,
		Models: []Model{{ID: "typed", Label: "Моя", Efforts: []string{"medium"}, Manual: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Check(context.Background(), v.ID); err != nil {
		t.Fatal(err)
	}
	p, _ := svc.Get(v.ID)
	if len(p.Models) != 2 || p.Models[0].ID != "listed" || p.Models[1].ID != "typed" || p.Models[1].Efforts[0] != "medium" {
		t.Fatalf("models: %+v", p.Models)
	}
}

// The window of a model comes in the vendors' own words; a refresh gives it to a model the person named without one.
func TestTheModelsWindowIsReadFromTheList(t *testing.T) {
	got, ok := parseModels([]byte(`{"data":[{"id":"a","context_length":131072},{"id":"b","context_window":32768},{"id":"c","top_provider":{"context_length":200000}},{"id":"d"}]}`))
	if !ok || got[0].Window != 131072 || got[1].Window != 32768 || got[2].Window != 200000 || got[3].Window != 0 {
		t.Fatalf("%+v", got)
	}
	merged := MergeModels([]Model{{ID: "a", Label: "Мой", Edited: true}, {ID: "b", Window: 16000, Edited: true}}, got)
	if merged[0].Window != 131072 || merged[1].Window != 16000 {
		t.Fatalf("merged: %+v", merged)
	}
	if _, err := cleanModels([]Model{{ID: "x", Window: -1}}); ReasonOf(err) != ReasonBadModel {
		t.Fatal("a negative window")
	}
}
