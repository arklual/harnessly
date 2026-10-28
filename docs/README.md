# Documentation

| Page | What it covers |
| --- | --- |
| [Getting started](getting-started.md) | build, run the runtime standalone, open tabs, run agents, connect the machine agent |
| [Architecture](architecture.md) | the runtime gate and its two modes, the core, tmux, the machine agent, the repository layout |
| [Slices](slices.md) | the slices of routes, switching them per machine, the shadow comparison |
| [Configuration](configuration.md) | every `RUNTIME_*` and `AGENT_*` environment variable |
| [Deploy](deploy.md) | plain commands, systemd, Docker, docker compose with boards and artifacts, Helm, Argo CD |
| [Voice input](voice.md) | the microphone in the message field: what it needs, the built-in recognition, how to change or turn it off |
| [Design pages](design-pages.md) | the page model of a design project (meta/), the page commands, the migration, the canvas.json guard |
| [The safe](safe.md) | secrets for the agents without the chat: storage, the ask flow, the threat model |
| [Integrations](integrations.md) | boards and artifacts beside the conversation, the agents' environment, the agent skills |
| [The hub](hub/deploy.md) | running a hub with sign-in for a team: [deploy](hub/deploy.md) (compose, Caddy, systemd, Helm), [connecting a machine](hub/machine.md), [architecture](hub/architecture.md), [the contract of the hub, the agent link and the UI](hub/api.md) |

Brand assets (wordmarks, the square avatar and the GitHub social preview) are in [brand/](brand).
