package apps

import (
	"strings"
	"testing"
)

// The chat's own interface an app puts into: starters, buttons at the field and link cards.
const withChatHost = `"chat": { "cards": ["card"], "commands": ["/cashback"],
    "starters": [{ "text": "Проверь кешбэк в макете" }],
    "buttons": [{ "id": "open", "title": "Кешбэки", "icon": "percent", "view": "main" }, { "id": "run", "title": "Проверить", "command": "/cashback" }],
    "links": [{ "match": "https://*.bank.example/cashback/*", "card": "card" }],
    "mentions": { "title": "Кешбэки", "tool": "bank-cashback.find" } }`

func chatHostManifest(t *testing.T, from, to string) Problems {
	t.Helper()
	raw := strings.Replace(cashback, `"chat": { "cards": ["card"] }`, withChatHost, 1)
	if raw == cashback {
		t.Fatal("the fixture did not change")
	}
	if from != "" {
		changed := strings.Replace(raw, from, to, 1)
		if changed == raw {
			t.Fatalf("the case does not change the manifest: %s", from)
		}
		raw = changed
	}
	_, ps := ParseManifest([]byte(raw))
	return ps
}

func TestChatHostParts(t *testing.T) {
	if ps := chatHostManifest(t, "", ""); len(ps.Errors()) != 0 {
		t.Fatalf("a good manifest: %v", ps)
	}
	cases := []struct{ name, from, to, path string }{
		{"empty starter", `"Проверь кешбэк в макете"`, `"  "`, "chat.starters[0].text"},
		{"long starter", `"Проверь кешбэк в макете"`, `"` + strings.Repeat("я", maxStarter+1) + `"`, "chat.starters[0].text"},
		{"button with both", `"view": "main" }`, `"view": "main", "command": "/cashback" }`, "chat.buttons[0]"},
		{"button with none", `, "view": "main" }`, ` }`, "chat.buttons[0]"},
		{"unknown command", `"command": "/cashback" }]`, `"command": "/nope" }]`, "chat.buttons[1].command"},
		{"view not in the pane", `"view": "main" }`, `"view": "card" }`, "chat.buttons[0].view"},
		{"same id", `"id": "run"`, `"id": "open"`, "chat.buttons[1].id"},
		{"no title", `"title": "Проверить"`, `"title": ""`, "chat.buttons[1].title"},
		{"plain http link", `"https://*.bank.example/cashback/*"`, `"http://bank.example/*"`, "chat.links[0].match"},
		{"mask in the middle", `"https://*.bank.example/cashback/*"`, `"https://bank.example/*/x"`, "chat.links[0].match"},
		{"a whole zone", `"https://*.bank.example/cashback/*"`, `"https://*.co.uk/*"`, "chat.links[0].match"},
		{"mention without title", `"title": "Кешбэки", "tool"`, `"title": "", "tool"`, "chat.mentions.title"},
		{"mention tool without server", `"tool": "bank-cashback.find"`, `"tool": "find"`, "chat.mentions.tool"},
		{"card not a chat card", `"card": "card" }]`, `"card": "main" }]`, "chat.links[0].card"},
	}
	for _, c := range cases {
		if problemAt(chatHostManifest(t, c.from, c.to), c.path) == nil {
			t.Errorf("%s: want a problem at %s", c.name, c.path)
		}
	}
}

func TestChatHostContributions(t *testing.T) {
	raw := strings.Replace(cashback, `"chat": { "cards": ["card"] }`, withChatHost, 1)
	m, ps := ParseManifest([]byte(raw))
	if len(ps.Errors()) != 0 {
		t.Fatal(ps)
	}
	kinds := map[string]int{}
	for _, c := range ContributionsOf(m) {
		kinds[c.Kind]++
	}
	if kinds["chat.starter"] != 1 || kinds["chat.button"] != 2 || kinds["chat.link"] != 1 || kinds["chat.mention"] != 1 {
		t.Fatalf("contributions: %v", kinds)
	}
}
