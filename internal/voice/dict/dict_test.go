package dict

import (
	"reflect"
	"strings"
	"testing"
)

var brands = []Entry{{Word: "Sinopia"}, {Word: "harnessly"}, {Word: "Tiksly"}, {Word: "Kubernetes"}, {Word: "Claude"}}

func TestApply(t *testing.T) {
	cases := []struct {
		name, in, want string
		d              []Entry
	}{
		{"inflected", "открой синопию в харнесли", "открой Sinopia в harnessly", brands},
		{"sinopia forms", "синопия синопии синопией синопию", "Sinopia Sinopia Sinopia Sinopia", brands},
		{"harnessly", "харнесли и харнеслы", "harnessly и harnessly", brands},
		{"tiksly", "тиксли и тикслай", "Tiksly и Tiksly", brands},
		{"kubernetes", "кубернетис кубернетес", "Kubernetes Kubernetes", brands},
		{"claude", "спроси клод", "спроси Claude", brands},
		{"claude inflected", "спроси клода", "спроси Claude", brands},
		{"split word", "в хар несли", "в harnessly", brands},
		{"comma", "это синопию, да", "это Sinopia, да", brands},
		{"upper", "СИНОПИЯ", "Sinopia", brands},
		{"mixed latin", "open Sinopia и харнесли", "open Sinopia и harnessly", brands},
		{"spacing kept", "  синопию \n  харнесли ", "  Sinopia \n  harnessly ", brands},
		{"prefix left alone", "открой синоп", "открой синоп", brands},
		{"empty dict", "открой синопию", "открой синопию", nil},
		{"empty text", "", "", brands},
		{"variants", "сенопия и синопиа", "Sinopia и Sinopia", []Entry{{Word: "Sinopia", Heard: []string{"синопиа", "сенопия"}}}},
		{"multiword variant", "это Хар Несли тут", "это harnessly тут", []Entry{{Word: "harnessly", Heard: []string{"хар несли"}}}},
		{"longest first", "хар несли", "A", []Entry{{Word: "B", Heard: []string{"хар"}}, {Word: "A", Heard: []string{"хар несли"}}}},
		{"yo", "всё", "ВСЁ", []Entry{{Word: "ВСЁ", Heard: []string{"все"}}}},
		{"cyrillic word", "позвони рома, рома", "позвони Рома, Рома", []Entry{{Word: "Рома", Heard: []string{"рома"}}}},
		{"cyrillic word no phonetic", "рамо", "рамо", []Entry{{Word: "Рома"}}},
		{"case kept as in dict", "Синопия открыта", "Sinopia открыта", brands},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Apply(c.in, c.d)
			if got != c.want {
				t.Fatalf("Apply(%q) = %q, want %q", c.in, got, c.want)
			}
			if again := Apply(got, c.d); again != got {
				t.Fatalf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestApplyNegatives(t *testing.T) {
	words := strings.Fields(`синтаксис писать харизма открой тикать клад кубик класс хорошо сейчас
		синий пиния похожий тиски линия клоун кубики синица пинать тикет хвост кусок кубок
		клан клапан клад кладу складе харчо крупно сенсация сильно тихо тихий классы`)
	for _, w := range words {
		if got := Apply(w, brands); got != w {
			t.Errorf("%q must stay, got %q", w, got)
		}
		if got := Apply("это "+w+" там.", brands); got != "это "+w+" там." {
			t.Errorf("%q in sentence changed: %q", w, got)
		}
	}
}

func TestIdempotentSentence(t *testing.T) {
	in := "синопию и харнесли в кубернетис, клод тикслай."
	once := Apply(in, brands)
	if twice := Apply(once, brands); twice != once {
		t.Fatalf("%q != %q", twice, once)
	}
}

func TestClean(t *testing.T) {
	long := strings.Repeat("a", MaxWordLen+1)
	var heard []string
	for i := 0; i < 20; i++ {
		heard = append(heard, strings.Repeat("б", i+1))
	}
	got := Clean([]Entry{
		{Word: "  Sinopia ", Heard: []string{" синопия ", "синопия", ""}},
		{Word: "sinopia", Heard: []string{"сенопия"}},
		{Word: ""}, {Word: "   "}, {Word: long},
		{Word: "X", Heard: heard},
	})
	if len(got) != 2 || got[0].Word != "Sinopia" || !reflect.DeepEqual(got[0].Heard, []string{"синопия", "сенопия"}) {
		t.Fatalf("unexpected: %#v", got)
	}
	if len(got[1].Heard) != 10 {
		t.Fatalf("heard not capped: %d", len(got[1].Heard))
	}
	var many []Entry
	for i := 0; i < MaxEntries+50; i++ {
		many = append(many, Entry{Word: strings.Repeat("w", i%60+1) + string(rune('a'+i/60))})
	}
	if n := len(Clean(many)); n > MaxEntries {
		t.Fatalf("entries not capped: %d", n)
	}
}

func TestHints(t *testing.T) {
	got := Hints([]Entry{{Word: " A "}, {Word: "a"}, {Word: ""}, {Word: "B"}})
	if !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("got %v", got)
	}
}

func TestAmbiguousLeftAlone(t *testing.T) {
	d := []Entry{{Word: "Sinopia"}, {Word: "Sinopio"}}
	if got := Apply("синопия", d); got == "Sinopio" {
		t.Fatalf("ambiguous match replaced: %q", got)
	}
}
