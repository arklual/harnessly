# Media providers: video and pictures by API

A media provider makes video and pictures, not text. It is added like any model provider, in **Settings → Providers →
By API**, and its key lives in the machine's safe. The first one is **Higgsfield** (Kling, Seedance, LTX, Soul and the
rest of its catalog behind one key).

## Connect Higgsfield

1. Make a key in the Higgsfield console (`console.higgsfield.ai`). A key is two parts, the key ID and the secret.
2. Settings → Providers → Higgsfield, paste them as `id:secret`. The check asks the provider the price of one request
   (it costs nothing), so a refused key, an expired one and an empty balance are told apart.
3. Higgsfield's API has its own balance in dollars, separate from a subscription; it is seen in the console, the API does
   not give it.

An agent that has no key is told how to ask for one through the safe, outside the chat:
`harnessly secret ask higgsfield-key --why "…"` (the value is `id:secret`). Until a provider holds a key, that secret is
used.

Address and headers (docs.higgsfield.ai): `https://api.higgsfield.ai`, `Authorization: Key <id>:<secret>`.

## Higgsfield MCP 2.0: without a key

Higgsfield's own MCP server (`mcp.higgsfield.ai/mcp`) is a connection of **Settings → Connections → Higgsfield (MCP)**,
signed in with the person's Higgsfield account (OAuth). Its plan, or its free 72-hour trial, pays: no API key. When no
provider holds a key, `generate_video` and `generate_images` (with `provider`/`model`) go through it, and it alone makes
audio (`generate_audio`). `provider: "higgsfield-mcp"` picks it even when a key exists.

- Models are the MCP's catalog: video `seedance` (Seedance 2.5, default), `kling` (Kling 3.0), `minimax`, `marketing`;
  pictures `gpt_image_2_6` (default), `soul` (Soul 2); audio `speech` (Seed Audio, default), `music` (Suno, 15–360 s),
  `sfx` (Mirelo). Any other id of its `models_explore` passes as it is; `params` carries a model's own settings.
- The price is asked first (`estimate_<kind>_cost`, in credits) and counted at $0.07 a credit, the Starter plan's (bigger
  plans pay less), so `max_usd` and the daily cap hold for every plan. The card shows both.
- Frames go up by `media_upload` → PUT → `media_confirm` (an address by `media_import_url`), and the first and last frame
  take the roles the model lists.
- When the person holds trial generations that cover the model, Higgsfield asks what pays: the job stops, the agent asks
  the person and runs it again with `allowance: "trial"` or `"credits"`. A trial generation costs nothing.
- The MCP has no cancel for a generation. An account without a plan or trial answers 403 on `/mcp/workspaces`; the
  agent is told to start one at higgsfield.ai/mcp.
- Everything else of the MCP (presets, Marketing Studio, 3D, GPU boxes, its gallery) agents reach through the
  harnessly-hub tools on the `higgsfield-mcp` connection.

## What an agent can do

| Tool | What it does |
| --- | --- |
| `generate_video` | A clip from one or two frames (image → video) or from a prompt (text → video). `model`: `kling` (default), `seedance`, `ltx` or an endpoint ID. `estimate_only` gives the price and makes nothing. |
| `generate_audio` | Music, speech or a sound effect, through the Higgsfield MCP connection only. The card plays it. |
| `wait_video` / `cancel_video` | Wait for a job that outlived the call; cancel one that the provider has not begun. |
| `generate_images` with `provider` / `model` | A picture from Higgsfield (`soul`, `z-image`) instead of Codex. Without them Codex draws, as before. |

The chat shows a card that fills as the clip is made, with the model and the price. The clip is a file in the job's
folder, and in `save_to` when one is named, beside `<clip>.origin.json`: the provider, the model, the endpoint, the
prompt, the source frames, the request ID, the price, and what the asker added (`origin`).

## Money

- Before a request is sent, the provider is asked its price. The job carries it, and the card shows it.
- A job over `max_usd` (2 by default) is refused and nothing is sent; the agent tells the person the price and asks. A
  machine's jobs of one day stop at $10 together.
- Generation is started only by a person's request or by a task that says to make a clip. Failed and refused-by-moderation
  requests are not charged by the provider; a canceled one is refunded.
- The result is kept by the provider for seven days; harnessly downloads it at once.

## From a design

Sinopia hands the frames of a scene (key frames, clay renders) to `mediagen.Service.Start` with `Owner: "design:<id>"` and
`CopyTo` its video's folder; the clip comes back with the origin file, and the scene, the camera and the outputs the frames
came from go into `origin.extra`.

## Routes

`<platform>/media`: `GET ?id=` or `?conv=&owner=` (jobs), `POST` a request (`?estimate=1` for the price only, `{"cancel": "<id>"}`
to cancel).
