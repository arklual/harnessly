import { useCallback, useEffect, useRef, useState } from 'react';
import useConfigStore from '@/hooks/use-config-store';
import useVoiceHoldHotkey from '@/hooks/use-voice-hold-hotkey';
import useVoiceTapKey from '@/hooks/use-voice-tap-key';
import { createBrowserVoiceEngine, isBrowserVoiceEngineSupported } from '@/lib/voice-engine-browser';
import { createLocalVoiceEngine, isLocalVoiceEngineSupported } from '@/lib/voice-engine-local';
import { createServerVoiceEngine, isServerVoiceEngineSupported } from '@/lib/voice-engine-server';
import {
  VoiceEngineUnavailableError,
  describeMicError,
  type IVoiceEngine,
  type IVoiceEngineHandlers,
  type TVoiceEngineKind,
} from '@/lib/voice-engine';
import {
  isLocalVoiceProven,
  isMobileDevice,
  recordLocalVoiceSpeed,
  resolveVoiceEngine,
  type IVoiceEngineEnvironment,
} from '@/lib/voice-engine-choice';
import { shouldApplyPunctuation } from '@/lib/voice-punctuation';
import {
  EMPTY_VOICE_TRANSCRIPT,
  VOICE_STATE,
  applyAutoPunctuation,
  applyFinalTranscript,
  applyPartialTranscript,
  composeDictatedValue,
  mergeDictation,
  type IVoiceTranscript,
  type TVoiceState,
} from '@/lib/voice-transcript';
import { t } from '@/lib/i18n';

/** Which engine actually ran, once known — for the mic button's tooltip/label. */
export type { TVoiceEngineKind };

export interface IUseVoiceInputOptions {
  value: string;
  onValueChange: (value: string) => void;
  /**
   * Called with the composed text and the recording's id once the final transcript has been applied; the caller
   * decides whether to send (the "send after recording" setting, an Enter pressed mid-dictation). Takes the
   * text as an argument rather than reading it back from `value`: React state updates are batched, so a caller
   * that closed over `value` at render time would still see the old string.
   */
  onAutoSend?: (text: string, recordingId: number, heard: boolean) => void;
  disabled?: boolean;
  /** Whether the push-to-talk hotkey drives this composer; only one composer on screen should own it. */
  hotkey?: boolean;
}

export interface IUseVoiceInputResult {
  state: TVoiceState;
  /** 0..1 input level for the mic button's meter; 0 outside `recording`. */
  level: number;
  error: string | null;
  /** The draft of what is being said; the field itself only changes once the dictation ends. Empty outside recording. */
  preview: string;
  engineKind: TVoiceEngineKind | null;
  /** False while the microphone records but the recognizer is still connecting: what is said now is buffered, not lost. */
  recognitionReady: boolean;
  /** 0..1 while the in-tab model downloads on first use, otherwise `null`. */
  modelProgress: number | null;
  localSupported: boolean;
  browserSupported: boolean;
  serverSupported: boolean;
  /** Whether at least one engine is usable at all, given current settings and this browser. */
  supported: boolean;
  /** Pointer/hotkey down — «hold» mode. */
  press: () => void;
  /** Pointer/hotkey up — «hold» mode. */
  release: () => void;
  /** Click — «toggle» mode. */
  toggle: () => void;
}

const NO_ENGINE_MESSAGE = t('hooks.useVoiceInput.noEngine');

const voiceEnvironment = (): IVoiceEngineEnvironment => ({
  local: isLocalVoiceEngineSupported(),
  browser: isBrowserVoiceEngineSupported(),
  server: isServerVoiceEngineSupported(),
  mobile: isMobileDevice(),
  localProven: isLocalVoiceProven(),
});

/** A press shorter than this is a click: it toggles recording on instead of acting as push-to-talk. */
const CLICK_MAX_MS = 350;

/** An engine that proved unavailable is skipped this long: a recognition service restarting comes back by then. */
const FAILED_ENGINE_TTL_MS = 60_000;

/**
 * Push-to-talk / toggle dictation into the composer: wraps the in-tab, browser and server engines
 * behind one API. Under "Auto" an engine that turns out unavailable here (no recognition service on the machine,
 * a model that cannot load, an unreachable vendor service) is skipped for a minute and the same press carries on
 * with the next one (@/lib/voice-engine-choice); the person only sees an error, with its real reason, once
 * nothing is left to try.
 */
const useVoiceInput = ({
  value,
  onValueChange,
  onAutoSend,
  disabled,
  hotkey = true,
}: IUseVoiceInputOptions): IUseVoiceInputResult => {
  const settings = useConfigStore((s) => s.voiceSettings);
  const [state, setRenderedState] = useState<TVoiceState>(VOICE_STATE.idle);
  /** Mirror of `state`, updated synchronously so handlers and a fallback restart never read a stale value. */
  const stateRef = useRef<TVoiceState>(VOICE_STATE.idle);
  const setState = useCallback((next: TVoiceState | ((prev: TVoiceState) => TVoiceState)) => {
    stateRef.current = typeof next === 'function' ? next(stateRef.current) : next;
    setRenderedState(stateRef.current);
  }, []);
  const [level, setLevel] = useState(0);
  const [preview, setPreview] = useState('');
  const recordingIdRef = useRef(0);
  const [error, setError] = useState<string | null>(null);
  const [engineKind, setEngineKind] = useState<TVoiceEngineKind | null>(null);
  const [modelProgress, setModelProgress] = useState<number | null>(null);
  const [recognitionReady, setRecognitionReady] = useState(true);

  const engineRef = useRef<IVoiceEngine | null>(null);
  /** When each engine last proved unavailable, and why; "Auto" skips it for a while. */
  const failedRef = useRef<Map<TVoiceEngineKind, { at: number; reason: string }>>(new Map());
  const pressedAtRef = useRef<number | null>(null);
  /** A stop was asked for during this recording: an engine that fails afterwards must not restart it on another engine. */
  const stopRequestedRef = useRef(false);
  /** Latest `start`, so an engine handler can restart dictation on the next engine without a stale closure. */
  const startRef = useRef<((lastError?: string) => void) | null>(null);
  const transcriptRef = useRef<IVoiceTranscript>(EMPTY_VOICE_TRANSCRIPT);
  const baseValueRef = useRef('');
  const valueRef = useRef(value);
  const onValueChangeRef = useRef(onValueChange);
  const onAutoSendRef = useRef(onAutoSend);
  useEffect(() => {
    valueRef.current = value;
    onValueChangeRef.current = onValueChange;
    onAutoSendRef.current = onAutoSend;
  }, [value, onValueChange, onAutoSend]);

  const environment = voiceEnvironment();
  const { local: localSupported, browser: browserSupported, server: serverSupported } = environment;
  const supported = resolveVoiceEngine(settings.engine, environment) !== null;

  const applyDictated = useCallback((): string => {
    const composed = mergeDictation(baseValueRef.current, valueRef.current, transcriptRef.current);
    onValueChangeRef.current(composed);
    return composed;
  }, []);

  const start = useCallback(
    (lastError?: string) => {
      // An error does not lock the mic: the next press starts over.
      if (disabled || (stateRef.current !== VOICE_STATE.idle && stateRef.current !== VOICE_STATE.error))
        return;
      const auto = settings.engine === 'auto';
      const now = Date.now();
      const failed = [...failedRef.current].filter(([, failure]) => now - failure.at < FAILED_ENGINE_TTL_MS);
      const kind = resolveVoiceEngine(
        settings.engine,
        voiceEnvironment(),
        new Set(failed.map(([failedKind]) => failedKind)),
      );
      if (!kind) {
        setError(lastError ?? failed.at(-1)?.[1].reason ?? NO_ENGINE_MESSAGE);
        setState(VOICE_STATE.error);
        return;
      }

      stopRequestedRef.current = false;
      baseValueRef.current = valueRef.current;
      transcriptRef.current = EMPTY_VOICE_TRANSCRIPT;
      recordingIdRef.current += 1;
      const id = recordingIdRef.current;
      setPreview('');
      setError(null);
      setEngineKind(kind);
      setRecognitionReady(true);
      setState(VOICE_STATE.requesting);

      const applyPunctuation = shouldApplyPunctuation(kind, settings.lang, settings.punctuation);
      let engine: IVoiceEngine | null = null;
      let heard = false;
      const current = () => engine !== null && engineRef.current === engine;

      /** "Auto" moves on to the next engine as long as this one has not produced any text yet. */
      const fallBack = (message: string): boolean => {
        if (!auto || heard || stopRequestedRef.current) return false;
        failedRef.current.delete(kind);
        failedRef.current.set(kind, { at: Date.now(), reason: message });
        engineRef.current?.abort();
        engineRef.current = null;
        setLevel(0);
        setState(VOICE_STATE.idle);
        startRef.current?.(message);
        return true;
      };

      const handlers: IVoiceEngineHandlers = {
        onCapture: () => {
          if (!current()) return;
          setRecognitionReady(false);
          setState((prev) => (prev === VOICE_STATE.requesting ? VOICE_STATE.recording : prev));
        },
        onPartial: (text) => {
          if (!current()) return;
          heard = true;
          transcriptRef.current = applyPartialTranscript(transcriptRef.current, text);
          setPreview(composeDictatedValue('', transcriptRef.current));
          setState((prev) =>
            prev === VOICE_STATE.error || prev === VOICE_STATE.transcribing ? prev : VOICE_STATE.recording,
          );
        },
        onFinal: (text) => {
          if (!current()) return;
          const spoken = applyPunctuation && text ? applyAutoPunctuation(text) : text;
          transcriptRef.current = applyFinalTranscript(transcriptRef.current, spoken);
          const composed = applyDictated();
          engineRef.current = null;
          setLevel(0);
          setPreview('');
          setState(VOICE_STATE.idle);
          onAutoSendRef.current?.(composed, id, spoken.trim() !== '');
        },
        onError: (message, unavailable) => {
          if (!current()) return;
          if (unavailable && fallBack(message)) return;
          engineRef.current = null;
          setLevel(0);
          setPreview('');
          setError(message);
          setState(VOICE_STATE.error);
        },
        onEnd: () => {
          if (!current()) return;
          engineRef.current = null;
          setLevel(0);
          setPreview('');
          setState((prev) => (prev === VOICE_STATE.error ? prev : VOICE_STATE.idle));
        },
      };

      if (kind === 'local') {
        engine = createLocalVoiceEngine(
          {
            lang: settings.lang,
            deviceId: settings.deviceId,
            onLevel: setLevel,
            onModelProgress: setModelProgress,
            onSpeed: recordLocalVoiceSpeed,
          },
          handlers,
        );
      } else if (kind === 'browser') {
        engine = createBrowserVoiceEngine(settings.lang, handlers);
      } else {
        engine = createServerVoiceEngine(
          { lang: settings.lang, deviceId: settings.deviceId, onLevel: setLevel },
          handlers,
        );
      }

      if (!engine) {
        setError(NO_ENGINE_MESSAGE);
        setState(VOICE_STATE.error);
        return;
      }
      engineRef.current = engine;
      const started = engine;
      engine
        .start()
        .then(() => {
          if (engineRef.current !== started) return;
          setRecognitionReady(true);
          setState((prev) => (prev === VOICE_STATE.requesting ? VOICE_STATE.recording : prev));
        })
        .catch((err: unknown) => {
          if (engineRef.current !== started) return;
          const message = describeMicError(err);
          if (err instanceof VoiceEngineUnavailableError && fallBack(message)) return;
          engineRef.current = null;
          setLevel(0);
          setError(message);
          setState(VOICE_STATE.error);
        });
    },
    [disabled, settings, applyDictated, setState],
  );

  useEffect(() => {
    startRef.current = start;
  }, [start]);

  const stop = useCallback(
    (graceful: boolean) => {
      const engine = engineRef.current;
      if (!engine) return;
      stopRequestedRef.current = true;
      setState((prev) =>
        prev === VOICE_STATE.recording || prev === VOICE_STATE.requesting ? VOICE_STATE.transcribing : prev,
      );
      if (graceful) engine.stop();
      else engine.abort();
    },
    [setState],
  );

  useEffect(
    () => () => {
      // Detach first: the abort makes `start()` reject, and a still-current engine would fall back and reopen the mic.
      const engine = engineRef.current;
      engineRef.current = null;
      engine?.abort();
    },
    [],
  );

  const press = useCallback(() => {
    if (settings.mode !== 'hold') return;
    if (state === VOICE_STATE.recording) {
      // Recording that a click switched on: this press is the click that switches it off.
      pressedAtRef.current = null;
      stop(true);
      return;
    }
    pressedAtRef.current = Date.now();
    start();
  }, [settings.mode, state, start, stop]);
  const release = useCallback(() => {
    if (settings.mode !== 'hold') return;
    const pressedAt = pressedAtRef.current;
    pressedAtRef.current = null;
    if (pressedAt === null) return;
    // A quick click, or a release while the browser was still asking for the microphone, keeps recording on:
    // the next click stops it. Only a real hold ends on release (push-to-talk).
    if (Date.now() - pressedAt < CLICK_MAX_MS || state === VOICE_STATE.requesting) return;
    stop(true);
  }, [settings.mode, state, stop]);
  const toggle = useCallback(() => {
    if (settings.mode !== 'toggle') return;
    if (state === VOICE_STATE.idle || state === VOICE_STATE.error) start();
    // The microphone records from the start: a click while the engine is still getting ready ends the dictation too.
    else if (state === VOICE_STATE.recording || state === VOICE_STATE.requesting) stop(true);
  }, [settings.mode, state, start, stop]);

  useVoiceHoldHotkey(
    () => {
      if (settings.mode === 'hold') start();
    },
    () => {
      if (settings.mode === 'hold') stop(true);
    },
    !disabled && hotkey,
  );

  // The dictation key is a switch whatever the mic button's mode is: a tap starts, the next tap ends.
  const tapKey = useCallback(() => {
    if (state === VOICE_STATE.idle || state === VOICE_STATE.error) start();
    else if (state === VOICE_STATE.recording || state === VOICE_STATE.requesting) stop(true);
  }, [state, start, stop]);
  useVoiceTapKey(settings.key, tapKey, !disabled && hotkey);

  return {
    state,
    level,
    error,
    preview,
    engineKind,
    recognitionReady,
    modelProgress,
    localSupported,
    browserSupported,
    serverSupported,
    supported,
    press,
    release,
    toggle,
  };
};

export default useVoiceInput;
