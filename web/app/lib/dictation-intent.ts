import type { TVoicePhase } from '@/hooks/use-voice-composer';

export type TDictationKeyAction = 'finish-and-send' | 'finish' | 'pass';

export const dictationKeyAction = (
  phase: TVoicePhase | null,
  key: { key: string; shiftKey: boolean },
  mobile: boolean,
): TDictationKeyAction => {
  if (phase === null) return 'pass';
  if (key.key === 'Escape') return 'finish';
  if (key.key === 'Enter' && !key.shiftKey && !mobile) return 'finish-and-send';
  return 'pass';
};

/**
 * The final transcript can arrive after Enter already asked for a send; one recording is sent at most once.
 * `text` is the whole field. Auto-send needs something actually heard, so a silent recording never sends text
 * that was typed before it; Enter is an explicit request and sends the field either way.
 */
export const shouldSendOnFinal = ({
  recordingId,
  sentRecordingId,
  wantSend,
  autoSend,
  heard,
  text,
}: {
  recordingId: number;
  sentRecordingId: number | null;
  wantSend: boolean;
  autoSend: boolean;
  heard: boolean;
  text: string;
}): boolean => text.trim() !== '' && (wantSend || (autoSend && heard)) && recordingId !== sentRecordingId;
