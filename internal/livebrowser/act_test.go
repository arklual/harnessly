package livebrowser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

const actPage = `<!doctype html><title>Форма</title><body>
<h1>Поиск</h1>
<form action="/found" method="get">
  <label>Запрос <input name="q" autofocus></label>
  <input type="password" name="pw" placeholder="Пароль">
  <select name="city"><option value="msk">Москва</option><option value="spb">Петербург</option></select>
  <input type="checkbox" name="agree" id="agree"><label for="agree">Согласен</label>
  <button type="submit">Найти</button>
</form>
<a href="/two">Вторая</a>
<button id="later" onclick="setTimeout(() => { document.body.insertAdjacentHTML('beforeend', '<p>Появилось</p>') }, 300)">Потом</button>
<div style="position: relative; width: 200px; height: 40px"><button id="under">Накрытая</button><div style="position: absolute; inset: 0; background: rgba(0,0,0,.2)"></div></div>
<iframe id="frame" src="about:blank" title="Рамка" style="width: 100px; height: 30px"></iframe>
<button id="mover" onmouseover="this.style.visibility='hidden'">Убегает</button>
<button id="focusframe" onclick="document.getElementById('frame').focus()">В рамку</button>
<input name="shy" oninput="this.blur()" placeholder="Робкое">
<div style="height: 3000px"></div>
<a href="/bottom" id="bottom">Внизу</a>
</body>`

func TestAgentActsOnThePage(t *testing.T) {
	s := chromeOrSkip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/found":
			_, _ = w.Write([]byte(`<title>Нашлось</title><body><h1>Результаты: ` + r.URL.Query().Get("q") + `</h1><a href="/">Назад</a></body>`))
		case "/two":
			_, _ = w.Write([]byte(`<title>Вторая</title><body><h1>Вторая страница</h1></body>`))
		default:
			_, _ = w.Write([]byte(actPage))
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tab, err := s.Open(ctx, "act", 800, 600, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer tab.Close()
	if err := tab.Navigate(srv.URL + "/"); err != nil {
		t.Fatal(err)
	}
	settle := func(want string) Info {
		deadline := time.Now().Add(10 * time.Second)
		var info Info
		for time.Now().Before(deadline) {
			info, _ = tab.Info()
			if strings.Contains(info.Title, want) && !info.Loading {
				return info
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("waiting for %q: %+v", want, info)
		return info
	}
	settle("Форма")

	// A snapshot lists what can be acted on, with refs; the password field is marked, not valued.
	snap := tab.Act(ctx, Act{ID: "1", Op: "snapshot"})
	if !snap.OK || !strings.Contains(snap.Text, "url: "+srv.URL+"/") || !strings.Contains(snap.Text, `- heading "Поиск" level=1`) {
		t.Fatalf("snapshot: %+v", snap)
	}
	ref := func(line string) string {
		for _, l := range strings.Split(snap.Text, "\n") {
			if strings.Contains(l, line) {
				start := strings.Index(l, "[e")
				end := strings.Index(l[start:], "]")
				return l[start+1 : start+end]
			}
		}
		t.Fatalf("no line %q in snapshot:\n%s", line, snap.Text)
		return ""
	}
	if !strings.Contains(snap.Text, `- textbox "Пароль" [`) || !strings.Contains(snap.Text, "] password") {
		t.Fatalf("password field: %s", snap.Text)
	}
	// The password field refuses typing.
	if r := tab.Act(ctx, Act{ID: "2", Op: "type", Ref: ref(`"Пароль"`), Text: "x"}); r.OK || !strings.Contains(r.Text, "the person types it themselves") {
		t.Fatalf("typing into the password: %+v", r)
	}
	// A key that types does not go into a password field; one that does not type does.
	if r := tab.Act(ctx, Act{ID: "2a", Op: "click", Ref: ref(`"Пароль"`)}); !r.OK {
		t.Fatalf("focusing the password: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "2b", Op: "press", Key: "a"}); r.OK || !strings.Contains(r.Text, "the person types it themselves") {
		t.Fatalf("a letter into the password: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "2b2", Op: "press", Key: "Control+v"}); r.OK || !strings.Contains(r.Text, "the person types it themselves") {
		t.Fatalf("a paste into the password: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "2b3", Op: "press", Key: "Backspace"}); r.OK {
		t.Fatalf("a backspace in the password: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "2c", Op: "press", Key: "Tab"}); !r.OK {
		t.Fatalf("tab out of the password: %+v", r)
	}
	// An element under a layer of the page is not clicked.
	if r := tab.Act(ctx, Act{ID: "2d", Op: "click", Ref: ref(`button "Накрытая"`)}); r.OK || !strings.Contains(r.Text, "covered") {
		t.Fatalf("a covered element: %+v", r)
	}
	// Keys are not sent into a frame, whatever they are: its document cannot be read from here.
	if r := tab.Act(ctx, Act{ID: "2d1", Op: "click", Ref: ref(`button "В рамку"`)}); !r.OK {
		t.Fatalf("focusing the frame: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "2d2", Op: "press", Key: "Tab"}); r.OK || !strings.Contains(r.Text, "frame") {
		t.Fatalf("a key into the frame: %+v", r)
	}
	// An element that leaves the point between the preparation and the click is not clicked.
	if r := tab.Act(ctx, Act{ID: "2d3", Op: "click", Ref: ref(`button "Убегает"`)}); r.OK || !strings.Contains(r.Text, "moved or got covered") {
		t.Fatalf("a moving element: %+v", r)
	}
	// The site the person allowed is the one acted on.
	if r := tab.Act(ctx, Act{ID: "2e", Op: "snapshot", Host: "other.test"}); r.OK || !strings.Contains(r.Text, "another site now (127.0.0.1)") {
		t.Fatalf("another site: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "2f", Op: "snapshot", Host: "127.0.0.1"}); !r.OK {
		t.Fatalf("the allowed site: %+v", r)
	}
	// A select takes an option by its text.
	if r := tab.Act(ctx, Act{ID: "3", Op: "select", Ref: ref("combobox"), Value: "Петербург"}); !r.OK || !strings.Contains(r.Text, `selected "Петербург"`) || r.Rect == nil {
		t.Fatalf("select: %+v", r)
	}
	// A click on the checkbox checks it: the browser's own click.
	if r := tab.Act(ctx, Act{ID: "4", Op: "click", Ref: ref(`checkbox "Согласен"`)}); !r.OK || !strings.HasPrefix(r.Text, "clicked checkbox") {
		t.Fatalf("click: %+v", r)
	}
	var checked bool
	if err := tab.Evaluate(`document.getElementById('agree').checked`, &checked); err != nil || !checked {
		t.Fatalf("checkbox after the click: %v %v", checked, err)
	}
	// A click that adds text later; wait finds it.
	if r := tab.Act(ctx, Act{ID: "5", Op: "click", Ref: ref(`button "Потом"`)}); !r.OK {
		t.Fatalf("click later: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "6", Op: "wait", Text: "Появилось", MS: 3000}); !r.OK {
		t.Fatalf("wait: %+v", r)
	}
	// Scroll by a ref brings the bottom link into view; by dy moves the page.
	if r := tab.Act(ctx, Act{ID: "7", Op: "scroll", Ref: ref(`link "Внизу"`)}); !r.OK || !strings.HasPrefix(r.Text, "scrolled to y=") || r.Rect == nil {
		t.Fatalf("scroll ref: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "8", Op: "scroll", DY: -10000}); !r.OK || !strings.HasPrefix(r.Text, "scrolled to y=0") {
		t.Fatalf("scroll dy: %+v", r)
	}
	// Hover gives the element's box.
	if r := tab.Act(ctx, Act{ID: "9", Op: "hover", Ref: ref(`link "Вторая"`)}); !r.OK || r.Rect == nil || !strings.HasPrefix(r.Text, "hovered link") {
		t.Fatalf("hover: %+v", r)
	}
	// A press of Tab moves the focus; the box of what is focused comes back.
	if r := tab.Act(ctx, Act{ID: "10", Op: "press", Key: "Tab"}); !r.OK || r.Rect == nil {
		t.Fatalf("press: %+v", r)
	}
	// A field that loses the focus before the Enter of a submit gets no Enter.
	if r := tab.Act(ctx, Act{ID: "10a", Op: "type", Ref: ref(`textbox "Робкое"`), Text: "x", Submit: true}); r.OK || !strings.Contains(r.Text, "lost the focus") {
		t.Fatalf("submit after the focus left: %+v", r)
	}
	// Typing with submit sends the form: the page moves, the answer says so.
	r := tab.Act(ctx, Act{ID: "11", Op: "type", Ref: ref(`textbox "Запрос"`), Text: "билеты", Submit: true})
	if !r.OK || !strings.Contains(r.Text, "typed into textbox") || !strings.Contains(r.Text, "pressed Enter") {
		t.Fatalf("type: %+v", r)
	}
	settle("Нашлось")
	// Old refs are gone with the old document; a new snapshot gives new ones.
	if r := tab.Act(ctx, Act{ID: "12", Op: "click", Ref: "e1"}); r.OK || !strings.Contains(r.Text, "take a snapshot") {
		t.Fatalf("stale ref: %+v", r)
	}
	snap = tab.Act(ctx, Act{ID: "13", Op: "snapshot"})
	if !snap.OK || !strings.Contains(snap.Text, `heading "Результаты: билеты"`) {
		t.Fatalf("snapshot after submit: %+v", snap)
	}
	// A click on a link moves the page and the answer tells where.
	if r := tab.Act(ctx, Act{ID: "14", Op: "click", Ref: ref(`link "Назад"`)}); !r.OK || !strings.Contains(r.Text, "the page is now "+srv.URL+"/") {
		t.Fatalf("click link: %+v", r)
	}
	settle("Форма")
	// Site tools are not here; an unknown op is refused.
	if r := tab.Act(ctx, Act{ID: "15", Op: "tools"}); !r.OK || !strings.Contains(r.Text, "no tools") {
		t.Fatalf("tools: %+v", r)
	}
	if r := tab.Act(ctx, Act{ID: "16", Op: "dance"}); r.OK {
		t.Fatalf("unknown op: %+v", r)
	}
}

func TestKeyOf(t *testing.T) {
	for chord, want := range map[string]keyPress{
		"a":             {key: "a", code: "KeyA", text: "a", vk: 65},
		"Enter":         {key: "Enter", code: "Enter", text: "\r", vk: 13},
		"Control+a":     {key: "a", code: "KeyA", vk: 65, modifiers: 2},
		"Shift+Tab":     {key: "Tab", code: "Tab", vk: 9, modifiers: 8},
		"Meta+Shift+z":  {key: "z", code: "KeyZ", vk: 90, modifiers: 12},
		" ":             {key: " ", code: "Space", text: " ", vk: 32},
		"ArrowDown":     {key: "ArrowDown", code: "ArrowDown", vk: 40},
		"+":             {key: "+", code: "+", text: "+", vk: '+'},
		"Alt+ArrowLeft": {key: "ArrowLeft", code: "ArrowLeft", vk: 37, modifiers: 1},
	} {
		if got := keyOf(chord); got != want {
			t.Errorf("%q: got %+v, want %+v", chord, got, want)
		}
	}
}

func TestMovesOnly(t *testing.T) {
	for chord, want := range map[string]bool{
		"Tab": true, "Shift+Tab": true, "Escape": true, "Enter": true, "ArrowDown": true, "End": true,
		"a": false, " ": false, "Backspace": false, "Delete": false, "Control+v": false, "Meta+v": false, "Shift+Insert": false, "Alt+ArrowLeft": false,
	} {
		if got := movesOnly(chord); got != want {
			t.Errorf("%q: got %v, want %v", chord, got, want)
		}
	}
}

const videoPage = `<!doctype html><title>Ролики</title><body>
<h1>Ролик дня</h1>
<p>Короткий ролик про мероприятие.</p>
<video id="clip" src="/clip.webm" width="160" height="90" aria-label="Ролик"></video>
<video id="loop" src="/clip.webm" width="160" height="90" aria-label="По кругу" loop></video>
<button onclick="document.getElementById('clip').play()">Играть</button>
<button onclick="document.getElementById('loop').play()">Крутить</button>
</body>`

// The agent reads the page's text and the state of its videos, and waits for a clip to end; a looping one ends when
// it starts over (the person's «листать Shorts, когда ролик кончился»).
func TestAgentReadsAndWaitsForVideo(t *testing.T) {
	s := chromeOrSkip(t)
	clip, err := os.ReadFile("testdata/clip.webm")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/clip.webm" {
			w.Header().Set("Content-Type", "video/webm")
			_, _ = w.Write(clip)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(videoPage))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	tab, err := s.Open(ctx, "video", 800, 600, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer tab.Close()
	if err := tab.Navigate(srv.URL + "/"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if info, _ := tab.Info(); strings.Contains(info.Title, "Ролики") && !info.Loading {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the page did not load")
		}
	}
	snap := tab.Act(ctx, Act{ID: "1", Op: "snapshot"})
	if !snap.OK || !strings.Contains(snap.Text, `- video "Ролик" [`) || !strings.Contains(snap.Text, " paused at=0s") {
		t.Fatalf("snapshot: %+v", snap)
	}
	ref := func(name string) string {
		m := regexp.MustCompile(`- (?:button|video) "` + name + `" \[(e\d+)\]`).FindStringSubmatch(snap.Text)
		if m == nil {
			t.Fatalf("no %s in %s", name, snap.Text)
		}
		return m[1]
	}
	read := tab.Act(ctx, Act{ID: "2", Op: "read"})
	if !read.OK || !strings.Contains(read.Text, "title: Ролики") || !strings.Contains(read.Text, "- video ["+ref("Ролик")+"] paused") ||
		!strings.Contains(read.Text, "text:\nРолик дня\n") || !strings.Contains(read.Text, "Короткий ролик про мероприятие.") {
		t.Fatalf("read: %+v", read)
	}
	if one := tab.Act(ctx, Act{ID: "3", Op: "read", Ref: ref("Играть")}); !one.OK || !strings.HasSuffix(one.Text, "text:\nИграть") {
		t.Fatalf("read one: %+v", one)
	}
	notYet := tab.Act(ctx, Act{ID: "4", Op: "wait", Event: "ended", Ref: ref("Ролик"), MS: 600})
	if notYet.OK || !strings.Contains(notYet.Text, "is not ended after 600 ms") || !strings.Contains(notYet.Text, "Call again") {
		t.Fatalf("wait while paused: %+v", notYet)
	}
	if bad := tab.Act(ctx, Act{ID: "5", Op: "wait", Event: "ended", Ref: ref("Играть")}); bad.OK || !strings.Contains(bad.Text, "is not a video or audio") {
		t.Fatalf("wait on a button: %+v", bad)
	}
	// The click is the person's gesture the autoplay rule wants; the clip is two seconds long.
	if c := tab.Act(ctx, Act{ID: "6", Op: "click", Ref: ref("Играть")}); !c.OK {
		t.Fatalf("click: %+v", c)
	}
	if p := tab.Act(ctx, Act{ID: "7", Op: "wait", Event: "playing", MS: 5000}); !p.OK || !strings.Contains(p.Text, "["+ref("Ролик")+"] is playing") {
		t.Fatalf("wait playing: %+v", p)
	}
	over := tab.Act(ctx, Act{ID: "8", Op: "wait", Event: "ended", MS: 8000})
	if !over.OK || !strings.Contains(over.Text, "["+ref("Ролик")+"] has ended") || over.Rect == nil {
		t.Fatalf("wait ended: %+v", over)
	}
	if after := tab.Act(ctx, Act{ID: "9", Op: "read"}); !strings.Contains(after.Text, "- video ["+ref("Ролик")+"] ended") {
		t.Fatalf("read after: %+v", after)
	}
	if c := tab.Act(ctx, Act{ID: "10", Op: "click", Ref: ref("Крутить")}); !c.OK {
		t.Fatalf("click loop: %+v", c)
	}
	looped := tab.Act(ctx, Act{ID: "11", Op: "wait", Event: "ended", Ref: ref("По кругу"), MS: 8000})
	if !looped.OK || !strings.Contains(looped.Text, "played to its end") || !strings.Contains(looped.Text, "loop") {
		t.Fatalf("wait loop: %+v", looped)
	}
}
