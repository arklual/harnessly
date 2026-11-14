# Harnessly for Raycast

Your Harnessly chats in Raycast, across several hubs at once.

## Commands

- **Connect Hub**: enter the hub address (for example `https://app.example.com`), confirm the code in the browser.
  The extension never sees your password; it keeps a personal token in Raycast's local storage.
- **Chats**: chats of all hubs, filtered by Attention / Working / All, with the agent's summary and the last turns.
  Reply, stop, mark read, open in the browser, start a new prompt in the same project.
- **Harnessly** (ask the agent): type the prompt in the search bar (or right after the command name in Raycast's root
  search) and press Enter. The search bar's dropdown picks the project, grouped by machine; ⌘⇧M picks the model of
  the machine's agents (Claude, Codex, …), ⌘⇧T switches the machine and keeps the project when it has one of the same
  name. All of it is remembered between runs. The answer shows in Raycast while the agent works. ⌘↵ opens the chat in
  the browser, ⌘⇧↵ works in a separate worktree, ⌘L writes a longer prompt.
- **Chats Waiting for You** (menu bar): the number of chats that wait for you, refreshed every minute.
- **Manage Hubs**: list and disconnect hubs.

## Notes

- The hub must have the `client-api` switch on for your account.
- Answering an agent's question or permission request is done in the browser (Open in Browser).
- If a token stops working, use the "Reconnect Hub" action. Tokens can be revoked in the hub's account settings.
- The API is described in `docs/hub/client-api.md` of the Harnessly repository.

## Development

```
npm install
npm run dev
```
