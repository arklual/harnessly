package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/journal"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/observe"
	"github.com/ly-solutions/harnessly/internal/settingsctl"
	"github.com/ly-solutions/harnessly/internal/telemetry"
)

// productConsent is this machine's yes or no to anonymous product telemetry (docs/telemetry.md): the settings item
// and every queue that leaves the machine ask it. nil until the runtime starts; then only the environment decides.
var productConsent *telemetry.Consent

// telemetryAllowed is the gate of everything sent for harnessly's makers, the deployment's own reports included.
func telemetryAllowed() bool {
	if productConsent != nil {
		return productConsent.Allowed()
	}
	off, _ := telemetry.EnvOff(os.Getenv)
	return !off
}

// productTelemetryTarget is where anonymous telemetry goes: HARNESSLY_TELEMETRY_URL and _KEY (a company's own
// PostHog), else what the release build was linked with. A build from a checkout has neither and sends nothing.
func productTelemetryTarget() (url, key string) {
	url, key = os.Getenv("HARNESSLY_TELEMETRY_URL"), os.Getenv("HARNESSLY_TELEMETRY_KEY")
	if url == "" {
		url, key = telemetry.DefaultURL, telemetry.DefaultKey
	}
	return url, key
}

// reportedSources are the journal's sources whose errors are harnessly's own; the person's project (preview) and
// the agents' tools are theirs and never leave.
var reportedSources = map[string]bool{journal.SourceWeb: true, journal.SourceRuntime: true, journal.SourceSinopia: true}

// startProductTelemetry opens the consent and, when a target is set, sends the journal's errors as $exception and a
// heartbeat a day. It says on start what is sent and how to turn it off.
func startProductTelemetry(ctx context.Context, dataDir string, j *journal.Journal, mode string) {
	productConsent = &telemetry.Consent{Path: filepath.Join(dataDir, "telemetry.json"), Getenv: os.Getenv}
	url, key := productTelemetryTarget()
	if url == "" {
		return
	}
	if !productConsent.Allowed() {
		agent.Log("info", "telemetry: off", agent.F{K: "by", V: productConsent.Why()})
	} else {
		agent.Log("info", "telemetry: anonymous error reports and a daily heartbeat go to harnessly's makers; "+
			"turn off with HARNESSLY_TELEMETRY=off, DO_NOT_TRACK=1 or Settings → Telemetry (docs/telemetry.md)")
	}
	common := jsvalue.NewObject("version", agent.BuildVersion, "os", runtime.GOOS, "arch", runtime.GOARCH, "mode", mode,
		"$lib", "harnessly-runtime")
	q := telemetry.New(telemetry.Config{URL: url, Key: key, Common: common, DistinctID: productConsent.ID,
		Allow: productConsent.Allowed, Log: machinesLog})
	rep := &telemetry.Reporter{Queue: q}
	if j != nil {
		j.AddSink(journal.Sink{Entry: func(e journal.Entry) {
			if e.Level != journal.Error || !reportedSources[e.Source] || journal.Benign(e) {
				return
			}
			msg := e.Text
			if msg == "" && e.Status != 0 {
				msg = e.Method + " " + strconv.Itoa(e.Status) + " " + e.URL
			}
			props := jsvalue.NewObject()
			if e.Status != 0 {
				props.Set("status", e.Status)
			}
			if e.Tool != "" {
				props.Set("tool", e.Tool)
			}
			rep.Error(e.Source, e.Event, msg, props)
		}})
	}
	started := time.Now()
	beat := func() {
		q.Push(telemetry.Event{Name: "harnessly_heartbeat", At: time.Now(), Properties: jsvalue.NewObject(
			"uptime_hours", int(time.Since(started).Hours()), "claude", hasCommand("claude"), "codex", hasCommand("codex"))})
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
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// startObservability hands the journal to the operator's own stack: OTLP logs when an OTLP endpoint is set, the
// Prometheus text format on RUNTIME_METRICS_LISTEN. Neither depends on the telemetry switch: it goes to them, not us.
func startObservability(ctx context.Context, j *journal.Journal) {
	otlp := observe.OTLPFromEnv(os.Getenv, "harnessly-runtime", agent.BuildVersion)
	if otlp != nil && j != nil {
		otlp.Log = machinesLog
		j.AddSink(otlp.Sink())
		agent.Log("info", "observe: journal goes to OTLP", agent.F{K: "endpoint", V: otlp.URL}, agent.F{K: "level", V: otlp.MinLevel})
		go func() { <-ctx.Done(); otlp.Flush() }()
	}
	listen := strings.TrimSpace(os.Getenv("RUNTIME_METRICS_LISTEN"))
	if listen == "" {
		return
	}
	if !strings.Contains(listen, ":") {
		listen = "127.0.0.1:" + listen
	}
	m := &observe.Metrics{Version: agent.BuildVersion, Started: time.Now()}
	if j != nil {
		j.AddSink(m.Sink())
		m.Dropped = j.Dropped
	}
	if otlp != nil {
		m.OTLPDropped = otlp.Dropped
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		agent.Log("warn", "observe: metrics not served", agent.F{K: "listen", V: listen}, agent.F{K: "error", V: err.Error()})
		return
	}
	srv := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	agent.Log("info", "observe: Prometheus metrics", agent.F{K: "url", V: "http://" + ln.Addr().String() + "/metrics"})
	go func() { _ = srv.Serve(ln) }()
	go func() { <-ctx.Done(); _ = srv.Close() }()
}

// telemetryItem is the settings switch of anonymous telemetry. Turned off by the environment, it says so and stays off.
func telemetryItem() settingsctl.Item {
	return settingsctl.Item{Key: "telemetry", Section: locale.Pick("Privacy", "Приватность"), Kind: settingsctl.KindBool,
		Title: locale.Pick("Send anonymous error reports", "Отправлять анонимные отчёты об ошибках"),
		Description: locale.Pick("Errors of harnessly itself (without your texts, files, addresses or names) and a daily heartbeat with the version go to its makers, so they see what breaks. Off also stops the deployment's own reports. HARNESSLY_TELEMETRY=off or DO_NOT_TRACK=1 turn it off for good.",
			"Ошибки самого harnessly (без ваших текстов, файлов, адресов и имён) и раз в сутки версия уходят его разработчикам, чтобы видеть, что ломается. Выключение останавливает и отчёты развёртывания. HARNESSLY_TELEMETRY=off или DO_NOT_TRACK=1 выключают насовсем."),
		Get: func() (string, error) {
			if telemetryAllowed() {
				return settingsctl.On, nil
			}
			return settingsctl.Off, nil
		},
		Set: func(v string) error {
			if productConsent == nil {
				return nil
			}
			return productConsent.Set(v == settingsctl.On)
		}}
}
