# Logs and metrics

The runtime keeps a journal of what its page, the runtime, app views, Sinopia and tool calls did (`harnessly logs`).
You can hand it to your own observability stack: as OpenTelemetry logs and as Prometheus metrics. Both are off until you
point them somewhere, both go to you only, and neither depends on the [telemetry](telemetry.md) switch.

## Logs over OTLP

Set the standard OpenTelemetry variables and the journal's lines go as OTLP/HTTP JSON logs to a Collector, Grafana
Alloy, Loki (`/otlp`), Elastic, SigNoz or anything else that takes OTLP:

| Variable | Meaning |
| --- | --- |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | the logs endpoint, whole (`http://alloy:4318/v1/logs`) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | or the base; `/v1/logs` is added |
| `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_LOGS_HEADERS` | headers, `key=value,key2=value2`, values percent-encoded (`Authorization=Basic%20…`) |
| `OTEL_SERVICE_NAME` | `service.name` (default `harnessly-runtime`) |
| `OTEL_RESOURCE_ATTRIBUTES` | more resource attributes (`deployment.environment=prod`) |
| `RUNTIME_OTLP_LEVEL` | the least severe level sent: `info` (everything, requests included), `warn` (the default) or `error` |

`RUNTIME_OTLP_ENDPOINT`, `RUNTIME_OTLP_LOGS_ENDPOINT`, `RUNTIME_OTLP_HEADERS`, `RUNTIME_OTLP_SERVICE_NAME` and
`RUNTIME_OTLP_RESOURCE_ATTRIBUTES` override the `OTEL_*` ones for the runtime alone, when other programs of the machine
read those too.

Each record carries the level as severity, the text as body and `harnessly.source`, `harnessly.event`,
`harnessly.chat`, `harnessly.app`, `harnessly.tool`, `http.request.method`, `url.path`,
`http.response.status_code`, `harnessly.duration_ms` as attributes. Lines go every 5 seconds or 200 at a time; while the
collector does not answer, up to 5000 wait, then the newest are dropped and counted.

## Prometheus metrics

`RUNTIME_METRICS_LISTEN` (`127.0.0.1:9464`, or a port alone for loopback) serves `GET /metrics` in the Prometheus text
format on a listener of its own. It checks nobody: keep it on loopback or on a network only your collector reaches.

| Metric | Type | Meaning |
| --- | --- | --- |
| `harnessly_runtime_build_info{version,goversion,os,arch}` | gauge | always 1 |
| `harnessly_runtime_start_time_seconds` | gauge | when the process started |
| `harnessly_runtime_journal_lines_total{level,source,event}` | counter | the journal's lines; past 500 label sets the rest count as `event="_other"` |
| `harnessly_runtime_journal_dropped_total` | counter | lines the journal lost to a full queue or a full hour |
| `harnessly_runtime_otlp_dropped_total` | counter | lines the OTLP export lost while its collector did not take them |
| `harnessly_runtime_goroutines`, `harnessly_runtime_heap_alloc_bytes`, `harnessly_runtime_sys_bytes`, `harnessly_runtime_gc_total` | | the process |

The hub has its own metrics, `harnessly_hub_*` ([Running a hub](../hub/deploy.md)).

## Example: Grafana Alloy

```alloy
otelcol.receiver.otlp "default" {
  http { endpoint = "127.0.0.1:4318" }
  output { logs = [otelcol.exporter.loki.default.input] }
}
otelcol.exporter.loki "default" { forward_to = [loki.write.default.receiver] }
loki.write "default" { endpoint { url = "http://loki:3100/loki/api/v1/push" } }

prometheus.scrape "harnessly" {
  targets    = [{"__address__" = "127.0.0.1:9464"}]
  forward_to = [prometheus.remote_write.default.receiver]
}
prometheus.remote_write "default" { endpoint { url = "http://prometheus:9090/api/v1/write" } }
```

and in the runtime's environment (`~/.config/harnessly/env`, or the unit):

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
RUNTIME_METRICS_LISTEN=127.0.0.1:9464
```

A useful alert: `sum by (source, event) (rate(harnessly_runtime_journal_lines_total{level="error"}[10m])) > 0.1`.
