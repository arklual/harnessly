#!/usr/bin/env bash
# Speaks lines.tsv into out/<id>.wav (24 kHz mono, 1.5 s of silence after) with Piper's Russian voice, plus out/noise.wav:
# a far TV under pink noise, which must not become a task. Needs ffmpeg; Piper and the voice go to ./.piper once.
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p out .piper
[ -x .piper/venv/bin/piper ] || { python3 -m venv .piper/venv && .piper/venv/bin/pip install -q piper-tts; }
V=.piper/ru_RU-dmitri-medium.onnx
for f in ru_RU-dmitri-medium.onnx ru_RU-dmitri-medium.onnx.json; do
  [ -s ".piper/$f" ] || curl -sfL -o ".piper/$f" "https://huggingface.co/rhasspy/piper-voices/resolve/main/ru/ru_RU/dmitri/medium/$f"
done
while IFS=$'\t' read -u 3 id text; do
  .piper/venv/bin/piper -m "$V" -f "out/$id.raw.wav" <<<"$text" >/dev/null 2>&1
  ffmpeg -nostdin -loglevel error -y -i "out/$id.raw.wav" -af apad=pad_dur=1.5 -ar 24000 -ac 1 "out/$id.wav"
  rm "out/$id.raw.wav"
done 3<lines.tsv
ffmpeg -nostdin -loglevel error -y -i out/tv.wav -f lavfi -t 8 -i "anoisesrc=color=pink:amplitude=0.03:r=24000" \
  -filter_complex "[0]volume=0.12[a];[a][1]amix=inputs=2:duration=longest" -ar 24000 -ac 1 out/noise.wav
echo "out/: $(ls out | wc -l) files; serve them: python3 -I serve.py out"
