# Telemetry

A released harnessly runtime sends its makers **anonymous error reports and a daily heartbeat**, so they see what
breaks on machines they do not run. It is on by default and off with one switch. A runtime built from a checkout
sends nothing: the address is linked into release builds only.

## What is sent

| Event | When | Properties |
| --- | --- | --- |
| `$exception` | an error of harnessly itself: its page (`web`), the runtime (`runtime`), Sinopia (`sinopia`) | where it was seen and what happened (`web.exception`, `runtime.http`, …), the message with every address, e-mail, home folder, IP address, id, token and long number masked and cut to 300 characters, the HTTP status |
| `harnessly_heartbeat` | at start and once a day | hours since start, whether `claude` and `codex` are installed |

Every event also carries the version, the operating system, the architecture, the mode (`standalone` or `cluster`) and
a random install id kept in `<data dir>/telemetry.json`. No person profile is made. An error with the same message goes
once an hour; at most 60 different errors an hour go.

Never sent: your chats, prompts, files, terminal output, the names of your projects, folders or machines, e-mail
addresses, the errors of your own project in the preview, the agents' tool calls.

## Turning it off

Any of these, the first that says off wins:

| Where | How |
| --- | --- |
| the installer | `sh install.sh --disable-telemetry` (writes `RUNTIME_TELEMETRY=off` to the env file, kept across updates) |
| the environment | `HARNESSLY_TELEMETRY=off` or `RUNTIME_TELEMETRY=off` (also `0`, `false`, `no`), or [`DO_NOT_TRACK=1`](https://consoledonottrack.com) |
| the page | Settings → Privacy → «Send anonymous error reports» (kept in `<data dir>/telemetry.json`) |

Turning it off on the page also forgets the install id: turned on again, the machine is a new one. The runtime logs on
start whether telemetry is on and how to turn it off. Off also stops the deployment's own reports
(`RUNTIME_TELEMETRY_URL`, `RUNTIME_JOURNAL_EXPORT=telemetry`).

Official builds send to `https://t.harnessly.ru/batch/` and nowhere else, so a firewall rule on that host is a
fourth way to turn it off. Builds from source have no target at all and send nothing.

## Sending to your own PostHog

A company can collect its machines' errors itself: `HARNESSLY_TELEMETRY_URL` (a PostHog batch endpoint,
`https://posthog.example.com/batch/`) and `HARNESSLY_TELEMETRY_KEY` (the project key) replace the built-in target.

To see the logs and metrics in your own Grafana, Loki or another OpenTelemetry stack, see [Logs and metrics](observability.md).
