# Memory adapter, example

An adapter of the memory protocol `harnessly.memory/v1` ([docs/memory-adapters.md](../../docs/memory-adapters.md)) for
example.com. It keeps the agent's notes in the memory of the process and shows the whole contract in one file with no
dependencies.

```sh
ADAPTER_TOKEN=secret PORT=8080 node --experimental-strip-types adapter.ts   # Node 22+
```

Put the token in the file `adapters.json` names (`tokenFile`), point the runtime at the declaration and restart it:

```sh
RUNTIME_MEMORY_ADAPTERS_FILE=$PWD/adapters.json
```

A note the agent cites by name (`handbook.md`) is then read from the adapter first, and from the files on the machine
when the adapter does not have it. To keep the notes in a wiki or a database, replace the `Map` with calls to it; the
handlers (`list`, `get`, `put`, `search`) stay as they are.
