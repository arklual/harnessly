package peers

import "testing"

func TestPlain(t *testing.T) {
	one := Wrap(Envelope{ID: "m1", Person: "Глеб", Machine: "box", Chat: "Сборка", Ref: "box/c1", Via: "agent", Text: "готово"})
	two := one + "\n" + Wrap(Envelope{ID: "m2", Person: "Вера", Ref: "box/c2", Via: "agent", Text: "и тут"})
	cases := map[string]string{
		one:                             "От «Сборка»: готово",
		two:                             "От «Сборка»: готово\n\nОт «Вера»: и тут",
		"просто текст":                  "просто текст",
		`…ref="a/b" via="agent">привет`: `…ref="a/b" via="agent">привет`,
		`<peer-message id="x" chat="Ч">хвост`: "хвост",
		"текст </peer-message>":               "текст",
	}
	for in, want := range cases {
		if got := Plain(in); got != want {
			t.Errorf("Plain(%q) = %q, want %q", in, got, want)
		}
	}
}
