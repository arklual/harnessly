# Documents and boards adapter, example

An adapter of the documents protocol `harnessly.docs/v1` ([docs/doc-adapters.md](../../docs/doc-adapters.md)) for
example.com. It keeps documents, their versions, access, comment threads and one board in the memory of the process and
shows the whole contract in one file with no dependencies.

```sh
ADAPTER_TOKEN=secret PORT=8080 node --experimental-strip-types adapter.ts   # Node 22+
```

Put the token in the file `adapters.json` names (`tokenFile`), point the runtime at the declaration and restart it:

```sh
RUNTIME_DOC_ADAPTERS_FILE=$PWD/adapters.json
```

The page's document reader then lists, opens, comments on and shares the adapter's documents instead of the built-in
catalog's. To put a corporate service behind it, replace the `Map` with calls to that service; the handlers stay.
