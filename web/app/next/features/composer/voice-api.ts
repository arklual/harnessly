// Voice over v2 (POST /api/v2/voice): a whole recording in, what was said out — "" for silence. The machine recognizes it
// piece by piece with the person's engine and may take up to its five minutes for a long one (HRN-674), so the page waits
// a little longer than that.
import { wordedPage } from '@/next/core/net/worded';
import { parsePage, parseRefusal, type MachineId } from '@/next/protocol/v2';
import type { TDictationLang } from '@/next/stores/voice';

const RECOGNIZE_MS = 310_000;

export const transcribe = (m: MachineId, recording: Blob, lang: TDictationLang, signal: AbortSignal) =>
  wordedPage(m, `/voice?lang=${lang}`, parsePage.voice, (v) => ({ ...parseRefusal(v), message: '' }), {
    method: 'POST',
    body: recording,
    signal,
    timeoutMs: RECOGNIZE_MS,
  });
