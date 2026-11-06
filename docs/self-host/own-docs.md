# Your own documentation

The /help page and the agents' `harnessly docs` read two things: the sections built into harnessly, and **the
machine's own documentation folders**. Put your team's pages in such a folder (runbooks, how your services are wired,
your conventions) and they show next to the built-in ones at once, with no rebuild and no restart.

## Where the folder is

| Setting | Folder |
| --- | --- |
| nothing set | `~/.harnessly/docs` (the runtime's data folder, `docs/`) |
| `HARNESSLY_DOCS_DIR=/srv/team-docs` | that folder |
| `HARNESSLY_DOCS_DIR=/srv/team-docs:/srv/ops-docs` | several, separated like `PATH`; `harnessly docs add` writes into the first |

Set the variable in the runtime's environment (the systemd unit's env file, the container, the Helm values); the
runtime passes it to the agents' panes so `harnessly docs add` writes where the page reads. `harnessly docs path`
prints the folders.

## What goes in it

```text
team-docs/
├── runbooks/                 a section of its own
│   ├── section.json          its title, who it is for, where it stands
│   ├── README.md             its first page ("overview")
│   ├── 01-deploy-api.md      a number orders the pages and is dropped from the name: "deploy-api"
│   ├── rollback.md
│   └── img/
│       └── pipeline.png      pictures next to the pages
├── start/                    the id of a built-in section: these pages join it
│   └── vpn.md
├── onboarding/               a folder without section.json is a section too, named after its first page
│   └── README.md
├── Team notes.md             Markdown files in the root make the section "team"
└── ru/                       Russian translations, the same paths: ru/runbooks/rollback.md
```

### section.json

```json
{
  "title": { "en": "Runbooks", "ru": "Регламенты" },
  "audience": "all",
  "order": 20,
  "pages": ["README.md", "01-deploy-api.md", "rollback.md"]
}
```

| Field | Meaning |
| --- | --- |
| `title` | the section's name by language; English is the fallback |
| `audience` | `people`, `agents` or `all`: who it is written for; `harnessly docs` marks it for the agents |
| `order` | where the section stands; the built-in ones are 10 (getting started), 40 (agents), 50 (architecture), 60 (self-hosting), 70 (apps). Without it: after them |
| `pages` | optional: the pages in order. Without it: README first, then every `*.md` by file name |

### Joining or replacing a built-in section

A folder named like a built-in section (`start`, `agents`, `inside`, `self-host`, `build-apps`) adds its pages to that
section. A page with the same name as a built-in one **replaces it on this machine**: write your own `start/README.md`
to greet your team with your own first page. A `section.json` in such a folder may also change the section's title.

## Pages, pictures, diagrams

- A page is Markdown; its first `# heading` is its title, the `##` and `###` headings are its contents.
- Pictures: png, jpg, gif, webp or svg up to 8 MB, by a path relative to the page: `![The pipeline](img/pipeline.png)`.
  Pictures from other sites are not loaded. A translation that has no picture of its own shows the English page's.
- Diagrams: a fenced block marked `mermaid` is drawn, as in the chat:

  ````markdown
  ```mermaid
  flowchart LR
      main --> ci["CI builds the image"] --> deploy["deploy/prod"]
  ```
  ````

- Links to other pages by their files, relative: `[rollback](rollback.md)`, `[the safe](../safe.md)`. They open
  inside the documentation. Raw HTML is not shown.

## Keep it in git

The folder is plain files, so the usual way is a repository of the team's docs cloned into it and pulled on a timer:

```bash
git clone git@git.example.com:team/docs.git /srv/team-docs
# a systemd timer or cron, every 10 minutes:
git -C /srv/team-docs pull --ff-only --quiet
```

Every machine of the team then shows the same pages; a merge into the docs repository is the release.

## Let the agents write it

An agent adds a page with `harnessly docs add` (see [the documentation, from the command line](../agents/docs.md)).
Ask in the chat: "write a runbook of what we just did to restore the database, with the commands and a screenshot of
the result". The page lands in the first folder; with the folder in git, commit it like any other change.
