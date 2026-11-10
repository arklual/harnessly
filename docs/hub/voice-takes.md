# Dictation recordings in the hub (voice takes)

A dictation recorded in the composer is a *take*. The browser keeps it while it is live; once it is over (recognized or
failed) the page also sends it to the hub, so a recording outlives the browser and the machine and shows in «Записи» on
every device of the person (HRN-989). The recognition itself is the runtime's: its recognition service, the built-in
whisper engine or a cloud engine ([../voice.md](../voice.md)); the hub only keeps what was recorded and said.

Off by default: `HUB_VOICE_TAKES=on`. Off, the routes below answer 404 and the page reads that as «not here», as on a
standalone runtime: the takes stay in the browser.

## API

Under the workspace's platform API (`/agents/api/<name>/voice/takes`), for the signed-in person's own takes only; another
person's id is a 404.

| Request | Answer |
| --- | --- |
| `GET voice/takes` | `{takes: [{id, machineId, at, ms, mime, state, text, audio}]}`, newest first |
| `PUT voice/takes/<id>?machine=&at=&ms=&state=failed\|done` (body: the audio, `Content-Type: audio/…`) | `{take}`; the same id again replaces it |
| `PATCH voice/takes/<id>` `{text?, state?}` | `{take}`; `text: null` clears it |
| `DELETE voice/takes/<id>` | `{ok: true}` |
| `GET voice/takes/<id>/audio` | the audio, 404 when it is gone |

## Keeping

- The audio is kept 30 days (`at` older than that: the audio is emptied when the list is read), the text for good.
- A recording is at most 8 MiB; a person keeps at most 500 takes, the oldest go first.
- Table `voice_takes` (migration 0043), audio as base64 text so one migration serves SQLite and Postgres.
