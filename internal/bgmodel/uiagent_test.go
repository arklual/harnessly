package bgmodel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/extproviders"
)

var uiPlaces = []UIPlace{{ID: "settings/plugins", Title: "Настройки → Плагины"}, {ID: "settings/general", Title: "Настройки → Основные"}}

var uiSettings = []UISetting{
	{Key: "runtime.idleStop", Title: "Останавливать чат", Section: "Чаты", Kind: "enum",
		Options: []UIOption{{Value: "off", Label: "никогда"}, {Value: "2h", Label: "через 2 часа"}}, Value: "2h", ValueLabel: "через 2 часа"},
	{Key: "runtime.limitContinue", Title: "Продолжать после лимита", Kind: "bool", Value: "on", ValueLabel: "Вкл"},
}

// filtered streams the model's words in pieces of n bytes through the filter: what the page is told.
func filtered(words string, n int) (act *UIAction, said string) {
	var b strings.Builder
	f := &uiFilter{places: uiPlaces, settings: uiSettings, onAction: func(a UIAction) {
		if act != nil {
			panic("a second action")
		}
		act = &a
	}, onDelta: func(s string) { b.WriteString(s) }}
	for len(words) > 0 {
		k := min(n, len(words))
		f.write(words[:k])
		words = words[k:]
	}
	f.endLine()
	return act, b.String()
}

func TestUIFilterTakesTheMarkWhereverTheModelPutIt(t *testing.T) {
	cases := []struct {
		words, kind, target, said string
	}{
		{"GO settings/plugins\nОткрыл «Плагины».", UIActGo, "settings/plugins", "Открыл «Плагины»."},
		// The mark after the words, in the same line or on the next one, decorated: it still leads there, unseen.
		{"Открываю настройки плагинов. GO settings/plugins", UIActGo, "settings/plugins", "Открываю настройки плагинов."},
		{"Открываю настройки плагинов.\nGO settings/plugins", UIActGo, "settings/plugins", "Открываю настройки плагинов."},
		{"\n\n**GO settings/plugins**\nОткрываю.", UIActGo, "settings/plugins", "Открываю."},
		{"Opening `GO settings/general`.", UIActGo, "settings/general", "Opening."},
		{"GO settings/plugins.\nOpened.", UIActGo, "settings/plugins", "Opened."},
		// A place or a value the request does not allow: no action, and the mark is not shown either.
		{"GO settings/unknown\nНе нашёл.", "", "", "Не нашёл."},
		{"SET runtime.idleStop = 2h\nУже так.", "", "", "Уже так."},
		{"SET runtime.idleStop = 9h\nНет такого.", "", "", "Нет такого."},
		{"SET nope = on\nНет.", "", "", "Нет."},
		{"SET runtime.idleStop = никогда\nПредлагаю не останавливать.", UIActSet, "runtime.idleStop", "Предлагаю не останавливать."},
		{"Предлагаю выключить. SET runtime.limitContinue = off", UIActSet, "runtime.limitContinue", "Предлагаю выключить."},
		{"-\nЭто в «Основных».", "", "", "Это в «Основных»."},
		// Words that only look like a mark pass as they are.
		{"Settings — GOod place. Section «General».", "", "", "Settings — GOod place. Section «General»."},
	}
	for _, c := range cases {
		for _, n := range []int{1, 3, 1000} {
			act, said := filtered(c.words, n)
			if said != c.said {
				t.Errorf("%q by %d: said %q, want %q", c.words, n, said, c.said)
			}
			switch {
			case c.kind == "" && act != nil:
				t.Errorf("%q by %d: no action wanted, got %+v", c.words, n, act)
			case c.kind != "" && (act == nil || act.Kind != c.kind || (act.Place != c.target && act.Key != c.target)):
				t.Errorf("%q by %d: %+v", c.words, n, act)
			}
		}
	}
	if act, _ := filtered("SET runtime.idleStop = никогда", 4); act == nil || act.Value != "off" || act.Label != "никогда" {
		t.Errorf("a value named by its words is sent as the value: %+v", act)
	}
}

func TestUIAgentAnswersInTheInterfacesLanguage(t *testing.T) {
	en := uiAgentCall("открой плагины", "", "en", uiPlaces, nil)
	ru := uiAgentCall("open plugins", "", "ru", uiPlaces, nil)
	if !strings.Contains(en.System, "in English") || strings.Contains(en.System, "по-русски") ||
		!strings.HasSuffix(en.Prompt, "Answer in English, whatever the language of the request.\n") {
		t.Errorf("en: %q", en.System)
	}
	if !strings.Contains(ru.System, "по-русски") || !strings.Contains(ru.Prompt, "Просьба: open plugins") {
		t.Errorf("ru: %q / %q", ru.System, ru.Prompt)
	}
}

func uiServer(t *testing.T, pieces []string, asked *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b struct{ Model string }
		_ = json.Unmarshal(raw, &b)
		if asked != nil {
			*asked = b.Model
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, p := range pieces {
			d, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": p}}}})
			_, _ = w.Write([]byte("data: " + string(d) + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func TestUIAgentStreamActsBeforeItSpeaks(t *testing.T) {
	var asked string
	srv := uiServer(t, []string{"GO sett", "ings/plugins\nОткрыл", " «Плагины»."}, &asked)
	defer srv.Close()
	reg := listedProviders{
		fakeProviders: fakeProviders{"a": {ID: "a", BaseURL: srv.URL, API: extproviders.APIOpenAI}},
		views:         []extproviders.View{{ID: "a", HasKey: true, Models: []extproviders.Model{{ID: "big-pro"}, {ID: "claude-haiku-4"}}}},
	}
	r := &Runner{Store: &Store{Path: filepath.Join(t.TempDir(), "bg.json")}, Providers: reg, Client: &Client{}}
	var order []string
	text, act, used, err := r.UIAgentStream(context.Background(), "открой настройку плагинов", "settings/general", "ru", uiPlaces, uiSettings,
		func(a UIAction) { order = append(order, "action:"+a.Place) },
		func(s string) { order = append(order, "delta:"+s) })
	if !used || err != nil || act == nil || act.Place != "settings/plugins" || text != "Открыл «Плагины»." {
		t.Fatalf("%q %+v used=%v err=%v", text, act, used, err)
	}
	if asked != "claude-haiku-4" {
		t.Errorf("a quick model by default, got %q", asked)
	}
	if len(order) < 2 || order[0] != "action:settings/plugins" || !strings.HasPrefix(order[1], "delta:Открыл") {
		t.Errorf("the order: %q", order)
	}
}

func TestUIAgentStreamKeepsWordsWithoutAHead(t *testing.T) {
	srv := uiServer(t, []string{"Плагины — в Настройках."}, nil)
	defer srv.Close()
	r := &Runner{Store: &Store{Path: filepath.Join(t.TempDir(), "bg.json")},
		Providers: fakeProviders{"a": {ID: "a", BaseURL: srv.URL, API: extproviders.APIOpenAI}}, Client: &Client{}}
	if _, err := r.Pick(FeatureUIAgent, &Choice{Provider: "a", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	text, act, _, err := r.UIAgentStream(context.Background(), "где плагины?", "", "ru", uiPlaces, nil, func(UIAction) { t.Error("no action") },
		func(s string) { said.WriteString(s) })
	if err != nil || act != nil || text != "Плагины — в Настройках." || said.String() != text {
		t.Fatalf("%q %q %+v %v", text, said.String(), act, err)
	}
}

func TestQuickCallAsksForNoThinking(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()
	c := &Client{}
	for _, quick := range []bool{true, false} {
		if _, err := c.Complete(context.Background(), Request{BaseURL: srv.URL, API: extproviders.APIAnthropic, Model: "m", Prompt: "p", Quick: quick}); err != nil {
			t.Fatal(err)
		}
		th, has := body["thinking"].(map[string]any)
		if has != quick || (quick && th["type"] != "disabled") {
			t.Errorf("quick=%v: thinking=%v", quick, body["thinking"])
		}
	}
}

func TestPlacesOfMapReadsTheWindowsMap(t *testing.T) {
	got := PlacesOfMap("Где что в harnessly:\n- Настройки → Плагины: /settings/plugins\n- Sinopia Design: /designly\n- без адреса\n- Чужое: https://example.com\n")
	if len(got) != 2 || got[0].ID != "/settings/plugins" || got[0].Title != "Настройки → Плагины" || got[1].ID != "/designly" {
		t.Fatalf("%+v", got)
	}
}
