/** Shared shape of the dictation engines: in-tab Whisper, browser Web Speech API and server ASR. */
import { t } from '@/lib/i18n';

export type TVoiceEngineKind = 'local' | 'browser' | 'server';

export interface IVoiceEngineHandlers {
  /** The whole text recognized so far in this dictation; replaces the previous partial. */
  onPartial: (text: string) => void;
  /** The dictation is over and this is its whole text. */
  onFinal: (text: string) => void;
  /**
   * `unavailable`: the engine cannot work here at all (no recognition service on the machine, model download
   * failed, the browser vendor's service is unreachable) and «Auto» should try the next one.
   */
  onError: (message: string, unavailable?: boolean) => void;
  /** The engine stopped itself (socket closed, recognition service died) without start()/stop() asking for it. */
  onEnd: () => void;
  /** The microphone is open and audio is being recorded, possibly before the recognizer is ready (it catches up from a buffer). */
  onCapture?: () => void;
}

export interface IVoiceEngine {
  /** Resolves once audio is flowing; rejects with {@link VoiceEngineUnavailableError} when «Auto» should move on. */
  start: () => Promise<void>;
  /** Graceful stop: asks for one last final transcription before ending. */
  stop: () => void;
  /** Immediate cancel: no final expected, nothing recognized so far is kept. */
  abort: () => void;
}

export class VoiceEngineUnavailableError extends Error {}

export const VOICE_MIME_CANDIDATES = [
  'audio/webm;codecs=opus',
  'audio/webm',
  'audio/mp4',
  'audio/aac',
] as const;

/** First MediaRecorder mime type the current browser can record, in preference order; `null` if none can. */
export const pickVoiceMimeType = (isSupported: (mime: string) => boolean): string | null =>
  VOICE_MIME_CANDIDATES.find((mime) => isSupported(mime)) ?? null;

const MIC_ERRORS: Record<string, string> = {
  NotAllowedError: t('lib.voiceEngine.micDenied'),
  SecurityError: t('lib.voiceEngine.micDenied'),
  NotFoundError: t('lib.voiceEngine.micNotFound'),
  NotReadableError: t('lib.voiceEngine.micBusy'),
  AbortError: t('lib.voiceEngine.micAborted'),
};

/** A getUserMedia failure as the person should read it. */
export const describeMicError = (error: unknown): string => {
  const name =
    error instanceof Error || (typeof error === 'object' && error !== null && 'name' in error)
      ? (error as { name: string }).name
      : '';
  if (MIC_ERRORS[name]) return MIC_ERRORS[name];
  return error instanceof Error && error.message ? error.message : t('lib.voiceEngine.micFailed');
};

const MIC_CONSTRAINTS = { channelCount: 1, echoCancellation: true, noiseSuppression: true } as const;

/**
 * Opens the microphone. The chosen device is a runtime-wide setting shared by every device the person uses, so
 * on a phone it names the laptop's microphone: a device that is not here falls back to the default one.
 */
export const openMicrophone = async (deviceId: string | null): Promise<MediaStream> => {
  if (!navigator.mediaDevices?.getUserMedia) throw new Error(t('lib.voiceEngine.noMediaDevices'));
  if (deviceId) {
    try {
      return await navigator.mediaDevices.getUserMedia({
        audio: { ...MIC_CONSTRAINTS, deviceId: { exact: deviceId } },
      });
    } catch (error) {
      const name = (error as { name?: string } | null)?.name;
      if (name !== 'OverconstrainedError' && name !== 'NotFoundError') throw error;
    }
  }
  return navigator.mediaDevices.getUserMedia({ audio: { ...MIC_CONSTRAINTS } });
};
