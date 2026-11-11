package hlcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// `design kit show <kit> component:X` and the kit_component tool give one answer: both are designkit.ComponentOf.
func TestKitShowAComponentIsWhatKitComponentAnswers(t *testing.T) {
	kits := t.TempDir()
	src, _ := filepath.Abs("../designkit/testdata/kit/acme-ui")
	dst := filepath.Join(kits, "acme-ui", "1.0.0")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	l := design.New(design.Config{LibraryDir: t.TempDir(), KitsDir: kits, Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	env := map[string]string{"RUNTIME_DESIGN_KITS_DIR": kits}
	for _, name := range []string{"Button", "Card", "PrimaryButton", "CardHeader"} {
		r := kitRun(t, env, t.TempDir(), "design", "kit", "show", "acme-ui", "component:"+name)
		if r.code != 0 {
			t.Fatalf("%s: %+v", name, r)
		}
		var shown struct {
			Value map[string]any `json:"value"`
			Call  string         `json:"call"`
		}
		if err := json.Unmarshal([]byte(r.out), &shown); err != nil {
			t.Fatal(err)
		}
		res := design.KitTools{L: l}.Call(context.Background(), "kit_component", jsvalue.NewObject("kit", "acme-ui", "name", name))
		var tool map[string]any
		if err := json.Unmarshal([]byte(res.Array("content")[0].(*jsvalue.Object).String("text")), &tool); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(shown.Value, tool) {
			t.Errorf("%s: the CLI and the tool differ\ncli:  %v\ntool: %v", name, shown.Value, tool)
		}
		if shown.Call != tool["call"] {
			t.Errorf("%s: call %q vs %q", name, shown.Call, tool["call"])
		}
	}
}
