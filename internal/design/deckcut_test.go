package design

import (
	"reflect"
	"strings"
	"testing"
)

func i64(v int64) *int64 { return &v }

func TestSpansMinus(t *testing.T) {
	cases := []struct {
		name      string
		spans     []deckSpan
		cut, want []deckSpan
	}{
		{"nothing", []deckSpan{{0, 100}}, nil, []deckSpan{{0, 100}}},
		{"middle", []deckSpan{{0, 100}}, []deckSpan{{40, 60}}, []deckSpan{{0, 40}, {60, 100}}},
		{"edges", []deckSpan{{0, 100}}, []deckSpan{{0, 10}, {90, 100}}, []deckSpan{{10, 90}}},
		{"all", []deckSpan{{0, 100}}, []deckSpan{{-5, 200}}, []deckSpan{}},
		{"overlapping cuts", []deckSpan{{0, 100}}, []deckSpan{{50, 70}, {10, 55}}, []deckSpan{{0, 10}, {70, 100}}},
		{"two spans", []deckSpan{{0, 50}, {80, 120}}, []deckSpan{{40, 100}}, []deckSpan{{0, 40}, {100, 120}}},
	}
	for _, c := range cases {
		if got := spansMinus(c.spans, c.cut); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMapOut(t *testing.T) {
	keep := []deckSpan{{0, 1000}, {3000, 4000}, {6000, 7000}}
	for at, want := range map[int64]int64{0: 0, 500: 500, 1000: 1000, 2000: 1000, 3000: 1000, 3500: 1500, 5000: 2000, 6500: 2500, 9000: 3000} {
		if got := mapOut(keep, at); got != want {
			t.Errorf("mapOut(%d) = %d, want %d", at, got, want)
		}
	}
}

func TestTakesLastTakeWins(t *testing.T) {
	slides := []string{"A", "B", "C"}
	evs := []DeckEvent{
		{T: 5000, K: DeckEvSlide, Page: "C"},
		{T: 1000, K: DeckEvSlide, Page: "B"},
		{T: 3000, K: DeckEvRetake, Page: "B"},
		{T: 4000, K: DeckEvCursor},
	}
	takes := deckTakes(slides, evs, 8000)
	want := []DeckTake{
		{"A", 1, 0, 1000},
		{"B", 1, 1000, 3000},
		{"B", 2, 3000, 5000},
		{"C", 1, 5000, 8000},
	}
	if !reflect.DeepEqual(takes, want) {
		t.Fatalf("takes = %+v, want %+v", takes, want)
	}
	if got := supersededSpans(takes); !reflect.DeepEqual(got, []deckSpan{{1000, 3000}}) {
		t.Errorf("superseded = %v", got)
	}
	if got := retakenSlides(slides, takes); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("retaken = %v", got)
	}
}

func TestTakesRevisitWithoutRetakeKeepsTheTake(t *testing.T) {
	takes := deckTakes([]string{"A", "B"}, []DeckEvent{
		{T: 1000, K: DeckEvSlide, Page: "B"}, {T: 2000, K: DeckEvSlide, Page: "A"}, {T: 3000, K: DeckEvSlide, Page: "B"},
	}, 4000)
	if len(supersededSpans(takes)) != 0 {
		t.Fatalf("a revisit is not a retake: %+v", takes)
	}
	if len(takes) != 4 {
		t.Fatalf("takes = %+v", takes)
	}
}

func TestSplitSpeechFindsFillers(t *testing.T) {
	parts := splitSpeech("Ну, давайте как бы начнём э-э с главного, вот.", 1000, 5000)
	var kinds, texts []string
	for _, p := range parts {
		kinds = append(kinds, string(p.Kind))
		texts = append(texts, p.Text)
	}
	wantKinds := []string{"filler", "word", "filler", "word", "filler", "word", "filler"}
	wantTexts := []string{"Ну,", "давайте", "как бы", "начнём", "э-э", "с главного,", "вот."}
	if !reflect.DeepEqual(kinds, wantKinds) || !reflect.DeepEqual(texts, wantTexts) {
		t.Fatalf("kinds %v texts %v", kinds, texts)
	}
	at := int64(1000)
	for _, p := range parts {
		if p.T0 != at || p.T1 <= p.T0 || p.Cut != (p.Kind == DeckPartFiller) {
			t.Errorf("part %+v after %d", p, at)
		}
		at = p.T1
	}
	if at != 5000 {
		t.Errorf("the parts end at %d", at)
	}
	if got := splitSpeech("   ", 0, 10); len(got) != 0 {
		t.Errorf("empty text: %v", got)
	}
	// "Как" alone and a word that only contains a filler are words.
	for _, text := range []string{"как дела", "ночной вотум"} {
		for _, p := range splitSpeech(text, 0, 1000) {
			if p.Kind != DeckPartWord {
				t.Errorf("%q: %+v", text, p)
			}
		}
	}
}

func TestParseSilences(t *testing.T) {
	out := `[silencedetect @ 0x1] silence_start: -0.002
[silencedetect @ 0x1] silence_end: 1.5 | silence_duration: 1.502
size=N/A
[silencedetect @ 0x1] silence_start: 10.25
[silencedetect @ 0x1] silence_end: 12 | silence_duration: 1.75
[silencedetect @ 0x1] silence_start: 19.2
`
	want := []deckSpan{{0, 1500}, {10250, 12000}, {19200, 20000}}
	if got := parseSilences(out, 20000); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func transcriptFixture() ([]string, []DeckEvent, []DeckTake, DeckTranscript) {
	slides := []string{"A", "B"}
	evs := []DeckEvent{
		{T: 2000, K: DeckEvASR, T0: i64(500), Text: "ну вот первый слайд"},
		{T: 6000, K: DeckEvSlide, Page: "B"},
		{T: 9000, K: DeckEvASR, T0: i64(7000), Text: "про второй"},
		{T: 11000, K: DeckEvRetake, Page: "B"},
		{T: 14000, K: DeckEvASR, T0: i64(11500), Text: "второй заново"},
	}
	takes := deckTakes(slides, evs, 15000)
	silences := []deckSpan{{0, 500}, {2000, 4500}, {9500, 11500}, {12000, 12400}}
	return slides, evs, takes, buildTranscript(slides, evs, takes, silences, 15000)
}

func TestBuildTranscript(t *testing.T) {
	_, _, _, tr := transcriptFixture()
	type row struct {
		T0, T1 int64
		Page   string
		Kind   DeckPartKind
	}
	var got []row
	for _, s := range tr.Segments {
		got = append(got, row{s.T0, s.T1, s.Page, s.Parts[0].Kind})
	}
	// The take of slide B before the retake (7000..11000) is gone with its speech and its pause.
	want := []row{
		{500, 2000, "A", DeckPartFiller},
		{2250, 4250, "A", DeckPartPause},
		{11500, 14000, "B", DeckPartWord},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("segments %+v, want %+v", got, want)
	}
	if first := tr.Segments[0]; len(first.Parts) != 2 || first.Parts[0].Text != "ну вот" || !first.Parts[0].Cut || first.Parts[1].Text != "первый слайд" {
		t.Errorf("first segment %+v", first)
	}
	if p := tr.Segments[1].Parts[0]; !p.Cut || !strings.Contains(p.Text, "2,0") && !strings.Contains(p.Text, "2.0") {
		t.Errorf("pause part %+v", p)
	}
}

func TestKeepAndCutMs(t *testing.T) {
	_, _, takes, tr := transcriptFixture()
	edit := DefaultDeckEdit()
	keep, cut := deckKeep(15000, takes, tr, edit)
	// Computed by hand: whole 0..15000; superseded B take is 6000..11000.
	// Cuts: filler "ну вот" in the first segment, the pause 2250..4250.
	first := tr.Segments[0].Parts[0]
	wantKeep2 := spansMinus(spansMinus([]deckSpan{{0, 15000}}, []deckSpan{{6000, 11000}}), []deckSpan{{first.T0, first.T1}, {2250, 4250}})
	if !reflect.DeepEqual(keep, wantKeep2) {
		t.Fatalf("keep %v, want %v", keep, wantKeep2)
	}
	if want := (first.T1 - first.T0) + 2000; cut != want {
		t.Errorf("cutMs = %d, want %d", cut, want)
	}
	// Keeping the filler gives its time back.
	edit.Kept = []string{"0.0"}
	_, cut2 := deckKeep(15000, takes, tr, edit)
	if cut2 != 2000 {
		t.Errorf("cutMs with the filler kept = %d", cut2)
	}
	// The cut off cuts nothing, but the old take is still gone.
	edit = DefaultDeckEdit()
	edit.Cut = false
	keep3, cut3 := deckKeep(15000, takes, tr, edit)
	if cut3 != 0 || spansLen(keep3) != 10000 {
		t.Errorf("cut off: %v cut %d", keep3, cut3)
	}
}
