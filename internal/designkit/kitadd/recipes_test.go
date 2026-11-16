package kitadd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFoldRootsAndAdoptItems(t *testing.T) {
	comps := []genComp{{ID: "Modal"}, {ID: "ModalRoot"}, {ID: "ModalBody", Part: "ModalRoot"}, {ID: "ListBox"}, {ID: "ListBoxItem"}, {ID: "ListBoxItemIndicator", Part: "ListBoxItem"}, {ID: "Collection"}}
	out := adoptItems("@heroui/react", foldRoots(comps))
	got := map[string]string{}
	for _, c := range out {
		got[c.ID] = c.Part
	}
	if _, ok := got["ModalRoot"]; ok || got["ModalBody"] != "Modal" {
		t.Errorf("root not folded: %v", got)
	}
	if got["ListBoxItem"] != "ListBox" || got["ListBoxItemIndicator"] != "ListBox" {
		t.Errorf("items not adopted: %v", got)
	}
	if _, ok := got["Collection"]; ok {
		t.Errorf("headless kept: %v", got)
	}
}

func TestRecipeSampleNeedsItsNames(t *testing.T) {
	has := func(n string) bool { return n == "Button" }
	props := []kitProp{{Name: "in"}}
	if recipeSample("@mui/material", "Fade", has, props) != nil {
		t.Error("recipe applied though Box is missing")
	}
	if recipeSample("@mui/material", "ButtonGroup", has, nil) == nil {
		t.Error("recipe not applied")
	}
}

func TestGeneralSampleRules(t *testing.T) {
	have := map[string]bool{"Tabs": true, "RadioGroup": true, "Select": true}
	for id, want := range map[string]string{"Tab": "Tabs", "Radio": "RadioGroup", "SelectOption": "Select", "Other": ""} {
		if got := collectionParent(id, have); got != want {
			t.Errorf("collectionParent(%s) = %q, want %q", id, got, want)
		}
	}
	props := []kitProp{{Name: "isOpen", Type: "boolean"}, {Name: "children", Type: "node"}}
	parts := []string{"DlgTrigger", "DlgBackdrop", "DlgDialog", "DlgBody"}
	pp := map[string][]kitProp{}
	for _, p := range parts {
		pp[p] = []kitProp{{Name: "children", Type: "node"}}
	}
	s := sampleFor("Dlg", props, parts, pp, "Box")
	if s["isOpen"] != true {
		t.Errorf("not opened: %v", s)
	}
	kids, _ := s["children"].([]any)
	if len(kids) != 2 || kids[0].(map[string]any)["$c"] != "DlgTrigger" || kids[1].(map[string]any)["$c"] != "DlgBackdrop" {
		t.Errorf("overlay not nested: %v", s["children"])
	}
	tr := sampleFor("Fade", []kitProp{{Name: "in", Type: "boolean"}, {Name: "children", Type: "node"}}, nil, nil, "Box")
	if tr["in"] != true || tr["children"].(map[string]any)["$c"] != "Box" {
		t.Errorf("transition: %v", tr)
	}
}

func TestTableSampleNests(t *testing.T) {
	parts := []string{"TableThead", "TableTbody", "TableTr", "TableTd", "TableTh", "TableCaption"}
	s := sampleFor("Table", []kitProp{{Name: "children", Type: "node"}}, parts, nil, "Box")
	raw, _ := json.Marshal(s)
	if !strings.Contains(string(raw), `"$c":"TableTbody","children":[{"$c":"TableTr","children":[{"$c":"TableTd"`) {
		t.Fatalf("table not nested: %s", raw)
	}
}
