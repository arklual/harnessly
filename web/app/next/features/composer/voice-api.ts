// Voice over v2 (POST /api/v2/voice): a whole recording in, what was said out — "" for silence. The machine recognizes it
// piece by piece with the person's engine and may take up to its five minutes for a long one, so the page waits
// a little longer than that.
import { wordedPage } from '@/next/core/net/worded';
import { parsePage, parseRefusal, type MachineId, type RefuseCode } from '@/next/protocol/v2';
import type { TDictationLang } from '@/next/stores/voice';

const RECOGNIZE_MS = 310_000;

// A refusal of the voice page may carry why the recording failed, worded for the person (no key for the picked model,
// a recording the decoder cannot read); without it the page says its own words for the code.
export const readVoiceRefusal = (v: unknown): { code: RefuseCode; message: string } => {
  const body = v !== null && typeof v === 'object' ? { ...(v as Record<string, unknown>) } : {};
  const message = typeof body.message === 'string' ? body.message : '';
  delete body.message;
  return { code: parseRefusal(body).code, message };
};

export const transcribe = (m: MachineId, recording: Blob, lang: TDictationLang, signal: AbortSignal) =>
  wordedPage(m, `/voice?lang=${lang}`, parsePage.voice, readVoiceRefusal, {
    method: 'POST',
    body: recording,
    signal,
    timeoutMs: RECOGNIZE_MS,
  });
