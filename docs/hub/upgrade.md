# Upgrading as one release

A release is the hub, runtime, agent and command line tool of one version, plus the web page inside the runtime. This
page is about installing it as a set, checking it, and putting it back. The extension protocols a release speaks
([extensions.md](extensions.md)) are part of that set, so an update never silently breaks an extension.

Two things exist side by side and do not replace each other:

| | `/dl/manifest.json` | `/dl/release.json` + `.sig` |
|---|---|---|
| holds | files and hashes | hub, runtime, agent, cli, ui versions, hashes, extension ranges, data rules |
| signed | no (served by the hub) | Ed25519, checked against a key you pinned |
| used by | the automatic update of a machine (`selfupdate`), unchanged | `harnessly upgrade` only |

The automatic update keeps working exactly as before. `harnessly upgrade` is a command someone runs.

## The release manifest

```json
{"schema": 2, "version": "1.4.0",
 "components": {"hub": {"version": "1.4.0", "files": {"hub-linux-amd64": {"sha256": "…", "size": 123}}},
                "runtime": {"version": "1.4.0", "files": {…}, "requires": {"hub": "1.4.0"}},
                "agent": {…}, "cli": {…}, "ui": {"version": "1.4.0"}},
 "extension_api": {"harnessly.machine-provider": {"min": 1, "max": 1}},
 "data": {"schema_version": 14, "additive": true, "min_from": "1.2.0"}}
```

A manifest whose parts disagree (a component requires a newer one than the release holds, a file that does not belong
to its component) is refused. The release builds and signs it with `tools/relmanifest`:

```
go run ./tools/relmanifest keygen release.key release.pub      # once; keep release.key secret
go run ./tools/relmanifest make -version 1.4.0 -dir dist -schema 14 -ext harnessly.machine-provider=1-1
go run ./tools/relmanifest sign -key release.key dist/release.json
```

Put `release.json` and `release.json.sig` next to the binaries the hub serves; it serves them at `/dl/release.json` and
`/dl/release.json.sig` when they are there. (Wiring these three commands into the release pipeline is separate work.)
The cosign signature of `checksums.txt` stays for those who verify the archives.

## Plan

```
harnessly upgrade plan --hub https://hub.example.com --key release.pub --extensions /etc/harnessly/extensions.json
```

Changes nothing. It lists each program as `update`, `install`, `current` or `skip`, the verdict for every registered
extension, and what the release does to the data:

- `compatible`: the release speaks that protocol major.
- `incompatible`: the release lists the protocol but not that major. The plan is **blocked** (exit 1) and `apply` refuses
  unless `--force`.
- `unknown`: the release does not list the protocol; nothing is promised either way.

## Apply

```
harnessly upgrade apply --hub … --key … --extensions … --restart-cmd 'systemctl restart harnessly-runtime' \
    --health-url http://127.0.0.1:8080/readyz
```

1. Every file is downloaded and checked against the signed manifest first. A bad download changes nothing.
2. The programs are replaced together: each old one is renamed to `<name>.prev`, the new one takes its place.
3. `--restart-cmd` runs, then the check: `harnessly-agent`, `harnessly` and the hub must answer `version`, the optional
   `--health-url` must answer 200 within 30 s, the optional `--check-cmd` must exit 0.
4. If anything in 2–3 fails, all the `.prev` files are put back, `--restart-cmd` runs again, and the command exits with
   the reason.

The machine's registration and the data folders are not opened by this command. `harnessly upgrade rollback` puts the
`.prev` programs back later by hand.

## Rolling back the binary is not rolling back the data

Putting the old programs back does **not** put the old data back. The database a new hub migrated stays migrated.

- Hub migrations are **additive** (a table or a column is added, nothing is dropped or rewritten), so the previous
  hub keeps working on the migrated database. `data.additive` in the manifest says whether this holds for the release;
  `plan` prints it, and when it is false the plan says to back up first.
- A release lists `min_from`, the oldest version that upgrades to it directly.
- A change that is not additive ships in two releases: first one that works with both shapes, later the one that drops
  the old shape. Never both at once.

### Order of recovery

1. Binary broken, data fine: `apply` already rolled back, or run `harnessly upgrade rollback`.
2. Binary fine, extension broken: the plan shows it beforehand; install the extension's matching version, or stay.
3. Data damaged (not by an additive migration): stop the hub, restore the last database backup, then put the programs of
   the matching release back. Data written after that backup is lost, which is why the backup comes before a release
   whose manifest says `additive: false`.
