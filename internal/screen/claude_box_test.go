package screen

import "testing"

func TestClaudeStillInBox(t *testing.T) {
	long := "Хочу новый диалог тоже уметь создавать по дизайну тому же, и чтобы он открывался сразу"
	cases := []struct {
		input, text string
		want        bool
	}{
		{"", "Как дела", false},
		{"Как дела", "Как дела", true},
		{"[Image #1] Как дела", "Как дела", true},
		{"[Pasted text #1 +12 lines]", "много строк", true},
		{"Хочу новый диалог тоже уметь", long, true},
		{"half-typed text", "x", false},
		{"Как", "Как дела, что нового", false},
		{"другое сообщение", "Как дела", false},
	}
	for _, c := range cases {
		if got := ClaudeStillInBox(c.input, c.text); got != c.want {
			t.Errorf("ClaudeStillInBox(%q, %q) = %v, want %v", c.input, c.text, got, c.want)
		}
	}
}

func TestClaudeStillInBoxPastAReferenceWrappedMidway(t *testing.T) {
	if !ClaudeStillInBox("[Image #100] [Image   #101] длинный текст сообщения", "длинный текст сообщения") {
		t.Error("own text behind a wrapped reference not seen")
	}
}
