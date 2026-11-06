# Voice input

The microphone in the message field records in the browser, the runtime turns the recording into text on the machine
and the words appear in the field. Nothing leaves the machine: the audio is held in memory only, the text is not logged,
and no service of ours or anyone else's is asked to recognize it.

The browser lets a page use the microphone only on `https://` or `localhost`. Behind a plain `http://` address on
another host the button is not offered; put TLS in front (docs/deploy.md) or open the page through `localhost`.

## What it needs

| | |
| --- | --- |
| `ffmpeg` | decodes what the browser records. In the Docker images; the hub installer's `dir` profile installs it, the others say how (`apt-get install ffmpeg`, `brew install ffmpeg`). Without it the microphone is not offered, and the runtime's log says so once. |
| a recognition server | `whisper-server` of [whisper.cpp](https://github.com/ggml-org/whisper.cpp) (MIT). In the Docker images; elsewhere the runtime downloads a build of the pinned release on the first dictation (Linux, amd64 with AVX2, or arm64). On macOS install it: `brew install whisper-cpp`. |
| a model | a multilingual Whisper model, quantized (MIT), downloaded on the first dictation into `<data folder>/voice/models` and checked against its SHA-256: `base` (60 MB) on machines with fewer than 4 processors, `small` (190 MB) from 4 up. |

The first press of the microphone on a machine that has not got the model yet says _Voice input is getting ready
(downloading the model)…_ in the field; what is said meanwhile is kept and is recognized as soon as the download ends.
The download happens once. If it fails (no network), the microphone says why and tries again at the next press; the
button stays where it is.

## Your own recognition server

Settings → Voice input → _Recognize with_ → _Your server_ sends the recordings to any server that speaks OpenAI's
speech-to-text API (`POST <address>/audio/transcriptions`): OpenAI itself (`https://api.openai.com/v1`, model
`whisper-1` or `gpt-4o-transcribe`), a self-hosted [faster-whisper-server](https://github.com/fedirz/faster-whisper-server)
or speaches (`http://127.0.0.1:8000/v1`, model `Systran/faster-whisper-large-v3`), Groq, LocalAI. The runtime then
downloads and runs nothing, so this also works on a machine the built-in engine cannot run on (no AVX2, macOS without
Homebrew's whisper-cpp).

- A recording is sent whole once you stop speaking; no words appear while you speak, so a paid service is asked once
  per recording.
- The address, model and key are kept on the machine in `<data folder>/voice/server.json` (mode 0600). The key goes
  only into the requests' `Authorization` header: the page never gets it back, it shows the last four characters.
  An empty key field keeps the saved key.
- Switching back to the built-in model keeps the server's settings for the next time.
- With `RUNTIME_VOICE_SOCKET` set (a service of the operator's) the choice is not offered.

## Speed

A 30-second recording takes a few seconds on two processors with `base` and about three times as long with `small`.
Words appear in the field about every two seconds while you speak, and the whole text is made exact when you stop.
Set `RUNTIME_VOICE_MODEL=tiny` for a very slow machine or `small` for better Russian on a fast one. Settings → Voice input
also offers `medium` (540 MB) and `large-v3-turbo` (575 MB) for a machine with many processors and memory to spare;
the turbo is the more exact of the two at about the same speed.

## Settings

| Variable | |
| --- | --- |
| `RUNTIME_VOICE_ENGINE=off` | do not start the built-in engine |
| `RUNTIME_VOICE_MODEL=tiny\|base\|small\|medium\|large-v3-turbo` | the model size (a choice in Settings wins) |
| `RUNTIME_VOICE_MODEL_FILE`, `RUNTIME_VOICE_WHISPER_BIN` | your own model file or server (no download, no checksum) |
| `RUNTIME_VOICE_DOWNLOAD=off` | air-gapped: use only the files that are already there |
| `RUNTIME_VOICE_PREFETCH=1` | download at start instead of at the first dictation |
| `RUNTIME_VOICE_THREADS=N` | processors the recognition may use (up to 4 by default) |
| `RUNTIME_VOICE_SOCKET` | use a recognition service of your own over a Unix socket instead (the built-in engine stays out of the way) |

For an air-gapped machine put the files in place by hand: `whisper-server` in `PATH` (or `RUNTIME_VOICE_WHISPER_BIN`)
and the model as `<data folder>/voice/models/ggml-base-q5_1.bin` (or `RUNTIME_VOICE_MODEL_FILE`), then set
`RUNTIME_VOICE_DOWNLOAD=off`.

## How it works

The runtime listens on a Unix socket in its data folder (`voice/asr.sock`, readable by its user only) that speaks the
protocol of `internal/voice` and starts `whisper-server` on a loopback port the first time it has audio to recognize;
the server is stopped after five idle minutes and when the runtime stops. Whisper takes whole recordings, so the words
shown while you speak are the recording so far, recognized again every couple of seconds; silence is not sent to the
model at all. Downloads: the server from this repository's `whisper-server-*` release (built by the `whisper-server`
workflow from the commit pinned in the Dockerfile, checked against the release's `whisper-server.sha256`) and the model
from `huggingface.co/ggerganov/whisper.cpp`, checked against the SHA-256 in `internal/voice/engine/manifest.go`.

## Cloud models and your own words

Settings → Voice input → _Model_ picks who recognizes: the machine (the default; the recording never leaves it),
OpenAI `gpt-4o-transcribe` (Realtime API transcription) or Yandex SpeechKit (v3 streaming, gRPC). A cloud model is
offered once its key is saved in Settings → Models and keys (OpenAI: an API key; Yandex: a service account's API key and,
if the account needs it, the folder id). The keys sit beside the vendors' keys in `<data folder>/providers` (mode 0600)
and go only to that provider. The recording is sent to a provider only while its model is the picked one; text arrives
in the field as you speak, as with the machine's own model.

### The ChatGPT plan (unofficial)

_Model_ → _ChatGPT plan_ recognizes on the plan the machine's Codex CLI is signed in with (`codex login`), with no key
and no separate payment. The recording is held in memory and sent whole after the stop to
`https://chatgpt.com/backend-api/transcribe`, so:

- This is **not a documented API**. OpenAI's
  [plan usage for other apps](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations.md)
  covers the Responses API and says the transcription API is not supported. The address can change or close at any time,
  and using it is at the risk of the account; read OpenAI's terms before relying on it. Use it for your own machine and
  your own sign-in; it is off until you pick it.
- No words appear while you speak; the text comes after the stop. The endpoint takes no prompt, so _My words_ work only
  as replacements.
- The answer carries `asset_ttl: 30d`, so OpenAI appears to keep the recording for 30 days. Do not pick this model for what
  must not leave the machine.
- The model cannot be chosen: the request names none. Limits are not published.
- The request names this client as itself (`User-Agent: harnessly-voice`) and does not pass for one of OpenAI's apps.

_My words_ (in the same section, kept with the account) fix what the recognizer gets wrong. A word may have _how it is
heard_ variants (replaced exactly, ignoring case); without them it is found by how it sounds (a Cyrillic word that is
a transliteration of a Latin one, conservatively: «синопию» becomes `Sinopia`). The replacement is made by the runtime on
every partial and final text for any model. OpenAI also gets the words as a prompt; Yandex SpeechKit v3 takes no
vocabulary, so for it the replacement alone works. A recording sent whole after the stop (a machine without the
socket) is not corrected.


Licences: [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).
