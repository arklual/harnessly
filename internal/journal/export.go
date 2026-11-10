package journal

import (
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/telemetry"
)

// TelemetryEvent is a warning or an error as an optional export sends it (RUNTIME_JOURNAL_EXPORT=telemetry): technical
// properties only, as telemetry carries them — no text, no address, no chat.
func TelemetryEvent(e Entry) telemetry.Event {
	props := jsvalue.NewObject("level", e.Level, "source", e.Source, "event", e.Event)
	if e.Status != 0 {
		props.Set("status", e.Status)
	}
	if e.Dur != 0 {
		props.Set("duration_ms", e.Dur)
	}
	if e.App != "" {
		props.Set("app", e.App)
	}
	if e.Tool != "" {
		props.Set("tool", e.Tool)
	}
	if e.Method != "" {
		props.Set("method", e.Method)
	}
	if e.Trace != "" {
		props.Set("trace", e.Trace)
	}
	return telemetry.Event{Name: "harnessly_journal_" + e.Level, At: time.UnixMilli(e.At), Properties: props}
}
