package locale

import "testing"

func TestPick(t *testing.T) {
	msg := T("Tab not found", "Вкладка не найдена")
	t.Setenv("RUNTIME_LOCALE", "")
	if msg.String() != "Tab not found" || Russian() {
		t.Fatalf("default: %q", msg)
	}
	t.Setenv("RUNTIME_LOCALE", "de")
	if msg.String() != "Tab not found" {
		t.Fatalf("an unknown locale is English: %q", msg)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if msg.String() != "Вкладка не найдена" || Pick("a", "б") != "б" {
		t.Fatalf("ru: %q", msg)
	}
}
