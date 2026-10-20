import type { IVoiceEngine, IVoiceEngineHandlers } from '@/lib/voice-engine';
import { t } from '@/lib/i18n';

/**
 * Browser Web Speech API: streams audio straight to the browser vendor's own recognition service —
 * free and fast where it exists (Chrome desktop today), but the settings UI must say so, since that audio never
 * touches our server. Not in the DOM TS lib, so the shape used here is declared locally.
 */

interface ISpeechRecognitionResult {
  readonly isFinal: boolean;
  readonly length: number;
  [index: number]: { transcript: string } | undefined;
}

interface ISpeechRecognitionResultList {
  readonly length: number;
  [index: number]: ISpeechRecognitionResult;
}

interface ISpeechRecognitionEvent {
  resultIndex: number;
  results: ISpeechRecognitionResultList;
}

interface ISpeechRecognitionErrorEvent {
  error: string;
}

interface ISpeechRecognition {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  start: () => void;
  stop: () => void;
  abort: () => void;
  onresult: ((event: ISpeechRecognitionEvent) => void) | null;
  onerror: ((event: ISpeechRecognitionErrorEvent) => void) | null;
  onend: (() => void) | null;
}

type TSpeechRecognitionCtor = new () => ISpeechRecognition;

const speechRecognitionCtor = (): TSpeechRecognitionCtor | null => {
  if (typeof window === 'undefined') return null;
  const w = window as unknown as {
    SpeechRecognition?: TSpeechRecognitionCtor;
    webkitSpeechRecognition?: TSpeechRecognitionCtor;
  };
  return w.SpeechRecognition ?? w.webkitSpeechRecognition ?? null;
};

export const isBrowserVoiceEngineSupported = (): boolean => speechRecognitionCtor() !== null;

const BCP47: Record<string, string> = { ru: 'ru-RU', en: 'en-US' };

/** The vendor's recognition service is unreachable or refuses this language: «Auto» tries another engine. */
const UNAVAILABLE_ERRORS = new Set(['network', 'service-not-allowed', 'language-not-supported']);

export const createBrowserVoiceEngine = (
  lang: string,
  handlers: IVoiceEngineHandlers,
): IVoiceEngine | null => {
  const Ctor = speechRecognitionCtor();
  if (!Ctor) return null;

  const recognition = new Ctor();
  recognition.continuous = true;
  recognition.interimResults = true;
  if (BCP47[lang]) recognition.lang = BCP47[lang];
  let stoppedByUs = false;
  let failed = false;
  let text = '';

  // Every event carries all results of the session so far: the dictation is their concatenation, and it only
  // ends when recognition does, not at the recognizer's first final result.
  recognition.onresult = (event) => {
    const parts: string[] = [];
    for (let i = 0; i < event.results.length; i++) {
      const part = (event.results[i][0]?.transcript ?? '').trim();
      if (part) parts.push(part);
    }
    text = parts.join(' ');
    if (text) handlers.onPartial(text);
  };

  recognition.onerror = (event) => {
    if (event.error === 'no-speech' || event.error === 'aborted') return;
    failed = true;
    if (event.error === 'not-allowed') handlers.onError(t('lib.voiceEngineBrowser.micDenied'));
    else
      handlers.onError(
        t('lib.voiceEngineBrowser.recognitionError', { error: event.error }),
        UNAVAILABLE_ERRORS.has(event.error),
      );
  };

  recognition.onend = () => {
    if (failed) return;
    if (text) handlers.onFinal(text);
    else if (!stoppedByUs) handlers.onEnd();
    else handlers.onFinal('');
  };

  return {
    start: () => {
      stoppedByUs = false;
      try {
        recognition.start();
      } catch (error) {
        return Promise.reject(error instanceof Error ? error : new Error(String(error)));
      }
      return Promise.resolve();
    },
    stop: () => {
      stoppedByUs = true;
      recognition.stop();
    },
    abort: () => {
      stoppedByUs = true;
      failed = true;
      recognition.abort();
    },
  };
};
