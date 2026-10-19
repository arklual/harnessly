package screen

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func single(n int) QuestionShape    { return QuestionShape{OptionCount: n} }
func multi(n int) QuestionShape     { return QuestionShape{OptionCount: n, MultiSelect: true} }
func previewed(n int) QuestionShape { return QuestionShape{OptionCount: n, Preview: true} }

func choice(picked []int, other string) QuestionChoice {
	return QuestionChoice{Picked: picked, Other: other}
}

func pick(p ...int) QuestionChoice { return choice(p, "") }

// flat writes keys as their names and text quoted.
func flat(shapes []QuestionShape, choices []QuestionChoice) [][]string {
	var out [][]string
	for _, group := range BuildAnswerSteps(shapes, choices) {
		g := []string{}
		for _, s := range group {
			if s.Literal {
				g = append(g, `"`+s.Value+`"`)
			} else {
				g = append(g, s.Value)
			}
		}
		out = append(out, g)
	}
	return out
}

func TestBuildAnswerSteps(t *testing.T) {
	cases := []struct {
		name    string
		shapes  []QuestionShape
		choices []QuestionChoice
		want    [][]string
	}{
		{"one single-choice question: its digit alone", []QuestionShape{single(3)}, []QuestionChoice{pick(1)}, [][]string{{"2"}}},
		{`"Other": the digit past the last option, the text, Enter`, []QuestionShape{single(3)}, []QuestionChoice{choice(nil, "свой вариант")},
			[][]string{{"4", `"свой вариант"`, "Enter"}}},
		{"every question in turn, then Submit answers", []QuestionShape{single(3), single(3), single(2)}, []QuestionChoice{pick(1), pick(0), pick(1)},
			[][]string{{"2"}, {"1"}, {"2"}, {"1"}}},
		{"multiple choice toggles each pick in order and moves on with Right", []QuestionShape{single(3), multi(3)}, []QuestionChoice{pick(1), pick(2, 0)},
			[][]string{{"2"}, {"1", "3", "Right"}, {"1"}}},
		{`multiple choice reaches "Other" with arrows`, []QuestionShape{multi(3)}, []QuestionChoice{choice([]int{0}, "ещё")},
			[][]string{{"1", "Down", "Down", "Down", `"ещё"`, "Down", "Enter"}, {"1"}}},
		{"a lone multiple-choice question ends in the review screen too", []QuestionShape{multi(2)}, []QuestionChoice{pick(1)},
			[][]string{{"2", "Right"}, {"1"}}},
		{"previews: Enter after the digit", []QuestionShape{previewed(3)}, []QuestionChoice{pick(0)}, [][]string{{"1", "Enter"}}},
		{"previews among other questions", []QuestionShape{multi(4), previewed(3)}, []QuestionChoice{pick(0, 1, 2, 3), pick(1)},
			[][]string{{"1", "2", "3", "4", "Right"}, {"2", "Enter"}, {"1"}}},
		{"multiple choice with previews keeps the plain layout", []QuestionShape{{OptionCount: 2, MultiSelect: true, Preview: true}}, []QuestionChoice{pick(1)},
			[][]string{{"2", "Right"}, {"1"}}},
	}
	for _, c := range cases {
		if got := flat(c.shapes, c.choices); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q", c.name, got)
		}
	}
}

// The keys the answer-question route sends for its requests.
func TestAnswerRouteKeys(t *testing.T) {
	questions := []QuestionShape{single(3), multi(2)}
	cases := []struct {
		shapes  []QuestionShape
		choices []QuestionChoice
		want    [][]string
	}{
		{questions, []QuestionChoice{pick(1), pick(0, 1)}, [][]string{{"2"}, {"1", "2", "Right"}, {"1"}}},
		{questions[:1], []QuestionChoice{choice(nil, "Enter; C-c")}, [][]string{{"4", `"Enter; C-c"`, "Enter"}}},
		{questions[:1], []QuestionChoice{pick(2)}, [][]string{{"3"}}},
		{[]QuestionShape{previewed(3)}, []QuestionChoice{pick(0)}, [][]string{{"1", "Enter"}}},
	}
	for i, c := range cases {
		if err := ValidateAnswerRequest(c.shapes, c.choices); err != nil {
			t.Errorf("%d: %v", i, err)
		}
		if got := flat(c.shapes, c.choices); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%d: got %q", i, got)
		}
	}

	// A page that sends no preview flag: a lone question reads the layout off the screen.
	question := fixture(t, "claude-ask-preview-question.txt")
	if !PreviewFromScreen(1, question) {
		t.Error("the preview layout is not read off the screen")
	}
	if PreviewFromScreen(2, question) || PreviewFromScreen(1, fixture(t, "claude-ask-multi-review.txt")) {
		t.Error("preview guessed for several questions or a screen without notes")
	}
	shape := QuestionShape{OptionCount: 3, Preview: PreviewFromScreen(1, question)}
	if got := flat([]QuestionShape{shape}, []QuestionChoice{pick(1)}); !reflect.DeepEqual(got, [][]string{{"2", "Enter"}}) {
		t.Errorf("got %q", got)
	}

	// Nothing is typed without a question on the screen.
	if !IsQuestionOnScreen(question) || IsQuestionOnScreen(fixture(t, "claude-ask-answered-idle.txt")) {
		t.Error("the route's question and idle screens are misread")
	}
	if ValidateAnswerRequest([]QuestionShape{previewed(3)}, []QuestionChoice{choice(nil, "своё")}) == nil {
		t.Error("free text next to previews is taken")
	}
	if ValidateAnswerRequest(questions, []QuestionChoice{pick(5), pick(0)}) == nil {
		t.Error("an option out of range is taken")
	}
}

func TestTakesFreeText(t *testing.T) {
	for _, c := range []struct {
		shape QuestionShape
		want  bool
	}{
		{single(3), true}, {multi(3), true}, {QuestionShape{OptionCount: 3, MultiSelect: true, Preview: true}, true}, {previewed(3), false},
	} {
		if got := TakesFreeText(c.shape); got != c.want {
			t.Errorf("%+v: got %v", c.shape, got)
		}
	}
}

func TestIsQuestionOnScreen(t *testing.T) {
	for name, want := range map[string]bool{
		"claude-ask-preview-question.txt":    true,
		"claude-ask-preview-after-digit.txt": true,
		"claude-ask-multi-preview-tab.txt":   true,
		"claude-ask-multi-review.txt":        true,
		"claude-ask-answered-idle.txt":       false,
		"claude-idle-placeholder.ansi.txt":   false,
		"claude-trust-folder.txt":            false,
		"claude-model-picker.txt":            false,
	} {
		if got := IsQuestionOnScreen(fixture(t, name)); got != want {
			t.Errorf("%s: got %v", name, got)
		}
	}
	// The words in the conversation above the prompt do not count.
	idle := strings.Replace(fixture(t, "claude-ask-answered-idle.txt"), "● done", "● Enter to select · ↑/↓ to navigate · Esc to cancel", 1)
	if IsQuestionOnScreen(idle) {
		t.Error("a quoted footer reads as the dialog")
	}
}

func TestWaitForQuestionGone(t *testing.T) {
	var slept []time.Duration
	sleep := func(d time.Duration) { slept = append(slept, d) }

	screens := []string{fixture(t, "claude-ask-multi-review.txt"), fixture(t, "claude-ask-answered-idle.txt")}
	read := func() (string, bool) {
		if len(screens) == 0 {
			return "", false
		}
		s := screens[0]
		screens = screens[1:]
		return s, true
	}
	if !WaitForQuestionGone(read, sleep) || !reflect.DeepEqual(slept, []time.Duration{AnswerConfirmPoll, AnswerConfirmPoll}) {
		t.Errorf("gone question: slept %v", slept)
	}

	slept = nil
	stays := fixture(t, "claude-ask-preview-after-digit.txt")
	if WaitForQuestionGone(func() (string, bool) { return stays, true }, sleep) || len(slept) <= 1 {
		t.Errorf("a question that stays reads as gone (slept %v)", slept)
	}
	if WaitForQuestionGone(func() (string, bool) { return "", false }, sleep) {
		t.Error("an unreadable screen reads as gone")
	}
}

func TestIsInstantAnswer(t *testing.T) {
	if !IsInstantAnswer([]QuestionShape{single(3)}) || IsInstantAnswer([]QuestionShape{multi(3)}) || IsInstantAnswer([]QuestionShape{single(3), single(3)}) {
		t.Error("only one single-choice question answers at once")
	}
}

type keyLog struct {
	log    []string
	failOn string
}

func (k *keyLog) Key(name string) error {
	if name == k.failOn {
		return errors.New("tmux gone")
	}
	k.log = append(k.log, "key:"+name)
	return nil
}
func (k *keyLog) Text(text string) error { k.log = append(k.log, "text:"+text); return nil }
func (k *keyLog) Sleep(d time.Duration) {
	k.log = append(k.log, fmt.Sprintf("sleep:%d", d.Milliseconds()))
}

func TestPlayAnswerSteps(t *testing.T) {
	io := &keyLog{}
	if err := PlayAnswerSteps(BuildAnswerSteps([]QuestionShape{single(3), single(3)}, []QuestionChoice{choice(nil, "x"), pick(0)}), io); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"key:4", "sleep:150", "text:x", "sleep:150", "key:Enter", "sleep:150", "sleep:350",
		"key:1", "sleep:150", "sleep:350",
		"key:1", "sleep:150", "sleep:350",
	}
	if !reflect.DeepEqual(io.log, want) {
		t.Errorf("got %q", io.log)
	}

	// tmux failing midway stops the keys (the route answers 500).
	failing := &keyLog{failOn: "Right"}
	if err := PlayAnswerSteps(BuildAnswerSteps([]QuestionShape{single(3), multi(2)}, []QuestionChoice{pick(0), pick(0)}), failing); err == nil {
		t.Error("the error is lost")
	}
	if !reflect.DeepEqual(failing.log, []string{"key:1", "sleep:150", "sleep:350", "key:1", "sleep:150"}) {
		t.Errorf("got %q", failing.log)
	}
}

func TestValidateAnswerRequest(t *testing.T) {
	ok := []struct {
		shapes  []QuestionShape
		choices []QuestionChoice
	}{
		{[]QuestionShape{single(3), multi(3)}, []QuestionChoice{pick(0), pick(1, 2)}},
		{[]QuestionShape{single(3)}, []QuestionChoice{choice(nil, "текст")}},
		{[]QuestionShape{previewed(3)}, []QuestionChoice{pick(2)}},
	}
	for i, c := range ok {
		if err := ValidateAnswerRequest(c.shapes, c.choices); err != nil {
			t.Errorf("%d: %v", i, err)
		}
	}
	bad := []struct {
		shapes  []QuestionShape
		choices []QuestionChoice
		want    string
	}{
		{nil, nil, "questions and choices must be the same non-empty length"},
		{[]QuestionShape{single(3)}, nil, "questions and choices must be the same non-empty length"},
		{[]QuestionShape{single(3)}, []QuestionChoice{pick(3)}, "question 1: option out of range"},
		{[]QuestionShape{single(3)}, []QuestionChoice{pick(0, 1)}, "question 1: exactly one answer expected"},
		{[]QuestionShape{single(3)}, []QuestionChoice{choice([]int{0}, "и текст")}, "question 1: exactly one answer expected"},
		{[]QuestionShape{multi(3)}, []QuestionChoice{pick()}, "question 1: no answer"},
		{[]QuestionShape{multi(3)}, []QuestionChoice{pick(1, 1)}, "question 1: repeated option"},
		{[]QuestionShape{single(0)}, []QuestionChoice{pick(0)}, "question 1: bad option count"},
		{[]QuestionShape{previewed(3)}, []QuestionChoice{choice(nil, "свой вариант")}, "question 1: no free text next to previews"},
		{[]QuestionShape{single(9)}, []QuestionChoice{pick(0)}, "question 1: bad option count"},
		{[]QuestionShape{single(2), single(2)}, []QuestionChoice{pick(0), pick(-1)}, "question 2: option out of range"},
	}
	for i, c := range bad {
		err := ValidateAnswerRequest(c.shapes, c.choices)
		if err == nil || err.Error() != c.want {
			t.Errorf("%d: got %v, want %q", i, err, c.want)
		}
	}
}

func TestIsChoiceComplete(t *testing.T) {
	cases := []struct {
		c       QuestionChoice
		otherOn bool
		want    bool
	}{
		{pick(), false, false}, {pick(0), false, true}, {choice([]int{0}, ""), true, false}, {choice(nil, "  "), true, false}, {choice(nil, "да"), true, true},
	}
	for i, c := range cases {
		if got := IsChoiceComplete(c.c, c.otherOn); got != c.want {
			t.Errorf("%d: got %v", i, got)
		}
	}
}

func TestParseAnswer(t *testing.T) {
	labels := []string{"Токен в шлюзе", "Тестовый вход, без Keycloak", "Да, но потом"}
	cases := []struct {
		answer string
		want   QuestionChoice
	}{
		{"Токен в шлюзе", choice([]int{0}, "")},
		{"Да, но потом, Токен в шлюзе", choice([]int{2, 0}, "")},
		{"Токен в шлюзе, свой вариант, с запятой", choice([]int{0}, "свой вариант, с запятой")},
		{"просто текст", choice([]int{}, "просто текст")},
		{"Тестовый вход, без Keycloak", choice([]int{1}, "")},
	}
	for _, c := range cases {
		if got := ParseAnswer(c.answer, labels); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %+v", c.answer, got)
		}
	}
}
