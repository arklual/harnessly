package kitadd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHostOf(t *testing.T) {
	tag, attrs := hostOf("button[mat-button], a[mat-button]")
	if tag != "button" || !reflect.DeepEqual(attrs, [][2]string{{"mat-button", ""}}) {
		t.Fatalf("got %q %v", tag, attrs)
	}
	if tag, _ := hostOf("[tuiButton]:not(a)"); tag != "div" {
		t.Fatalf("an attribute selector gets a div, got %q", tag)
	}
	if tag, _ := hostOf("mat-card"); tag != "mat-card" {
		t.Fatalf("got %q", tag)
	}
}

func TestNgID(t *testing.T) {
	taken := map[string]bool{"Card": true}
	cases := []struct{ class, ns, want string }{
		{"MatButton", "Mat", "Button"},
		{"MatCard", "Mat", "MatCard"},   // Card is taken
		{"Material", "Mat", "Material"}, // not a prefix on a word boundary
		{"MatRoot", "Mat", "MatRoot"},
		{"TuiLink", "Tui", "Link"},
	}
	for _, c := range cases {
		if got := ngID(c.class, c.ns, taken); got != c.want {
			t.Errorf("ngID(%s,%s) = %s, want %s", c.class, c.ns, got, c.want)
		}
	}
}

func TestAngularPeers(t *testing.T) {
	dir := t.TempDir()
	pk := filepath.Join(dir, "node_modules", "lib")
	if err := os.MkdirAll(pk, 0o755); err != nil {
		t.Fatal(err)
	}
	pj := `{"peerDependencies":{"@angular/core":"^22.0.0 || ^23.0.0","@angular/cdk":"^22.0.0","@angular/forms":">=19.0.0","react":"^18","rxjs":"^7"}}`
	if err := os.WriteFile(filepath.Join(pk, "package.json"), []byte(pj), 0o644); err != nil {
		t.Fatal(err)
	}
	got := angularPeers(dir, "lib")
	want := map[string]bool{"@angular/core@22": true, "@angular/cdk@22": true, "@angular/forms@latest": true,
		"@angular/common@22": true, "@angular/platform-browser@22": true, "@angular/compiler@22": true, "rxjs": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected %s in %v", g, got)
		}
	}
}

func TestNgMarkParts(t *testing.T) {
	comps := []genComp{{ID: "Table"}, {ID: "Cell"}, {ID: "Card"}, {ID: "CardHeader"}, {ID: "CardHeaderTitle"}, {ID: "Cardinal"}}
	ngMarkParts(comps, map[string]string{"Cell": "Table"})
	got := map[string]string{}
	for _, c := range comps {
		got[c.ID] = c.Part
	}
	want := map[string]string{"Table": "", "Cell": "Table", "Card": "", "CardHeader": "Card", "CardHeaderTitle": "Card", "Cardinal": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestPrimengLicense(t *testing.T) {
	dir := t.TempDir()
	prov := "providers.push(providePrimeNG({ theme: { preset: Aura } }));"
	var log strings.Builder
	o := &Options{Log: &log}
	if got := primengLicense(o, dir, prov); got != prov || log.Len() != 0 {
		t.Fatalf("a version without the check must stay as it is: %q %q", got, log.String())
	}
	f := filepath.Join(dir, "node_modules", "primeng", "fesm2022")
	if err := os.MkdirAll(f, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f, "primeng-license.mjs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := primengLicense(o, dir, prov); got != prov || !strings.Contains(log.String(), "21.1.10") {
		t.Fatalf("no key: a warning naming the version, provider unchanged: %q %q", got, log.String())
	}
	o.License = `k"1`
	if got := primengLicense(o, dir, prov); !strings.Contains(got, `providePrimeNG({ license: "k\"1", theme:`) {
		t.Fatalf("the key is put into the provider: %q", got)
	}
}

func TestAngularRuntimeSetsOwnDefs(t *testing.T) {
	// The template's directive table is set from the classes' own definitions: the JIT stops short on long import
	// lists (PrimeNG), leaving p-table and p-select as bare elements.
	for _, want := range []string{"directiveDefs = function", "allDefs()", "ɵtransitiveScopesFor"} {
		if !strings.Contains(angularRuntime, want) {
			t.Fatalf("the runtime lacks %q", want)
		}
	}
}

func TestImproveAngularDocsReplacesGeneratedAlternatives(t *testing.T) {
	docs := map[string]compDoc{
		"Chip":   {WhenSrc: srcGenerated, WhenNot: []string{"A chip avatar fits better: take ChipAvatar instead."}},
		"Button": {WhenSrc: srcJSDoc, WhenNot: []string{"kept"}},
	}
	improveAngularDocs(docs, []genComp{{ID: "Chip"}, {ID: "Button"}})
	if got := docs["Chip"].WhenNot[0]; !strings.Contains(got, "Badge") {
		t.Fatalf("Chip alternative not replaced: %q", got)
	}
	if docs["Button"].WhenNot[0] != "kept" {
		t.Fatalf("authors' text must stay")
	}
}
