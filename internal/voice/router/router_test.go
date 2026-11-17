package router

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var req = Request{
	Task: "открой настройки",
	Chats: []Chat{
		{ID: "c1", Title: "Лендинг", Current: true, State: "asks", Ask: &Ask{Question: "Какой цвет?", Options: []string{"синий", "красный"}}},
		{ID: "c2", Title: "Бэкенд", State: "needs_permission", Permission: "npm test"},
	},
	Catalog: Catalog{
		Pages:    []Choice{{ID: "home", Label: "Главная"}, {ID: "settings", Label: "Настройки"}},
		Sections: []Choice{{ID: "voice", Label: "Голос"}},
		Themes:   []Choice{{ID: "dark", Label: "Тёмная"}, {ID: "light", Label: "Светлая"}},
		Settings: []Setting{{Key: "enterSends", Label: "Enter отправляет", Values: []string{"on", "off"}}},
	},
}

func TestParseTakesTheFirstObject(t *testing.T) {
	p, err := Parse("```json\n{\"say\":\"ок\",\"actions\":[{\"do\":\"theme\",\"value\":\"light\"}]}\n```{\"say\":\"x\"}")
	if err != nil || p.Say != "ок" || len(p.Actions) != 1 {
		t.Fatalf("got %+v %v", p, err)
	}
	if _, err := Parse("no json here"); err == nil {
		t.Fatal("want an error")
	}
}

func TestCleanKeepsOnlyWhatThePageCanDo(t *testing.T) {
	yes := true
	got := Clean([]Action{
		{Do: DoTheme, Value: "Светлая"},
		{Do: DoTheme, Value: "neon"},
		{Do: DoOpen, Page: "settings", Section: "nope"},
		{Do: DoOpen, Page: "galaxy"},
		{Do: DoAnswer, Chat: "Лендинг", Option: 2},
		{Do: DoAnswer, Chat: "zzz", Option: 1},
		{Do: DoPermit, Chat: "c2", Allow: &yes},
		{Do: DoPermit, Chat: "c2"},
		{Do: DoSend, Chat: "c2", Text: " "},
		{Do: DoSetting, Key: "enterSends", Value: "maybe"},
		{Do: "launch_rockets"},
		{Do: DoMood, Value: "joy"},
	}, req)
	want := []string{"theme:light", "open:settings:", "answer:c1", "permit:c2", "mood:joy"}
	var have []string
	for _, a := range got {
		switch a.Do {
		case DoTheme, DoMood:
			have = append(have, a.Do+":"+a.Value)
		case DoOpen:
			have = append(have, a.Do+":"+a.Page+":"+a.Section)
		default:
			have = append(have, a.Do+":"+a.Chat)
		}
	}
	if strings.Join(have, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", have, want)
	}
}

func TestRouteRecallsInASecondRound(t *testing.T) {
	var prompts []string
	r := Router{
		Ask: func(_ context.Context, _, prompt string) (string, error) {
			prompts = append(prompts, prompt)
			if len(prompts) == 1 {
				return `{"say":"сейчас вспомню","actions":[{"do":"recall","query":"тарифы"}]}`, nil
			}
			return `{"say":"вчера решили три тарифа","mood":"joy","actions":[]}`, nil
		},
		Recall: func(_ context.Context, q string) []string {
			return []string{"тарифы: три тарифа, 0/20/50"}
		},
	}
	p, err := r.Route(context.Background(), Request{Task: "помнишь про тарифы?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "три тарифа, 0/20/50") {
		t.Fatalf("second round did not get the recall: %v", prompts)
	}
	if p.Say != "вчера решили три тарифа" || len(p.Actions) != 0 || len(p.Recalled) != 1 {
		t.Fatalf("got %+v", p)
	}
}

func TestPromptListsChatsQuestionsAndCatalog(t *testing.T) {
	s := Prompt(req, nil)
	for _, w := range []string{"id=c1 «Лендинг» [CURRENT]", "ASKS: Какой цвет?", "2. красный", "WANTS PERMISSION: npm test",
		"settings («Настройки»)", "light («Светлая»)", "setting enterSends"} {
		if !strings.Contains(s, w) {
			t.Errorf("prompt misses %q", w)
		}
	}
}

func TestMemoryRecallerFindsByStems(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj", "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pricing.md", "---\nname: pricing\ndescription: тарифная сетка продукта\n---\nТри тарифа: бесплатный, про, команда.")
	write("other.md", "---\nname: other\ndescription: деплой по пятницам запрещён\n---\nНе катим в пятницу.")
	got := MemoryRecaller(root)(context.Background(), "помнишь, мы обсуждали тарифы?")
	if len(got) != 1 || !strings.Contains(got[0], "Три тарифа") {
		t.Fatalf("got %v", got)
	}
}
