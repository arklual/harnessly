# 0001-data-to-hub: a machine's own data into the hub

A runtime of a version that keeps a person's data in the hub (the store, [docs/hub/store.md](../hub/store.md)) moves
what an older version kept only on its disk, by itself, the first time it starts linked to a hub that has a store. This
page is what an agent reads when the move stops: `harnessly migrate status` names it, `hub doctor` prints its codes,
and the «Продолжить с агентом» button of a failed move opens a chat with it.

## What moves, and where

| Kind | On the machine (before) | In the hub (after) |
|---|---|---|
| `uploads` | `<data>/v2/uploads/<id>`: pictures and files people put into chats | The space's `uploads` collection, the blob by its sha256; `upload_refs` binds the upload id to it for the machine's person |
| `files` | Files the agents handed over (`harnessly deliver`), found by their records in the chats' transcripts | The space's `files` collection, by sha256 |
| `handoff` | `<data>/v2/handoffs/…`: handoff and merge notes, 24 h | The space's `handoff` collection, by sha256 |
| `sidebar` | The machine's projects and favorites (one object, `organizer`) | The person's sidebar (`history_sidebars`), merged as the page merges a machine: a project joins the profile's project of the same name or becomes its own, the favorites' groups join by name, the loose ones go after the loose; the favorites once per machine, a new project whenever it appears. Sent with `POST /store/migration/v1/sidebar`; `verified` once the sidebar lists the machine in `merged` |

Two kinds of data reach the hub by their own way and are not counted here: the Sinopia library (the library sync keeps
it in the space's `design` collection, `docs/hub/store.md`) and the chats (the machine's agent copies them into the
hub's history by its own cursor, `docs/hub/history.md`).

Nothing is deleted on the machine. The local copy stays while the move runs, after it is verified, and for three more
versions of the runtime after that: rolling back to an older version works, because the machine still holds everything.

## How it runs

1. **Inventory.** The runtime lists every object of the kinds above and its size.
2. **Upload.** Each object goes into its collection by sha256. The store refuses bytes that do not hash to the name.
3. **Verify.** The runtime asks the store whether it has the blob under that sha256; only then the object is `verified`.
4. **Journal.** Every object's state is reported to the hub (`POST /store/migration/v1/report`) and kept in its
   database, table `migration_objects`: machine × migration × kind × object → state, error code, attempts. A run that
   stopped (restart, network, hub away) goes on where it was; what is verified is not sent again.
5. **Done.** When every object is `verified` or `skipped`, the run is over (`migration_runs.finished_at`).

While the move runs, the web reads what is moved from the hub and the rest from the machine, as before. A person sees
«переносим данные с машины X: 412 из 420».

The move does not end with the old data: new objects keep going into the hub as they come. An upload is put into the
store by the page that sends it, a file an agent hands over by the runtime when it hands it over, and a handoff note
wakes the move as soon as it is written. Besides, the runtime takes a pass every 6 hours, starting 30 seconds after it
starts (`RUNTIME_DATA_MOVE_DELAY`, a Go duration). `RUNTIME_DATA_MOVE=off` turns the move off on a machine.

## Commands

Run them on the machine, in a harnessly tab (they talk to the runtime of that machine):

```sh
harnessly migrate status [--json]      # how far it is, every failed object with its code
harnessly migrate retry [KIND/OBJECT…] # try the failed ones again (all of them without arguments)
harnessly migrate skip KIND/OBJECT…    # leave these on the machine only; the move can be over without them
harnessly migrate verify [--json]      # check every verified object is still in the hub under its sha256
```

On the hub: `hub doctor` lists every machine's move and the codes of what failed.

## Error codes

### MIG-READ

The runtime cannot read the object on its disk: permission denied, an I/O error, a folder where a file was.

Fix: give the runtime's user read access (`ls -l` the path from `harnessly migrate status`; `chown` or `chmod` it), then
`harnessly migrate retry`. If the file is not needed, `harnessly migrate skip uploads/<id>`.

### MIG-GONE

The object was in the inventory and is not on the disk any more (an upload cleaned up, a handoff note expired).

Fix: nothing to move; `harnessly migrate skip KIND/OBJECT`. The next inventory leaves it out by itself.

Before skipping an upload, `ls <path>*`: an upload's bytes are `<id><ext>` (`up_X.png`) beside `<id>.json`. A runtime
without the fix of HRN-982 looked for a bare `<id>` and called such uploads gone: those are not to be skipped — update
the runtime and `harnessly migrate retry`. A skipped object is not moved again.

### MIG-HASH

The bytes read do not hash to what the machine recorded for them: the file changed while it was read, or it is damaged.

Fix: `harnessly migrate retry`. If it fails again, the file on the disk is not what the chat showed; keep a copy aside
and `harnessly migrate skip` it.

### MIG-TOO-BIG

The object is bigger than the store takes (64 MiB for a file of a chat).

Fix: none in the store. It stays on the machine and opens while the machine is on: `harnessly migrate skip` it.

### MIG-PUT

The hub did not take the upload: the network, the hub, or its bucket failed (HTTP 5xx, a timeout, a busy key).

Fix: `harnessly migrate retry` after a while. If it keeps failing, on the hub: `hub doctor` (its `s3` check), the hub's
log around the time of `changedAt`.

### MIG-VERIFY

The upload was answered, and then the store said it does not have the blob under that sha256.

Fix: `harnessly migrate retry`. If it repeats, the hub's bucket loses writes: `hub doctor`, then the bucket's own
console; do not skip such objects, the hub is what is wrong.

### MIG-NOT-KEPT

The hub keeps no place for the object: a sidebar on a hub with its history off (`HUB_HISTORY=off`) or of an older
version. The object is `skipped`, so the move is over without it, and the runtime asks again on each pass.

Fix: nothing on the machine. Turn the hub's history on, or update the hub; the next pass merges the sidebar.

### MIG-SESSION

The machine has no store session: it is not in a space, it was removed or switched off on the hub, or the hub's clock
is far off.

Fix: `hub doctor` on the hub; register the machine again if it was removed (`harnessly-agent register …`). Then the
move goes on by itself.

## How to check

- `harnessly migrate status --json`: `verified + skipped == total`, `failed == 0`.
- `harnessly migrate verify`: every verified object is in the hub.
- Switch the machine off and open its chats in the web: the pictures, the files the agents handed over and the
  Sinopia projects open.

## What not to do

- Do not delete the source files on the machine to "finish" a move: a skipped or failed object is read only from there.
- Do not edit the hub's database by hand (`migration_objects`, `upload_refs`): use `harnessly migrate retry|skip`.
- Do not copy blobs into the bucket by hand: the store keeps a record of every blob in its database too.
