import type { TVoiceEngineKind } from '@/lib/voice-engine';

/** The regex pass is for the English server model only: GigaAM (ru), in-tab Whisper and the browser recognizer already punctuate. */
export const shouldApplyPunctuation = (kind: TVoiceEngineKind, lang: string, setting: boolean): boolean =>
  setting && kind === 'server' && lang !== 'ru';
