// The picture of a sound: the browser decodes the file on the person's device, nothing leaves it. Decoding holds all of
// it in memory, and a small file at a low bitrate can unpack to gigabytes, so both its length and its size are limited
// first; a file past either, or of a size nobody states, has no picture, and the player works without one.
import { useEffect, useState } from 'react';
import { peaksOf } from '@/next/features/pane/media/media-model';

const BARS = 180;
const MAX_BYTES = 80 * 1024 * 1024;
/** At the decoder's 8 kHz, two channels of this hold about 115 MB. */
export const WAVE_MAX_SECONDS = 30 * 60;

const decode = async (src: string, signal: AbortSignal): Promise<number[] | null> => {
  const res = await fetch(src, { credentials: 'same-origin', signal });
  const size = Number(res.headers.get('Content-Length'));
  if (!res.ok || !(size > 0) || size > MAX_BYTES) {
    await res.body?.cancel();
    return null;
  }
  const context = new OfflineAudioContext(1, 1, 8000);
  const sound = await context.decodeAudioData(await res.arrayBuffer());
  return peaksOf(sound.getChannelData(0), BARS);
};

/** The wave's bars; null until it is ready, or for a file that has none. */
export const useAudioWave = (src: string, on: boolean): number[] | null => {
  const [peaks, setPeaks] = useState<number[] | null>(null);
  useEffect(() => {
    if (!on) return;
    const abort = new AbortController();
    decode(src, abort.signal).then(
      (next) => {
        if (!abort.signal.aborted) setPeaks(next);
      },
      () => {
        if (!abort.signal.aborted) setPeaks(null);
      },
    );
    return () => abort.abort();
  }, [src, on]);
  return peaks;
};
