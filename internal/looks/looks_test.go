package looks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPresetsAreLooksAndMatchThePage(t *testing.T) {
	page, err := os.ReadFile("../../web/app/next/features/looks/presets.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(page), bytes.TrimSpace(presetsJSON)) {
		t.Fatal("internal/looks/presets.json differs from web/app/next/features/looks/presets.json: copy it over")
	}
	for _, p := range Presets() {
		raw, _ := json.Marshal(p.Look)
		if _, err := Parse(raw); err != nil {
			t.Errorf("preset %s: %v", p.ID, err)
		}
	}
}

func TestParseRefusesWhatIsNotALook(t *testing.T) {
	ok := `{"v":1,"mode":"dark","hues":[{"h":300,"s":80}],"intensity":50,"bg":{"kind":"scene","scene":"matrix"},"grain":10,"dim":20,"blur":0,"parts":{}}`
	if _, err := Parse([]byte(ok)); err != nil {
		t.Fatalf("a good look refused: %v", err)
	}
	for name, bad := range map[string]string{
		"css":        `{"v":1,"mode":"dark","hues":[{"h":1,"s":1}],"intensity":1,"bg":{"kind":"none"},"grain":0,"dim":0,"blur":0,"parts":{},"css":"body{display:none}"}`,
		"scene":      `{"v":1,"mode":"dark","hues":[{"h":1,"s":1}],"intensity":1,"bg":{"kind":"scene","scene":"<script>"},"grain":0,"dim":0,"blur":0,"parts":{}}`,
		"hue":        `{"v":1,"mode":"dark","hues":[{"h":400,"s":1}],"intensity":1,"bg":{"kind":"none"},"grain":0,"dim":0,"blur":0,"parts":{}}`,
		"four hues":  `{"v":1,"mode":"dark","hues":[{"h":1,"s":1},{"h":1,"s":1},{"h":1,"s":1},{"h":1,"s":1}],"intensity":1,"bg":{"kind":"none"},"grain":0,"dim":0,"blur":0,"parts":{}}`,
		"image url":  `{"v":1,"mode":"dark","hues":[{"h":1,"s":1}],"intensity":1,"bg":{"kind":"image","image":"https://evil.example.com/x.png"},"grain":0,"dim":0,"blur":0,"parts":{}}`,
		"part value": `{"v":1,"mode":"dark","hues":[{"h":1,"s":1}],"intensity":1,"bg":{"kind":"none"},"grain":0,"dim":0,"blur":0,"parts":{"bubble":"hidden"}}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
}

func TestSubmitWaitsForThePage(t *testing.T) {
	moved := 0
	b := &Inbox{Moved: func() { moved++ }}
	go func() {
		for i := 0; i < 100; i++ {
			if p := b.Pending(); len(p) == 1 {
				b.Done(p[0].ID, StateApplied, "")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	req, status, err := b.Submit(context.Background(), Ask{Scope: ScopeFolder, Conv: "c1", Preset: "matrix", Why: "w", Wait: 2 * time.Second})
	if err != nil || status != StateApplied || req.Look == nil || req.Look.Bg.Scene != "matrix" || moved != 1 {
		t.Fatalf("status %q err %v req %+v moved %d", status, err, req, moved)
	}
	if _, _, err := b.Submit(context.Background(), Ask{Scope: ScopeChat, Preset: "matrix"}); err == nil {
		t.Fatal("a chat scope without a chat was taken")
	}
	_, status, _ = b.Submit(context.Background(), Ask{Reset: true, Wait: 0})
	if status != "waiting" {
		t.Fatalf("no page: %q", status)
	}
}

func TestFxIsDataToo(t *testing.T) {
	look := func(fx string) string {
		return `{"v":1,"mode":"dark","hues":[{"h":1,"s":1}],"intensity":1,"bg":{"kind":"none"},"grain":0,"dim":0,"blur":0,"parts":{}` + fx + `}`
	}
	hash := strings.Repeat("ab", 32)
	for name, ok := range map[string]string{
		"none":         look(``),
		"preset":       look(`,"fx":{"preset":"snowplay"}`),
		"preset power": look(`,"fx":{"preset":"laser","back":true,"power":0}`),
		"script":       look(`,"fx":{"script":"` + hash + `","power":100}`),
	} {
		l, err := Parse([]byte(ok))
		if err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
		if name == "preset" && (l.Fx == nil || l.Fx.Preset != "snowplay") {
			t.Errorf("fx not read: %+v", l.Fx)
		}
	}
	for _, p := range FxPresets {
		if _, err := Parse([]byte(look(`,"fx":{"preset":"` + p + `"}`))); err != nil {
			t.Errorf("fx preset %s refused: %v", p, err)
		}
	}
	for name, bad := range map[string]string{
		"empty":        look(`,"fx":{}`),
		"both":         look(`,"fx":{"preset":"laser","script":"` + hash + `"}`),
		"unknown":      look(`,"fx":{"preset":"<script>"}`),
		"short hash":   look(`,"fx":{"script":"abc"}`),
		"upper hash":   look(`,"fx":{"script":"` + strings.ToUpper(hash) + `"}`),
		"not hex":      look(`,"fx":{"script":"` + strings.Repeat("zz", 32) + `"}`),
		"url":          look(`,"fx":{"script":"https://evil.example.com/x.js"}`),
		"power high":   look(`,"fx":{"preset":"laser","power":101}`),
		"power low":    look(`,"fx":{"preset":"laser","power":-1}`),
		"inline code":  look(`,"fx":{"preset":"laser","code":"alert(1)"}`),
		"back no list": look(`,"fx":{"preset":"laser","back":"yes"}`),
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
}

func submitErr(t *testing.T, a Ask) error {
	t.Helper()
	b := &Inbox{}
	_, _, err := b.Submit(context.Background(), a)
	return err
}

func TestSubmitFxCode(t *testing.T) {
	const noFx = `{"v":1,"mode":"dark","hues":[{"h":1,"s":1}],"intensity":1,"bg":{"kind":"none"},"grain":0,"dim":0,"blur":0,"parts":{}`
	code := FxMagic + "hx.on('frame', () => {});\n"
	// the page uploads the code and names it in fx.script: the look may come without fx, or with fx without a script
	for name, raw := range map[string]string{
		"no fx":      noFx + `}`,
		"fx no hash": noFx + `,"fx":{"back":true,"power":60}}`,
	} {
		b := &Inbox{}
		req, status, err := b.Submit(context.Background(), Ask{Look: json.RawMessage(raw), FxCode: code, Why: "w"})
		if err != nil || status != "waiting" || req.FxCode != code || req.Look == nil {
			t.Errorf("%s: status %q err %v req %+v", name, status, err, req)
		}
	}
	// an fx that names nothing is still refused when there is no code to come
	if err := submitErr(t, Ask{Look: json.RawMessage(noFx + `,"fx":{"power":60}}`), Why: "w"}); err == nil {
		t.Error("an fx with no effect taken without code")
	}
	for name, a := range map[string]Ask{
		"too long":   {Look: json.RawMessage(noFx + `}`), FxCode: FxMagic + strings.Repeat("a", MaxFxCode)},
		"not utf8":   {Look: json.RawMessage(noFx + `}`), FxCode: FxMagic + "\xff\xfe"},
		"no look":    {FxCode: code},
		"preset":     {Preset: "matrix", FxCode: code},
		"reset":      {Reset: true, FxCode: code},
		"both":       {Look: json.RawMessage(noFx + `,"fx":{"preset":"laser"}}`), FxCode: code},
		"bad script": {Look: json.RawMessage(noFx + `,"fx":{"script":"xyz"}}`), FxCode: code},
	} {
		a.Why = "w"
		if err := submitErr(t, a); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	// the biggest code goes in
	if err := submitErr(t, Ask{Look: json.RawMessage(noFx + `}`), FxCode: FxMagic + strings.Repeat("a", MaxFxCode-len(FxMagic)), Why: "w"}); err != nil {
		t.Errorf("the largest code refused: %v", err)
	}
}
