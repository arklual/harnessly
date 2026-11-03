package design

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func viewResultText(t *testing.T, r *jsvalue.Object) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(json.RawMessage(jsvalue.Stringify(r, "")))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Content) == 0 {
		t.Fatalf("result %s: %v", raw, err)
	}
	return out.Content[0].Text, out.IsError
}

func TestVideoViewOffersWhileThePersonMovesThePlayer(t *testing.T) {
	const id = "view-offer"
	now := time.Unix(1_800_000_000, 0)
	at := int64(5000)
	if offered, open := videoViewPush(id, VideoViewCmd{Kind: "seek", T: &at}, now); offered || open {
		t.Fatalf("no page: offered %v open %v", offered, open)
	}
	if cmds := videoViewTake(id, VideoViewReport{T: 1000, Format: VideoLandscape, TouchedAgo: -1}, now); len(cmds) != 0 {
		t.Fatalf("nothing was sent to a closed player: %+v", cmds)
	}
	if offered, open := videoViewPush(id, VideoViewCmd{Kind: "seek", T: &at}, now); offered || !open {
		t.Fatalf("a quiet player: offered %v open %v", offered, open)
	}
	cmds := videoViewTake(id, VideoViewReport{T: 1000, Format: VideoLandscape, TouchedAgo: 2000}, now)
	if len(cmds) != 1 || cmds[0].Offer || cmds[0].Kind != "seek" || *cmds[0].T != 5000 {
		t.Fatalf("the command: %+v", cmds)
	}
	if offered, open := videoViewPush(id, VideoViewCmd{Kind: "pause"}, now); !offered || !open {
		t.Fatalf("the person just moved it: offered %v open %v", offered, open)
	}
	cmds = videoViewTake(id, VideoViewReport{T: 1000, Format: VideoLandscape, TouchedAgo: -1, Acked: cmds[0].Seq}, now)
	if len(cmds) != 1 || !cmds[0].Offer || cmds[0].Kind != "pause" {
		t.Fatalf("an offer: %+v", cmds)
	}
	if seen := videoViewSeen(id, now); !seen.Open || !seen.Moving || seen.T != 1000 {
		t.Fatalf("seen %+v", seen)
	}
	if seen := videoViewSeen(id, now.Add(videoViewLive+time.Second)); seen.Open {
		t.Fatalf("a page gone quiet is closed: %+v", seen)
	}
	late := now.Add(videoViewCmdTTL + time.Second)
	if cmds := videoViewTake(id, VideoViewReport{Format: VideoLandscape, TouchedAgo: -1}, late); len(cmds) != 0 {
		t.Fatalf("an old command is dropped: %+v", cmds)
	}
}

func TestVideoToolsExistOnlyInAVideoChat(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	writeFile(t, filepath.Join(root, "clip", ArtifactFile), `{"title":"Clip"}`)
	writeFile(t, filepath.Join(root, "clip", MetaDirname, MetaProjectFile), `{"v":1,"title":"Clip"}`)
	writeFile(t, filepath.Join(root, "clip", VideoSettingsFile), `{"v":1,"formats":["16:9"],"fps":30,"seconds":null,"voice":"none","subs":false}`)
	writeFile(t, filepath.Join(root, "site", ArtifactFile), `{"title":"Site"}`)

	for cwd, want := range map[string]string{
		filepath.Join(root, "clip"):            "clip",
		filepath.Join(root, "clip", "project"): "clip",
		filepath.Join(root, "site"):            "",
		root:                                   "",
		"/somewhere/else":                      "",
		"":                                     "",
	} {
		if got := l.VideoProjectOf(cwd); got != want {
			t.Errorf("%q: %q, want %q", cwd, got, want)
		}
	}
	off := VideoTools{L: l}
	if off.Instructions() != "" || len(off.Tools()) != 0 || off.Has("sinopia_view_seek") {
		t.Error("a chat outside a video carries the video's tools")
	}
	on := VideoTools{L: l, Project: "clip"}
	if len(on.Tools()) != 13 || !on.Has("sinopia_view_seek") || !strings.Contains(on.Instructions(), "clip") {
		t.Errorf("a video's chat: %d tools", len(on.Tools()))
	}
	text, isErr := viewResultText(t, on.Call(context.Background(), "sinopia_view_pause", jsvalue.NewObject()))
	if isErr || !strings.Contains(text, "does not have the player open") {
		t.Errorf("no player: %q %v", text, isErr)
	}
	if _, isErr := viewResultText(t, on.Call(context.Background(), "sinopia_view_format", jsvalue.NewObject("format", "9:16"))); !isErr {
		t.Error("a format the video lacks is refused")
	}
	if _, isErr := viewResultText(t, on.Call(context.Background(), "sinopia_view_seek", jsvalue.NewObject())); !isErr {
		t.Error("a seek with nowhere to go is refused")
	}
}

func TestVideoGuideTopics(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	for _, topic := range VideoGuideTopics {
		for _, loc := range []string{"en", "ru"} {
			t.Setenv("RUNTIME_LOCALE", loc)
			text, ok := VideoGuide(topic, "acme-cli")
			if !ok || text == "" || strings.Contains(text, "{{") {
				t.Errorf("%s/%s: %v %q", topic, loc, ok, text[:min(len(text), 40)])
			}
		}
	}
	if _, ok := VideoGuide("../secret", "x"); ok {
		t.Error("a topic that is not listed is refused")
	}
}

func TestVideoLintFindsWhatBreaksTime(t *testing.T) {
	codes := func(src string) string {
		var out []string
		for _, i := range videoLintSource("s", src) {
			out = append(out, i.Code)
		}
		return strings.Join(out, ",")
	}
	cases := map[string]string{
		`<script>requestAnimationFrame(loop)</script>`:                                          VideoCheckRaf,
		`<script>gsap.to('.x',{x:1})</script>`:                                                  VideoCheckUnbound,
		`<script>tkVideo.motion(function(){gsap.to('.x',{x:1})})</script>`:                      "",
		`<script src="https://cdn.example/x.js"></script>`:                                      VideoCheckExternal,
		`<link rel="stylesheet" href="//fonts.example/a.css">`:                                  VideoCheckExternal,
		`<script src="/__lib/motion/gsap.min.js"></script><script src="./support.js"></script>`: "",
		`<script>setTimeout(f,1)</script>`:                                                      VideoCheckTimer,
	}
	for src, want := range cases {
		if got := codes(src); got != want {
			t.Errorf("%s: %q, want %q", src, got, want)
		}
	}
}

func TestVideoFramesCloseIgnoresNoise(t *testing.T) {
	if !videoFramesClose([]byte("same"), []byte("same")) {
		t.Error("equal bytes")
	}
	if videoFramesClose([]byte("not a picture"), []byte("another")) {
		t.Error("what cannot be read is not close")
	}
}

func TestVideoSheetTimes(t *testing.T) {
	got := videoSheetTimes(2000)
	if got[0] != 0 || got[len(got)-1] != 2000-videoCheckBeforeEnd || len(got) != 5 {
		t.Errorf("%v", got)
	}
	if long := videoSheetTimes(300_000); len(long) > videoSheetMaxCells {
		t.Errorf("a long scene: %d cells", len(long))
	}
}

func TestMotionKitIsServed(t *testing.T) {
	kit := motionKit()
	for _, name := range []string{"gsap.min.js", "tk.js", "DrawSVGPlugin.min.js", "SplitText.min.js", "MorphSVGPlugin.min.js", "fonts.css", "fonts/inter-latin-wght-normal.woff2", "fonts/inter-cyrillic-wght-normal.woff2"} {
		if kit[name] == nil || len(kit[name].raw) == 0 {
			t.Errorf("%s is missing", name)
		}
	}
	if kit["fonts/inter-latin-wght-normal.woff2"].typ != "font/woff2" || kit["fonts.css"].typ != "text/css; charset=utf-8" {
		t.Error("the kit's types")
	}
}
