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

// A message too long for the box (tab-dBSEBV): Claude shows the rows around the cursor, so the ❯ row starts in
// the middle of the message. The text is still ours, and is never typed twice.
func TestClaudeStillInBoxClippedToItsTail(t *testing.T) {
	msg := "Вот. Потом следующую задачу, которую хочется, чтобы ты тоже сделал, это управление агентом Сайбаром. " +
		"Также, если он работает над каким-то дизайном, правит какой-то архитектурный документ Ну, именно артефакты, либо " +
		"это схема какая-то. Пусть он мне их открывает. Вот. С анимацией самостоятельно пусть мне откроет. Чё там? Как работает?"
	tail := "именно артефакты, либо это схема какая-то. Пусть он мне их от   крывает. Вот. С анимацией самостоятельно пусть " +
		"мне откроет. Чё там? Как работает? "
	cases := []struct {
		name, input string
		want        bool
	}{
		{"the box shows its tail, wrapped mid-word", tail, true},
		{"a piece in the middle", "Также, если он работает над каким-то дизайном", true},
		{"a short piece is no proof", "Как работает?", false},
		{"someone else's long draft", "именно артефакты, либо это схема другая совсем, не наша", false},
	}
	for _, c := range cases {
		if got := ClaudeStillInBox(c.input, msg); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}
