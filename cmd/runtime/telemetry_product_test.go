package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/settingsctl"
	"github.com/ly-solutions/harnessly/internal/telemetry"
)

// The settings switch keeps its answer in the data folder; the environment's off wins over it; a checkout sends nothing.
func TestTelemetrySwitch(t *testing.T) {
	t.Setenv("HARNESSLY_TELEMETRY", "")
	t.Setenv("RUNTIME_TELEMETRY", "")
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("HARNESSLY_TELEMETRY_URL", "")
	old := productConsent
	defer func() { productConsent = old }()
	dir := t.TempDir()
	startProductTelemetry(context.Background(), dir, nil, "standalone")
	if u, _ := productTelemetryTarget(); u != "" && telemetry.DefaultURL == "" {
		t.Fatal("a target without one being set")
	}
	item := telemetryItem()
	if v, _ := item.Get(); v != settingsctl.On {
		t.Fatalf("default %s, want on", v)
	}
	if err := item.Set(settingsctl.Off); err != nil {
		t.Fatal(err)
	}
	if v, _ := item.Get(); v != settingsctl.Off || telemetryAllowed() {
		t.Fatal("off not kept")
	}
	again := &telemetry.Consent{Path: filepath.Join(dir, "telemetry.json")}
	if again.Allowed() {
		t.Fatal("off not written to the data folder")
	}
	_ = item.Set(settingsctl.On)
	t.Setenv("DO_NOT_TRACK", "1")
	if v, _ := item.Get(); v != settingsctl.Off {
		t.Fatal("DO_NOT_TRACK does not win")
	}
}
