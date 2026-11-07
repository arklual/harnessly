# Guest links of designs and `harnessly doctor share`

A guest link opens a design to people without a session. The hub signs a short token for the link and fetches the
project's files from the machine; on the machine, the only thing between a guest's path and the rest of the disk is
how the runtime opens it:

- **one confined lookup**: the path is resolved beneath the project folder (`os.Root`), so `..`, a relative or an
  absolute symlink, and a symlinked directory that point out of it are refused;
- **one stable handle**: the file is served from the handle that lookup returned, so a path swapped after the lookup
  (a rename over it) does not change what the guest gets.

A runtime whose filesystem does not keep these guarantees cannot serve guests safely. A PNG and an MP4 that render
prove nothing here: `harnessly doctor render` passes in such a runtime.

## The probe

Before it serves a guest or lets the hub make a link, the runtime checks the guarantees themselves in a throwaway
folder made inside the design library (the same filesystem the projects are on) and removes it after. The result is
kept an hour when every check holds and a minute when one fails, so a fixed runtime is seen again soon.

While a check fails, guest sharing is **closed**, never weakened:

- every guest read answers 503 with `X-Design-Share-Refused: share_unsafe_runtime`; the hub shows the guest
  «Владелец пока не может открыть этот дизайн по ссылке…» rather than «machine is down»;
- the hub makes no link, pinned or not: the owner's dialog gets 409 `share_unsafe_runtime` with what to run instead;
- links already made stay as they are and open again once the probe holds.

Run the same check by hand, on the machine, as the runtime's user:

```sh
harnessly doctor share                 # RUNTIME_DESIGN_LIBRARY_DIR, else RUNTIME_WORKSPACE_DIR/design-artifacts
harnessly doctor share --library DIR --json
```

It exits 1 when a check fails.

| Code    | Check               | What failed                                                                  |
| ------- | ------------------- | ---------------------------------------------------------------------------- |
| SHR-100 | `setup`             | the probe could not make its folder in the library (missing, not writable)   |
| SHR-101 | `inside`            | a file inside the folder does not open through the confined lookup           |
| SHR-201 | `dotdot`            | `..` leaves the folder                                                       |
| SHR-202 | `relative-symlink`  | a relative symlink out of the folder is followed                             |
| SHR-203 | `absolute-symlink`  | an absolute symlink out of the folder is followed                            |
| SHR-204 | `directory-symlink` | a symlinked directory out of the folder is followed                          |
| SHR-301 | `swap`              | an open file reads what was renamed over its path after it was opened        |

A check that cannot run counts as failed.

## Where guest links work

Checked to pass: Linux run directly, a virtual machine, a container under the default `runc` or `crun` runtime.

Known to fail: sandboxes that emulate the kernel's filesystem, such as gVisor (`runsc`). Keep guest sharing off there
or move the runtime; there is no switch that skips the probe.

Other runtimes are not listed until they are checked: run `harnessly doctor share` there. If a probe result looks wrong
for a runtime, report it privately to the maintainers, not in a public issue: the details of a containment failure are
security-sensitive.
