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

## Speed

A 30-second recording takes a few seconds on two processors with `base` and about three times as long with `small`.
Words appear in the field about every two seconds while you speak, and the whole text is made exact when you stop.
Set `RUNTIME_VOICE_MODEL=tiny` for a very slow machine or `small` for better Russian on a fast one.

## Settings

| Variable | |
| --- | --- |
| `RUNTIME_VOICE_ENGINE=off` | do not start the built-in engine |
| `RUNTIME_VOICE_MODEL=tiny\|base\|small` | the model size |
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

Licences: [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).
