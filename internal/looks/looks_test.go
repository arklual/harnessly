package looks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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
