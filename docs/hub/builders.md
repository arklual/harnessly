# Builders: heavy jobs on machines of their own

Renders, builds and other heavy work used to run on the machine the people's chats sit on and slow them down. A hub keeps
a **pool of builder machines** for it: a heavy job is handed to the hub, the hub places it on an online builder that has
what it needs, the builder runs it in a folder of its own and gives the result files back through the hub. A person can pin
a job to one builder or leave it to «auto». With no online builder the job runs on the submitter's own heavy slots
([`internal/heavy`](../../internal/heavy/heavy.go)) as a draft, with a line that says why.

- [The role of a machine](#the-role-of-a-machine)
- [A job](#a-job)
- [Where a job goes](#where-a-job-goes)
- [From the command line](#from-the-command-line)
- [In the page](#in-the-page)
- [The wire](#the-wire)
- [Limits and safety](#limits-and-safety)

## The role of a machine

A machine is an **agents'** machine (the default: chats are placed on it) or a **builder** (heavy jobs are, chats never).
An owner of the space changes it on the machine's card (Machines → the machine → «Роль машины») or with
`PUT /api/machines/{machineId}/builder {role, caps?, slots?}`. A builder has **labels** (`blender`, `gpu`, `ffmpeg`,
`docker`…) and a number of **heavy slots**. Left empty, they are read from the machine's agent when it is online: the tools
of its list that are installed, `gpu` when it has an NVIDIA device, and the cores' worth of slots (1 up to 4 cores, 2 up to
16, one per 8 above). A builder needs no Claude account: no chat ever starts there.

The machine says yes too. Its agent takes the hub's jobs only with **`AGENT_HUB_JOBS=on`** in its environment: a job is a
command from the hub's people, so the person who runs the machine decides it can be asked for one. The builder's row shows
«not ready: set AGENT_HUB_JOBS=on» until then. `AGENT_JOBS_TOOLS` (comma separated, default `blender,ffmpeg,docker,go,node,
npm,pnpm,npx,python3,make,cargo,git`) lists the programs a job may start, by name; `AGENT_JOBS_CAPS` adds labels;
`AGENT_JOBS_SLOTS` fixes the slots the agent itself allows at once.

## A job

A job is `{need, argv, env?, outputs?, timeoutSec?, hasInput?}`:

- `need` is `{kind: "heavy", caps: [...], machine?}`: the labels the builder must carry, and optionally a builder by id or
  name (an explicit machine passes the same filters; one that does not fit is not replaced silently, the job falls back with
  the reason).
- `argv` is the command, a program name without a path and its arguments: no shell. The builder's tool list decides.
- `hasInput` says a folder follows (`PUT …/input`, a `tar.gz`): it is unpacked as the command's working folder.
- `outputs` are globs relative to that folder; the files they match are handed back, keeping their paths (also when the
  command failed: a render that stopped half way keeps its frames).

Statuses: `queued`, `running`, `done`, `failed`, `cancelled`. The status carries the **choice** (`auto` or the builder named),
the **builder** it went to, the **reason** in words («builder render-1: 1 of 2 slots busy, has blender»), the exit code, the
tail of the log, and the result files `{path, size}`. A queue entry waits for its input, then for a free slot. A job waits
at most 6 hours, its files 20 minutes for the input; cancelling a queued job is at once, a running one goes to its builder.
Jobs are kept 30 days; their files on the hub's disk (`<HUB_DATA_DIR>/jobs/<id>/`) 24 hours after they end.

## Where a job goes

`place(need)` is one function (`internal/hub/jobs/place.go`) that answers a machine and a reason, or «wait», or «local»:

1. filters of the kind (`heavy`): the machine is a builder, online, its agent offers `job.run`, it has every label asked;
2. an explicit machine must be among those; with no free slot the job waits for it;
3. otherwise the least loaded fitting builder (running/slots), then the one with more slots, then by name; all busy → the
   job waits in the queue;
4. no fitting online builder (or no builders at all) → **local**: the submitter's own machine runs it on its heavy slots,
   at most 10 minutes (a draft), with `HARNESSLY_JOB_LOCAL=1` in the command's environment so a render script can lower its
   samples. The reason names what was wrong with each builder (offline, no `AGENT_HUB_JOBS`, no label).

`Kinds` maps a kind to its rules; the placement of chats adds a kind there and does not touch the heavy one.
A job queued for a busy builder that goes away while it waits is turned local the same way.

## From the command line

```sh
harnessly builders                      # name, busy/slots, labels, ready or why not
harnessly job run --need blender --in ./scene --out 'out/*.png' --timeout 30m -- blender -b scene.blend -o out/f_ -f 1
harnessly job run --need blender,gpu --builder render-1 --in ./scene --out 'out/**' -- blender -b -P render.py
harnessly job list | status ID | cancel ID
```

`--builder auto` or none lets the hub choose. The first lines of stderr say where the job went and why; stdout lists the
result files; the exit code is the command's. A machine that is in no hub runs the command here the same way. In Go:
`builders.NewLocal(dataDir, tokenHeader)` gives a `*builders.Client`; `Client.Run(ctx, builders.Spec{…})` does the same
and returns the result files.

## In the page

- The hub's machine card shows the role, the labels and slots, and the tenant's pool with its load.
- A heavy button of the runtime's page (a render) offers the list: `BuilderPicker` (`web/app/next/features/builders`) reads
  `GET <runtime>/api/platform/builders` (`{hub, builders}`) and offers «Авто» first. The chosen id goes to the job as
  `need.machine`; `GET <runtime>/api/platform/jobs?id=` reads a job's status.

## The wire

A machine speaks to the hub in signed HTTP (`POST|GET|PUT /agent/jobs…`, signed under `harnessly-agent-jobs-v1` like the
messages between machines): `builders`, `jobs`, `jobs/{id}` (+`/cancel`, `/finish` for a local run, `/input`, `/out?path=`).
A job is its submitter machine's, and its builder's only while it is placed there. The hub asks a builder over its link:
`job.caps`, `job.start`, `job.poll`, `job.cancel` (feature `job.run`, [api.md](api.md)). The page uses
`/api/spaces/{spaceId}/builders`, `…/jobs`, `…/jobs/{jobId}` (+`/cancel`, `/out?path=`). `HUB_BUILDERS=off` turns all of it
off (404).

## Limits and safety

- Input up to 1 GiB, result files up to 4 GiB in all, an archive may unpack to 8 GiB; links and special files are refused.
- The command runs with no shell, in a folder of the job, a clean environment (`PATH`, `HOME` and `TMPDIR` inside the
  folder), in its own process group that is killed at the timeout or the cancel. `LD_*`, `PATH` and `HOME` cannot be set by a
  job.
- A job's tool must be in the machine's list; a tool that is not gets «refused» with the list. Every start and cancel is a
  line of the machine's audit log.
- Placing and following jobs runs on the leading replica only; a builder that does not answer for 3 minutes fails its jobs.
