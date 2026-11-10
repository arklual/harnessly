package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/preview"
)

// HRN-914: without RUNTIME_PREVIEW_PUBLIC_ORIGINS the slots' origins come from RUNTIME_PREVIEW_DOMAIN and an id that
// stays the same across starts.
func TestSlotOriginsFromDomain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_MACHINE_FILE", dir+"/none.json")
	t.Setenv("RUNTIME_PREVIEW_PUBLIC_ORIGINS", "")
	t.Setenv("RUNTIME_PREVIEW_ID", "")
	t.Setenv("RUNTIME_PREVIEW_DOMAIN", "*.Preview.Example.com")
	first := slotPublicOrigins(dir)
	if len(first) != previewSlotCount {
		t.Fatalf("origins %v", first)
	}
	for i, o := range first {
		if !strings.HasPrefix(o, "https://s"+string(rune('1'+i))+"-") || !strings.HasSuffix(o, ".preview.example.com") ||
			!previewOriginRe.MatchString(o) {
			t.Fatalf("origin %d: %q", i+1, o)
		}
	}
	// A restart (the service manager, an update, the supervisor) names the same slots.
	if again := slotPublicOrigins(dir); !reflect.DeepEqual(again, first) {
		t.Fatalf("after a restart %v, before %v", again, first)
	}

	t.Setenv("RUNTIME_PREVIEW_ID", "box-1")
	if got := slotPublicOrigins(dir); got[0] != "https://s1-box-1.preview.example.com" || got[7] != "https://s8-box-1.preview.example.com" {
		t.Fatalf("named id: %v", got)
	}
}

func TestSlotOriginsListWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNTIME_PREVIEW_DOMAIN", "preview.example.com")
	list := make([]string, previewSlotCount)
	for i := range list {
		list[i] = "https://p" + string(rune('1'+i)) + ".example.org"
	}
	t.Setenv("RUNTIME_PREVIEW_PUBLIC_ORIGINS", strings.Join(list, ","))
	if got := slotPublicOrigins(dir); !reflect.DeepEqual(got, list) {
		t.Fatalf("got %v", got)
	}
	// A list that is wrong is said in the log and leaves the slots on the app's host, whatever the domain.
	t.Setenv("RUNTIME_PREVIEW_PUBLIC_ORIGINS", "https://p1.example.org")
	if got := slotPublicOrigins(dir); got != nil {
		t.Fatalf("a short list gave %v", got)
	}
}

func TestSlotOriginsNone(t *testing.T) {
	t.Setenv("RUNTIME_PREVIEW_PUBLIC_ORIGINS", "")
	t.Setenv("RUNTIME_PREVIEW_DOMAIN", "")
	if got := slotPublicOrigins(t.TempDir()); got != nil {
		t.Fatalf("got %v", got)
	}
	t.Setenv("RUNTIME_PREVIEW_DOMAIN", "not a domain")
	if got := slotPublicOrigins(t.TempDir()); got != nil {
		t.Fatalf("a bad domain gave %v", got)
	}
}

// The machine check fails on a slot name that does not resolve and passes a name of another address (a proxy there).
func TestPreviewDNSMachineCheck(t *testing.T) {
	ok := previewDNSCheck([]preview.OriginCheck{{Host: "s1-a.preview.example.com", State: preview.OriginElsewhere}})
	if !ok.OK || ok.ID != "preview_dns" {
		t.Fatalf("%+v", ok)
	}
	bad := previewDNSCheck([]preview.OriginCheck{
		{Host: "s1-a.preview.example.com", State: preview.OriginOK},
		{Host: "s2-a.preview.example.com", State: preview.OriginNotFound},
	})
	if bad.OK || !strings.Contains(bad.Detail, "s2-a.preview.example.com") || strings.Contains(bad.Detail, "s1-a") ||
		bad.FixCommand != "harnessly doctor preview" {
		t.Fatalf("%+v", bad)
	}
	p := machineProbe{PreviewOrigins: func() []string { return []string{"https://s1-a.preview.example.com"} },
		PreviewDNS: func(context.Context, []string) []preview.OriginCheck {
			return []preview.OriginCheck{{Host: "s1-a.preview.example.com", State: preview.OriginNotFound}}
		}}
	if got := p.previewCheck(context.Background()); got == nil || got.OK {
		t.Fatalf("%+v", got)
	}
}
