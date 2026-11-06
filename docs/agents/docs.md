# The documentation, from the command line

The documentation the person reads on the /help page is yours too, through `harnessly docs`. It needs no runtime and
no network: the built-in pages are in the binary, the team's pages are files on the machine.

```bash
harnessly docs                         # the sections and their pages, and where the team's pages live
harnessly docs search hub deploy       # the pages with every word, best first, with the line that matched
harnessly docs read self-host/own-docs # print a page
harnessly docs add runbooks/deploy-api ./deploy-api.md --section-title "Runbooks"
```

## Read before you guess

When the person asks how something in harnessly works, how to set something up, or how this team does a thing,
search the docs before you answer from memory: the team's own pages are there, and they win over what you assume.

## Add what was missing

When you worked something out that the docs did not have (a deploy procedure, a fix for a recurring error, how a
service of the team is wired), offer to write it down. With the person's yes:

```bash
harnessly docs add runbooks/deploy-api --title "Deploy the API" --picture ./after-deploy.png <<'MD'
Deploy from main only: CI builds the image, the rollout is a merge into `deploy/prod`.

![The pipeline after a deploy](img/after-deploy.png)

## Roll back
…
MD
```

- **SECTION/PAGE** are lower-case latin names with dashes: `runbooks/deploy-api`. A new section is made with
  `--section-title` (and `--audience people|agents|all`); adding to a built-in section (`start/our-vpn`) keeps its
  title. A page with the name of a built-in page replaces it on this machine.
- The first line of the page is its title (`# …`), or `--title` puts one there.
- **Pictures**: `--picture FILE` (png, jpg, gif, webp, svg, up to 8 MB) copies it next to the page as `img/<name>`;
  show it with `![what it shows](img/<name>)`. A screenshot of the real screen beats a description of it.
- **Diagrams**: a fenced `mermaid` block is drawn on the page, as in the chat. Prefer it to a picture of a diagram:
  the next agent can change it.
- A page that exists is not written over without `--replace`.
- It shows on the /help page at once; `harnessly docs path` says which folder it went to.

## How to write a page

- One page, one question: "how do we deploy the API", not "everything about the API".
- Commands that can be copied and run as they are; what to expect after each one.
- The why in one sentence where it is not obvious; no history of how it came to be.
- Link to the other pages by their files (`../self-host/own-docs.md`), they open inside the documentation.
