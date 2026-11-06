# Example extension pack

A pack is what a company puts next to an unmodified harnessly: its own machine provider, memory store, tracker,
secrets and interface. This one is for the invented `example-corp` (`example.com`); copy it and change the names.
How it is built, and why: [docs/extensions/build-your-pack.md](../../docs/extensions/build-your-pack.md).

| Part      | Protocol                        | What it is here                                                          |
| --------- | ------------------------------- | ------------------------------------------------------------------------ |
| Machines  | `harnessly.machine-provider/v1` | `examples/machine-adapter` (Go), built or given with `--machine-adapter` |
| Memory    | `harnessly.memory/v1`           | `adapters/memory.ts`, notes in a JSON file                               |
| Tracker   | `harnessly.connection/v1`       | `adapters/connection.ts`, an invented tracker at `tracker.example.com`   |
| Secrets   | the safe's vault mode           | settings in `runtime.env`, no code                                       |
| Interface | app of kind `ui`                | `ui-app/`: settings, onboarding and object-action views, data version 2  |

## One command

```sh
examples/ext-pack-example/ext-pack.sh install --prefix /opt/example-corp-pack \
    [--machine-adapter ./machine-adapter] [--harnessly harnessly] [--vault-addr https://vault.example.com]
examples/ext-pack-example/ext-pack.sh start  --prefix /opt/example-corp-pack
```

`install` copies the pack, makes a token file for each adapter (mode 0600, kept on a second install), installs the
interface extension with `harnessly app install` when `--harnessly` is given, and writes three files you point the
products at: `hub.env` (the hub), `runtime.env` (the runtime) and `extensions.json` (both read it). It does not edit
the hub or the runtime. `status`, `stop`, `rollback` (the previous install comes back) and `uninstall` do what they say.

Needs node 22.6+ (the adapters are TypeScript that node runs itself) and, without `--machine-adapter`, Go.
Ports: `PACK_PORT_MACHINE`, `PACK_PORT_MEMORY`, `PACK_PORT_CONNECTION` (18099, 18081, 18082).

## Check

```sh
node --experimental-strip-types --test adapters/adapters.test.ts   # the two TypeScript adapters
tools/ci/acceptance-pack.sh                                        # the whole scenario, on a release tag in CI
```
