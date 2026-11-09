# When the right pane opens by itself

The right pane shows what the agent works on, but it is the person's space: it comes out only when someone asked for
it. An address the agent writes in its answer is a chip in the text, not an opening.

## Rules

- The pane comes out by itself only for the agent's explicit `open` (`harnessly open …`, or the deployment's name of
  the command) and for what the person clicks.
- An address in the text of an answer becomes a chip; the pane stays as it is.
- Each address opens once: the agent's second `open` of the same tab and target within 10 minutes is answered
  «already opened» and asks nothing of the page.
- The tab the person is in is never taken away: while they are busy in a tab of the open pane, a new one goes behind it
  with a dot.
- On a phone the pane is a sheet over the chat and never comes out by itself; the tabs wait under its button.

## Triggers

| Trigger | Where it is decided | What happens |
|---|---|---|
| The agent's `open <tab> <target>` / `open show` | `internal/coreapi/layoutapi` `cliPanel`, applied by `web/app/next/bindings/right-pane.tsx` | Its tab in front, the pane comes out (not on a phone) |
| The same `open` again within 10 minutes | `paneauto.Repeated` | Nothing; the agent is told it was already opened |
| A design file the agent writes | `internal/paneauto` `Detect` (auto) | Its tab behind with a dot; the pane does not come out |
| A document or board the agent publishes (`artifact publish`, `board create`) | `internal/paneauto` `Detect` (auto) | Its tab behind with a dot; the pane does not come out |
| The same document published again | `internal/paneauto` (auto, reload) | The tab that shows it reloads; nothing opens |
| The agent's browser tool (`chrome_*`) | `internal/paneauto` `Detect` (auto) | The agent's browser tab behind with a dot |
| A file the agent hands over (`deliver`, `deliver_file`) | `paneauto.OpenOnce` (auto) | Its tab behind with a dot; a card in the chat opens it |
| An address in the agent's answer (document, board, stand, dev server, MR, task) | `web/app/next/features/chat` chips and cards | A chip or a card; the pane opens on its click |
| A dev server the agent starts, a stand it deploys | — | Nothing by itself: the agent runs `open` or names it as a chip |
| The person's click on a chip, a card, a view's button | `right-pane.tsx` `openRef`, `applyRequests(…, asked)` | Its tab in front, the pane comes out |

The person can turn off even the quiet tabs: Settings → General → «Open what the agent works on».

## Kinds of addresses

One set of rules (`internal/panel` `Targets.Classify`) tells every address, for the agent's `open`, for the chips the
person clicks and for the runtime's own opening. The chat's cards for documents and boards use the same patterns
(`web/app/next/features/chat/link-card.tsx`), and so do the cards of what a turn made (`internal/outputs`).

| Kind | Exact pattern | Opens in |
|---|---|---|
| Document | `https://<docs host>/a/<slug>` or `https://<platform host><reader path><slug>` (`/agents/docs/<slug>`) | the documents tab |
| Board | `https://<boards host>/b/<slug>` (`?k=<key>` keeps the board's own frame) | the documents tab |
| Stand | `https://<one label><stand domain>/…` (`stand-1.preview.example.org`), no port; `ai-…` and `auth` are not stands | the browser |
| Dev server | `http(s)://localhost:<port>`, `127.0.0.1`, `0.0.0.0`, `[::1]`, a bare port | the browser |
| Merge request, commit | `https://<GitLab host>/<project>/-/merge_requests/<n>`, `/-/commit/<sha>`; GitHub pull requests, commits and issues | the GitLab tab |
| Task | a tracker key (`PROJ-12`) or `https://<tracker host>/issue/<key>` | the task tab |

A document is never the browser: a page of the portal or the platform that is no `/a/<slug>`, `/b/<slug>` or reader
path is a plain site, and a «stand» card is made only for a stand or a port of this machine.
