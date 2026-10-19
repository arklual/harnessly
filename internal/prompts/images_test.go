package prompts

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func picturePaths(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("/home/u/.app/uploads/ws/tab/%d-ab-shot %d.png", i+1, i+1)
	}
	return out
}

type tuiModel struct {
	pasteOverhead, perImage, capture time.Duration
}

// imageTUI is a terminal on the fake clock: every path of a paste becomes an [Image #n] after the paste is processed.
type imageTUI struct {
	m        tuiModel
	clock    *fakeClock
	busy     time.Time
	appearAt []time.Time
	pastes   []string
	existing int
}

var unescapedSpace = regexp.MustCompile(`[^\\] `)

func newImageTUI(m tuiModel, existing int) *imageTUI {
	c := newFakeClock()
	return &imageTUI{m: m, clock: c, busy: c.Now(), existing: existing}
}

func (u *imageTUI) paste(body string) error {
	u.pastes = append(u.pastes, body)
	count := len(unescapedSpace.FindAllStringIndex(body, -1)) + 1
	at := u.clock.Now()
	if u.busy.After(at) {
		at = u.busy
	}
	at = at.Add(u.m.pasteOverhead)
	for i := 0; i < count; i++ {
		at = at.Add(u.m.perImage)
		u.appearAt = append(u.appearAt, at)
	}
	u.busy = at
	return nil
}

func (u *imageTUI) capture() (string, bool) {
	u.clock.Sleep(u.m.capture)
	landed := u.existing
	for _, at := range u.appearAt {
		if !at.After(u.clock.Now()) {
			landed++
		}
	}
	refs := make([]string, landed)
	for i := range refs {
		refs[i] = fmt.Sprintf("[Image #%d]", i+1)
	}
	return strings.Join(refs, " "), true
}

var (
	phoneShots = tuiModel{pasteOverhead: 145 * time.Millisecond, perImage: 2 * time.Millisecond, capture: 40 * time.Millisecond}
	bigShots   = tuiModel{pasteOverhead: 900 * time.Millisecond, perImage: 450 * time.Millisecond, capture: 40 * time.Millisecond}
)

func escapedJoined(paths []string) string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = EscapePathForPrompt(p)
	}
	return strings.Join(out, " ")
}

func TestEscapePathForPrompt(t *testing.T) {
	if got := EscapePathForPrompt("/a b/c(1).png"); got != `/a\ b/c\(1\).png` {
		t.Fatalf("got %s", got)
	}
}

func TestSendImagePathsInOnePaste(t *testing.T) {
	u := newImageTUI(phoneShots, 0)
	paths := picturePaths(8)
	res := SendImagePaths(paths, true, u.paste, u.capture, u.clock)
	if !reflect.DeepEqual(u.pastes, []string{escapedJoined(paths)}) || !res.Confirmed || res.Attached != 8 || res.Expected != 8 {
		t.Fatalf("batch: %+v %d pastes", res, len(u.pastes))
	}

	u = newImageTUI(phoneShots, 3)
	if res := SendImagePaths(picturePaths(2), true, u.paste, u.capture, u.clock); !res.Confirmed || res.Attached != 2 {
		t.Fatalf("only new references: %+v", res)
	}

	slow := phoneShots
	slow.perImage = 9 * time.Second
	u = newImageTUI(slow, 0)
	if res := SendImagePaths(picturePaths(8), true, u.paste, u.capture, u.clock); res.Confirmed || res.Expected != 8 || res.Attached <= 0 || res.Attached >= 8 {
		t.Fatalf("partly landed: %+v", res)
	}

	never := phoneShots
	never.perImage = 100 * time.Second
	small, big := newImageTUI(never, 0), newImageTUI(never, 0)
	one := SendImagePaths(picturePaths(1), true, small.paste, small.capture, small.clock)
	many := SendImagePaths(picturePaths(8), true, big.paste, big.capture, big.clock)
	if many.Elapsed <= one.Elapsed {
		t.Fatalf("a bigger batch waits no longer: %v vs %v", many.Elapsed, one.Elapsed)
	}

	clock := newFakeClock()
	pastes := 0
	res = SendImagePaths(picturePaths(3), true, func(string) error { pastes++; return nil }, nil, clock)
	if pastes != 1 || len(clock.slept) != 1 || !res.Confirmed {
		t.Fatalf("without a screen: %d pastes, %d sleeps", pastes, len(clock.slept))
	}

	res = SendImagePaths(nil, true, func(string) error { t.Fatal("pasted nothing"); return nil }, nil, newFakeClock())
	if !res.Confirmed || res.Attached != 0 || res.Expected != 0 {
		t.Fatalf("no pictures: %+v", res)
	}
}

func TestSendImagePathsOnePerPicture(t *testing.T) {
	u := newImageTUI(phoneShots, 0)
	paths := picturePaths(3)
	res := SendImagePaths(paths, false, u.paste, u.capture, u.clock)
	want := []string{EscapePathForPrompt(paths[0]), EscapePathForPrompt(paths[1]), EscapePathForPrompt(paths[2])}
	if !reflect.DeepEqual(u.pastes, want) || !res.Confirmed || res.Attached != 3 {
		t.Fatalf("one by one: %+v", res)
	}
	never := phoneShots
	never.perImage = 100 * time.Second
	u = newImageTUI(never, 0)
	if res := SendImagePaths(paths, false, u.paste, u.capture, u.clock); len(u.pastes) != 1 || res.Confirmed || res.Attached != 0 || res.Expected != 3 {
		t.Fatalf("stops at the first: %+v %d pastes", res, len(u.pastes))
	}
}

func TestOnePasteIsFaster(t *testing.T) {
	measure := func(m tuiModel, batch bool) (time.Duration, int) {
		u := newImageTUI(m, 0)
		start := u.clock.Now()
		if !SendImagePaths(picturePaths(8), batch, u.paste, u.capture, u.clock).Confirmed {
			t.Fatal("not confirmed")
		}
		return u.clock.Now().Sub(start), len(u.pastes)
	}
	before, _ := measure(phoneShots, false)
	after, pastes := measure(phoneShots, true)
	if pastes != 1 || after*3 >= before {
		t.Errorf("small pictures: %v vs %v", after, before)
	}
	before, _ = measure(bigShots, false)
	after, _ = measure(bigShots, true)
	if after >= before {
		t.Errorf("large pictures: %v vs %v", after, before)
	}
}

func TestCountImageRefs(t *testing.T) {
	for screen, want := range map[string]int{
		"":                            0,
		"Hello world\nNo images here": 0,
		"────\n❯ [Image #1]\n────":                                  1,
		"❯ [Image #1] [Image #2] [Image #3] видишь?":                3,
		"[Image #10] [Image #99] [Image #123]":                      3,
		"[image #1]\n[Image#1]\n[Image # 1]\n[Image #1] valid":      1,
		"User: [Image #1] прошлый\n──\n❯ [Image #2] [Image #3]\n──": 3,
	} {
		if got := CountImageRefs(screen); got != want {
			t.Errorf("%q: %d", screen, got)
		}
	}
}

func TestWaitForImages(t *testing.T) {
	seq := func(screens ...string) (func() (string, bool), *int) {
		n := 0
		return func() (string, bool) {
			s := screens[min(n, len(screens)-1)]
			n++
			return s, s != "!"
		}, &n
	}
	never := func() (string, bool) { t.Fatal("captured"); return "", false }
	if w := WaitForImages(never, 0, 0, time.Second, 100*time.Millisecond, newFakeClock()); !w.Confirmed || w.Attempts != 0 {
		t.Errorf("nothing expected: %+v", w)
	}
	c, _ := seq("❯ [Image #1] [Image #2]")
	if w := WaitForImages(c, 2, 0, 5*time.Second, 100*time.Millisecond, newFakeClock()); !w.Confirmed || w.Attempts != 1 || w.FinalCount != 2 {
		t.Errorf("first look: %+v", w)
	}
	c, _ = seq("User: [Image #1]\n❯ [Image #2]")
	if w := WaitForImages(c, 1, 1, 5*time.Second, 100*time.Millisecond, newFakeClock()); !w.Confirmed || w.Attempts != 1 {
		t.Errorf("baseline: %+v", w)
	}
	c, _ = seq("❯ ", "❯ ", "❯ [Image #1]")
	if w := WaitForImages(c, 1, 0, 5*time.Second, 100*time.Millisecond, newFakeClock()); !w.Confirmed || w.Attempts != 3 || w.FinalCount != 1 {
		t.Errorf("while polling: %+v", w)
	}
	c, n := seq("❯ ")
	if w := WaitForImages(c, 1, 0, 200*time.Millisecond, 100*time.Millisecond, newFakeClock()); w.Confirmed || w.FinalCount != 0 || *n == 0 {
		t.Errorf("timeout: %+v", w)
	}
	c, _ = seq("!", "❯ [Image #1]")
	if w := WaitForImages(c, 1, 0, 5*time.Second, 100*time.Millisecond, newFakeClock()); !w.Confirmed || w.Attempts != 2 {
		t.Errorf("unreadable screen retried: %+v", w)
	}
	c, _ = seq("❯ [Image #1]", "❯ [Image #1] [Image #2]", "❯ [Image #1] [Image #2] [Image #3]")
	if w := WaitForImages(c, 3, 0, 5*time.Second, 100*time.Millisecond, newFakeClock()); !w.Confirmed || w.Attempts != 3 || w.FinalCount != 3 {
		t.Errorf("all of several: %+v", w)
	}
}
