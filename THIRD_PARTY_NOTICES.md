# Third-party notices

The packages bundled into the built page are listed in [web/THIRD_PARTY_NOTICES.md](web/THIRD_PARTY_NOTICES.md). These
are not part of the repository's code but are built into the Docker images or downloaded by the runtime when voice
input is used ([docs/voice.md](docs/voice.md)):

| Component | Licence | Where it comes from |
| --- | --- | --- |
| Inter, Geologica, Manrope, Roboto (`internal/design/vendor/motion/fonts/`) | SIL Open Font License 1.1, texts beside the files | Fontsource builds, served to a video's boards by the library |
| GSAP 3.15 and its plugins (`internal/design/vendor/motion/`) | GreenSock Standard "No Charge" licence, see gsap.com/standard-license | npm package `gsap`, served to a video's boards by the library |
| whisper.cpp (`whisper-server`) | MIT, Copyright (c) 2023-2026 The ggml authors | built from the commit pinned in the `Dockerfile` (`whisper` stage); copied into the images and published by the `whisper-server` workflow |
| Whisper models (`ggml-tiny`, `ggml-base`, `ggml-small`, quantized to 5 bits), weights by OpenAI, ggml conversion by the whisper.cpp project | MIT | downloaded by the runtime on the first dictation from `huggingface.co/ggerganov/whisper.cpp`, checked against the SHA-256 in `internal/voice/engine/manifest.go` |
| ffmpeg (Debian package, not linked: run as a separate program) | LGPL-2.1+ / GPL-2+ as packaged by Debian | `apt-get install ffmpeg` in the Docker images |
