# The portable pack

One file with Sinopia projects and the UI kits they are pinned to, to carry to another harnessly (a closed network, no
outside access) and open there. A project pinned to a kit version draws only where that version is installed, so the
pack brings both.

```
harnessly design pack export <project…> [--all] [--kit ID[@VERSION]…] --out FILE [--accept-restricted] [--library DIR] [--kits DIR] [--json]
harnessly design pack import <file> [--dry-run] [--library DIR] [--kits DIR] [--json]
```

Both work on the library and the kits folder directly, without a running runtime. The same checks are routes of the
runtime: `GET /api/platform/design/pack/export?project=<id>&project=<id>` (or `all=1`, `kit=<id>[@version]`,
`acceptRestricted=1`) and `POST /api/platform/design/pack/import` (`dryRun=1`, `upload=<id>` for a chunked upload).

## The file

A `.tar.gz` whose first entry is `sinopia-pack.json`:

```
{ "format": "sinopia-pack", "schema": 1, "exportedAt": "…",
  "projects": [ { "id", "title", "kit": {"id","version"}, "files", "bytes", "sha256" } ],
  "kits":     [ { "id", "version", "file": "kits/<id>/<version>.kit.tar.gz", "size", "sha256", "requires": […] } ],
  "files":    [ { "path", "size", "sha256" } ],     every other entry of the file
  "sha256": "…" }                                    hash of the sorted "path size sha256" lines of files
```

- `projects/<id>/…` are the project's files as «Весь проект» takes them: `meta/` (pages with their ids, notes, the pin
  to a kit version), `project/` (the boards and what they load), `artifact.json`. Service folders (a name starting with
  a dot), files that look like credentials and the agent's instruction files (`CLAUDE.md`, `AGENTS.md`…) stay out, on
  the way in as well.
- `kits/…` is each kit as its own export (`design kit export`, with its `kit-export.json`), parents (`requires`)
  included. So a kit's own checks, the refusal to overwrite a version and the license confirmation are the kit's.

## Export

Packs the named projects (`--all`: the whole library) and every kit version they are pinned to, with the kits those
require. `--kit` adds a kit on its own. A project pinned to a kit that is not installed here is an error that names it.
Kit files with a restricted license (a corporate font) are packed only with `--accept-restricted`; the export lists them
first. A file that changes while the pack is written fails the export instead of getting a hash that is no longer true.

## Import

1. The file is unpacked aside (regular files only, names inside the folder) and the manifest is checked against the
   bytes: every listed file with its size and sha256, no unlisted file, the manifest's own hash. A pack from a newer
   `schema` is refused with a message saying so; nothing is written.
2. The plan: each kit version is *added*, *unchanged* or in *conflict* (installed with other content). A conflict
   refuses the whole import and writes nothing (a kit version never changes). Each project is *added*, *renamed* (its
   id is taken by another project: it gets `id-2`, …, and the other project is not touched) or *unchanged*.
3. `--dry-run` stops here and prints the plan.
4. Otherwise the kits are installed, parents first, then each project is placed as one rename. Page ids and the kit pin
   are kept as they are in the files.

Importing the same pack again changes nothing: the library remembers (`<library>/.pack-imports.json`) which project a
pack's project became, even after it was edited here. Nothing goes out to the network.

Not in this stage: buttons in the interface, taking a folder of projects as a unit, a pack in the hub's shared storage. The pack carries no `.owner.json` (a hidden file stays out), so the import writes one: the importer owns what it brought, as a project they made.

## Перенос на другой инстанс

Пошагово, для человека. Старый инстанс — тот, где проекты; новый — куда их переносят (в том числе в закрытой сети).

1. **Выгрузить** на старом (папки по умолчанию подходят, если не заданы иные):
   `harnessly design pack export <проект> [<проект> …] --out portable.tar.gz`
   (`--all` — вся библиотека; `--kit <id>[@версия]` — кит отдельно). Команда печатает проекты, киты и sha256 пакета.
   Если у кита есть файлы с ограниченной лицензией (корпоративный шрифт), она назовёт их и остановится: запустить ещё раз
   с `--accept-restricted`, когда это разрешено.
2. **Перенести файл** любым разрешённым способом (флешка, внутренняя шара). Это один файл; открывать его не нужно.
3. **Проверить** на новом, ничего не записывая:
   `harnessly design pack import portable.tar.gz --dry-run`
   Отчёт: каждый кит — `added`, `unchanged` или `conflict`; каждый проект — `added`, `renamed` (id занят, будет `id-2`)
   или `unchanged`; предупреждения (проект на ките, которого нет ни в пакете, ни здесь).
4. **Загрузить**: `harnessly design pack import portable.tar.gz`. Повторная загрузка того же файла ничего не меняет.
5. Если рантайм на новом инстансе уже запущен, проект появится в библиотеке сам (папка библиотеки читается с диска);
   киты читаются из папки китов при открытии проекта.

Папки: `--library DIR` и `--kits DIR`, иначе `$RUNTIME_DESIGN_LIBRARY_DIR` (или `design-artifacts` в
`$RUNTIME_WORKSPACE_DIR`) и `$RUNTIME_DESIGN_KITS_DIR` (или `design-kits` в папке данных).

### Конфликт версии кита

`conflict` значит: версия кита с этим id уже стоит на новом инстансе с другим содержимым, а версия не меняется.
Импорт отказывается целиком и ничего не пишет; отчёт говорит, сколько файлов добавлено, изменено и убрано. Что делать:

- обычно правильно **выпустить кит новой версией** на старом инстансе (`design kit` — версия растёт, проект
  перезакрепляется) и выгрузить пакет заново;
- если нужны обе копии, поставить кит из пакета под другим id: достать его из пакета (`tar -xzf portable.tar.gz
  kits/<id>/<версия>.kit.tar.gz`) и `harnessly design kit import <файл> --as-id <новый-id>`, затем закрепить проект на нём;
- версию на новом инстансе молча заменять нельзя: проекты, закреплённые на ней, поменяют вид.

### Запасной путь (без `pack`)

Работает на любой версии, где есть `kit export|import`; `pack` его заменяет. Проверено: «Весь проект» и «ZIP проекта»
для этого **не годятся** — импорт архива «Весь проект» отказывается («в проекте нет досок `*.dc.html`»), а «ZIP проекта»
теряет `meta/`: закрепление на ките, id страниц, заметки.

1. Кит, на старом: `harnessly design kit export <id> --version <V> --out kit.tar.gz`.
2. Кит, на новом: `harnessly design kit import kit.tar.gz` (родителей из `requires` ставить первыми).
3. Проект, на старом: `tar -C <библиотека> -czf p.tgz --exclude='.*' <id>`.
4. Проект, на новом: `tar -C <библиотека> -xzf p.tgz`. Если такой id уже занят, сначала переименовать папку в архиве
   или распаковать во временную папку и переложить. Закрепление, id страниц и заметки живут в `meta/`, поэтому
   папка копируется целиком.

## Kits between the machines of one instance

Under a hub the kits folder syncs like the library does, as the `kits` collection of the space: `<kit id>/<version>/<file>`.
A published version is immutable: a file already in the room is not overwritten and not deleted (a different write
is answered as a conflict, and the machine keeps its own copy in `.conflicts`); the same bytes again are no news. A
draft is a project of the library and syncs there. Staging folders and every hidden name stay on their machine.
Access is the store's: a live session of a machine of the space. Kits with a restricted license (a corporate font) sync
to the space's machines too; leaving the space still goes through `--accept-restricted`.

### The seal of a version

A version is one unit: `<id>/<version>/kit.sealed.json` (a plain name, so it syncs) lists its files with sizes and sha256
and a hash over the list. Every install writes it last.

- **Installed means sealed and whole.** `kit list`, `kit show`, a project's pin, `kit check` and the exports see a version
  only when its seal is there and every listed file is in place with the listed bytes. A version still arriving from
  another machine is not installed yet; a file the seal does not list is not part of the version (not served, listed or
  exported). Looked at again every 10 s, hashed again when a file's size or time moves.
- **The room.** Before the seal, files may be added to a version and none changes. The seal is taken when it is well
  formed (it may come before its files); after it the folder takes only the files it lists with the bytes it lists, and
  nothing is changed or deleted. The refused write is a 403: the machine keeps the file and does not send it.
- **A race.** Two machines laying out one version with different bytes: whoever's file or seal lands first stays; the
  other machine's copy goes to `.conflicts` in its kits folder, with the winner's bytes in its place.
- **Versions from before seals.** The runtime seals them in one pass when it starts, before anything syncs, and marks the
  kits folder (`.seal-required`); from then on a version without a seal is one still arriving. In a kits folder without
  the mark (nothing has started on it) a version without a seal is installed as it always was.
- `kit install --force` replaces a local version and seals the new one; the room refuses it, so it stays local.

Writers are machines of one space, as for the library.
