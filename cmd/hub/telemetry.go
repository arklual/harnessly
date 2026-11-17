package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/buildinfo"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/telemetry"
)

// notReported marks a log line at error level that is not a fault of the hub (a warning of the configuration): it
// stays in the log and never goes to harnessly's makers.
var notReported = slog.Bool("report", false)

// hubTelemetryTarget is where the hub's anonymous reports go: HARNESSLY_TELEMETRY_URL and _KEY (a company's own
// PostHog), else what the release build was linked with. A build from a checkout has neither and sends nothing.
func hubTelemetryTarget(getenv func(string) string) (url, key string) {
	url, key = getenv("HARNESSLY_TELEMETRY_URL"), getenv("HARNESSLY_TELEMETRY_KEY")
	if url == "" {
		url, key = telemetry.DefaultURL, telemetry.DefaultKey
	}
	return url, key
}

// withHubTelemetry sends the hub's own errors (the log's error level) as $exception and a heartbeat a day, the way a
// runtime sends its own (docs/self-host/telemetry.md). The log stays as it was; with no target it is returned as is.
func withHubTelemetry(ctx context.Context, cfg *config.Config, log *slog.Logger, getenv func(string) string) *slog.Logger {
	url, key := hubTelemetryTarget(getenv)
	if url == "" {
		return log
	}
	consent := &telemetry.Consent{Path: filepath.Join(cfg.DataDir, "telemetry.json"), Getenv: getenv}
	if !consent.Allowed() {
		log.Info("telemetry: off", "by", consent.Why())
	} else {
		log.Info("telemetry: anonymous error reports and a daily heartbeat go to harnessly's makers; " +
			"turn off with HARNESSLY_TELEMETRY=off or DO_NOT_TRACK=1 (docs/self-host/telemetry.md)")
	}
	common := jsvalue.NewObject("version", buildinfo.Version, "os", runtime.GOOS, "arch", runtime.GOARCH,
		"mode", "hub", "edition", cfg.Edition, "database", dbKind(cfg), "$lib", "harnessly-hub")
	q := telemetry.New(telemetry.Config{URL: url, Key: key, Common: common, DistinctID: consent.ID,
		Allow: consent.Allowed, Log: func(level, msg string, kv ...any) { log.Debug(msg, kv...) }})
	rep := &telemetry.Reporter{Queue: q}
	started := time.Now()
	beat := func() {
		q.Push(telemetry.Event{Name: "harnessly_heartbeat", At: time.Now(), Properties: jsvalue.NewObject(
			"uptime_hours", int(time.Since(started).Hours()))})
	}
	go func() {
		beat()
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				q.Flush()
				return
			case <-t.C:
				beat()
			}
		}
	}()
	return slog.New(&reportHandler{next: log.Handler(), rep: rep})
}

// reportHandler passes every record on and reports the error-level ones: the message and the error, scrubbed by the
// reporter; the route and the method, which name no person, as properties.
type reportHandler struct {
	next  slog.Handler
	rep   *telemetry.Reporter
	attrs []slog.Attr
}

func (h *reportHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *reportHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.next.Handle(ctx, r)
	if r.Level >= slog.LevelError {
		h.report(r)
	}
	return err
}

func (h *reportHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return &reportHandler{next: h.next.WithAttrs(as), rep: h.rep, attrs: append(append([]slog.Attr{}, h.attrs...), as...)}
}

func (h *reportHandler) WithGroup(name string) slog.Handler {
	return &reportHandler{next: h.next.WithGroup(name), rep: h.rep, attrs: h.attrs}
}

func (h *reportHandler) report(r slog.Record) {
	msg, kind, skip := r.Message, "log", false
	props := jsvalue.NewObject()
	take := func(a slog.Attr) {
		switch a.Key {
		case notReported.Key:
			if a.Value.Kind() == slog.KindBool && !a.Value.Bool() {
				skip = true
			}
		case "error", "err":
			msg += ": " + a.Value.String()
		case "panic":
			kind = "panic"
			msg += ": " + fmt.Sprint(a.Value.Any())
		case "route":
			// A route is the mux's pattern; without one it is the path, scrubbed like the message.
			props.Set("route", telemetry.Scrub(a.Value.String()))
			if kind == "log" {
				kind = "http"
			}
		case "method":
			props.Set("method", a.Value.String())
		}
	}
	for _, a := range h.attrs {
		take(a)
	}
	r.Attrs(func(a slog.Attr) bool { take(a); return true })
	if skip {
		return
	}
	h.rep.Error("hub", kind, msg, props)
}

// osGetenv is the environment the hub's telemetry reads; tests replace it.
var osGetenv = os.Getenv
