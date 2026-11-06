# Memory adapters

The agent's memory is files: Claude Code keeps notes per project (`<projects>/<entry>/memory/<name>.md`), and synced
session copies sit beside them. The runtime reads them, and the freshest copy of a name wins. That is the default and it
does not change.

A team that keeps its notes in a wiki or a database connects them through a **memory adapter**: a small HTTP service
that speaks the memory protocol. No rebuild of the runtime is needed.

Without a declared adapter nothing changes: the memory is the files, and the adapters are never asked.

## The contract

The store in [`internal/memstore`](../internal/memstore) has four calls:

| Call | Does |
| --- | --- |
| `List` | every note without its text: `name`, `title`, `description`, `modifiedMs`, `version` |
| `Get` | one note with its text, front matter taken off |
| `Put` | writes a note; `ifVersion` refuses to overwrite a note that changed since it was read |
| `Search` | notes whose title or text holds the query (case-insensitive), with a snippet |

`FileStore` is the default implementation. It reads as before and writes only when it is given a folder to write to, so
the runtime never changes a person's notes on its own. `Layered` puts an adapter over the files: the adapter answers
first, and what it does not have, or cannot say because it is unreachable, comes from the files.

## Declare an adapter

Set `RUNTIME_MEMORY_ADAPTERS_FILE` to a JSON file, the same format as the hub's extensions file
([`internal/hub/ext`](../internal/hub/ext)) and the connection adapters ([connection-adapters.md](connection-adapters.md)):

```json
[
  {
    "name": "example-memory",
    "kind": "memory",
    "url": "http://127.0.0.1:8080",
    "tokenFile": "/etc/harnessly/example-memory.token",
    "protocol": "harnessly.memory/v1"
  }
]
```

- `tokenFile` holds the bearer token the runtime shows the adapter. It is a path, never a value.
- Entries of other protocols are left to their own loaders. An adapter that is silent or speaks another protocol is told
  on stderr and skipped; the files go on.
- The file is read once, when the process starts. The first adapter in the file is the one used.

## Protocol `harnessly.memory/v1`

`GET /v1/describe` answers the extension frame's description: `protocol`, `name`, `version` and `capabilities`. The
capabilities are `read` (required: list and get), `write` and `search`. A call whose capability is not listed is never
sent; a note search without `search` reads the notes through `read`. Every other call is `POST` with a JSON body.
`404` means "no such note", `501` or `not_supported` means "I do not do this", `409` means the note changed.

| Call | Body | Answer |
| --- | --- | --- |
| `/v1/list` | `{}` | `{notes: [entry]}` |
| `/v1/get` | `{name}` | the note: entry fields and `body` |
| `/v1/put` | `{name, text, ifVersion}` | the new entry |
| `/v1/search` | `{query, limit}` | `{hits: [entry + snippet]}` |

A name is letters, digits, dot, dash and underscore, ending in `.md`. `version` is an opaque string that changes with
every write. `modifiedMs` is milliseconds since the epoch.

## What reads it

The route the interface uses to open a note the agent cited by name (`memory-note`) reads through the store. The agent's
own tools (Claude Code's memory files) are not changed by an adapter: it is the place a team's notes can be read from,
not a replacement of the agent's own file memory.

An example adapter is in [`examples/memory-adapter-ts`](../examples/memory-adapter-ts).
