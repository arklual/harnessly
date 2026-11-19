# Codex defaults of a harnessly chat

Every Codex chat harnessly starts (a new one, a resumed one, a fork) gets these settings as `-c key=value` flags
(`internal/agents/codex/tuning.go`). A key the person wrote in their own `config.toml` is never overridden: no flag is
added for it. A `config.toml` that does not parse gets nothing from this page. Every key was checked against Codex 0.170.

| Key | Default | Why |
| --- | --- | --- |
| `model_context_window` | the model's `max_context_window` from Codex's own catalog (`models_cache.json`; 872k now, Codex runs on 272k or 500k otherwise) | a bigger usable context; ChatGPT sign-in only (an API key is billed per token, and past 200k some models cost double), and not on a vendor's backend profile |
| `model_auto_compact_token_limit` | 80% of that window | compaction comes later, and the summary still has room to work in; the context ring is drawn against the same point |
| `project_doc_max_bytes` | 65536 (Codex: 32768) | a long AGENTS.md chain is no longer cut without a word |
| `background_terminal_max_timeout` | 900000 ms (Codex: 300000) | a build or a test run left in the background is waited for |
| `mcp_servers.<name>.startup_timeout_sec` | 30 (Codex: 10) | an MCP server started through npx is slow on the first start |
| `mcp_servers.<name>.tool_timeout_sec` | 300 (Codex: 60) | the tools that wait for another chat take longer than a minute |

What was measured and left alone: the model, the reasoning effort and its summary, the sandbox and approvals (as the
launch's permission mode says), web search, parallel tool calls, history, the prompt cache (Codex keys it by the
conversation; the instructions and the MCP servers harnessly passes are the same on every start of a chat, so the prefix
stays), and `tool_output_token_limit` (the model asks for its own limit on every command). To change a default for
yourself, write the key in `~/.codex/config.toml`.
