import { useEffect, useRef, useState, type RefObject } from 'react';
import { ArrowUp, Check, X } from 'lucide-react';
import { VOICE_PHASE, type IVoiceComposer } from '@/hooks/use-voice-composer';
import { WAVE_BAR_STEP, pushWaveLevel, waveBarCount, waveBarHeight } from '@/lib/voice-wave';
import { t } from '@/lib/i18n';

const WAVE_TICK_MS = 70;
const WAVE_TICK_REDUCED_MS = 250;
const WAVE_BAR_WIDTH = 2.5;
/** The browser only really asked for the microphone if the stream is still missing after this long. */
const PERMISSION_PROMPT_DELAY_MS = 300;

const formatElapsed = (ms: number) => {
  const total = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
};

/** Scrolling level history on a canvas as wide as the track; the level is sampled on its own beat, not per audio frame. */
const useWave = (canvasRef: RefObject<HTMLCanvasElement | null>, level: number, running: boolean) => {
  const levelRef = useRef(level);
  useEffect(() => {
    levelRef.current = level;
  }, [level]);
  useEffect(() => {
    const canvas = canvasRef.current;
    const context = canvas?.getContext('2d');
    if (!canvas || !context) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)');
    const dark = window.matchMedia('(prefers-color-scheme: dark)');
    let levels: number[] = [];
    let width = 0;
    let height = 0;
    let colors = { loud: '', muted: '' };
    let frame: number | null = null;

    const readColors = () => {
      const style = getComputedStyle(canvas);
      colors = {
        loud: style.getPropertyValue('--t-text').trim(),
        muted: style.getPropertyValue('--t-text-muted').trim(),
      };
    };
    const draw = () => {
      frame = null;
      const ratio = window.devicePixelRatio || 1;
      context.setTransform(ratio, 0, 0, ratio, 0, 0);
      context.clearRect(0, 0, width, height);
      levels.forEach((value, index) => {
        const barHeight = waveBarHeight(value);
        const loud = value > 0.02;
        context.globalAlpha = loud ? 0.9 : 0.55;
        context.fillStyle = loud ? colors.loud : colors.muted;
        context.beginPath();
        context.roundRect(index * WAVE_BAR_STEP, (height - barHeight) / 2, WAVE_BAR_WIDTH, barHeight, 1.25);
        context.fill();
      });
    };
    const schedule = () => {
      if (frame === null) frame = requestAnimationFrame(draw);
    };
    const resize = () => {
      const ratio = window.devicePixelRatio || 1;
      width = canvas.clientWidth;
      height = canvas.clientHeight;
      canvas.width = Math.round(width * ratio);
      canvas.height = Math.round(height * ratio);
      // Re-fit the history to the new width without adding a bar.
      levels = pushWaveLevel(levels.slice(0, -1), levels.at(-1) ?? 0, waveBarCount(width));
      schedule();
    };
    const recolor = () => {
      readColors();
      schedule();
    };

    readColors();
    const resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(canvas);
    resize();
    const themeObserver = new MutationObserver(recolor);
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme', 'class'],
    });
    dark.addEventListener('change', recolor);

    let timer: number | null = null;
    const tick = () => {
      levels = pushWaveLevel(levels, levelRef.current, waveBarCount(width));
      schedule();
    };
    const startTimer = () => {
      if (timer !== null) window.clearInterval(timer);
      timer = running
        ? window.setInterval(tick, reduced.matches ? WAVE_TICK_REDUCED_MS : WAVE_TICK_MS)
        : null;
    };
    startTimer();
    reduced.addEventListener('change', startTimer);

    return () => {
      if (timer !== null) window.clearInterval(timer);
      if (frame !== null) cancelAnimationFrame(frame);
      reduced.removeEventListener('change', startTimer);
      dark.removeEventListener('change', recolor);
      resizeObserver.disconnect();
      themeObserver.disconnect();
    };
  }, [canvasRef, running]);
};

/** True once `active` has held for `delayMs`; a quick start never flashes the prompt. */
const useDelayedFlag = (active: boolean, delayMs: number) => {
  const [elapsed, setElapsed] = useState(false);
  useEffect(() => {
    if (!active) return;
    const timer = window.setTimeout(() => setElapsed(true), delayMs);
    return () => {
      window.clearTimeout(timer);
      setElapsed(false);
    };
  }, [active, delayMs]);
  return active && elapsed;
};

const useElapsed = (running: boolean) => {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!running) return;
    const startedAt = Date.now();
    const timer = window.setInterval(() => setElapsed(Date.now() - startedAt), 250);
    return () => window.clearInterval(timer);
  }, [running]);
  return running ? elapsed : 0;
};

interface IVoiceRecorderBarProps {
  dictation: IVoiceComposer;
  /** «Send automatically» is on: «Done» sends, so it shows the send arrow. */
  sendsOnFinish?: boolean;
}

/**
 * Recording in the composer, the way Claude shows it: the buttons row turns into «Cancel», a live
 * waveform with a timer, and «Done». The recognized text shows in the field as it is heard (VoiceLiveDraft) and lands there for good on «Done».
 */
const VoiceRecorderBar = ({ dictation, sendsOnFinish = false }: IVoiceRecorderBarProps) => {
  const { voice, phase, finish, cancel } = dictation;
  const recording = phase === VOICE_PHASE.recording;
  const canvasRef = useRef<HTMLCanvasElement>(null);
  useWave(canvasRef, voice.level, recording);
  const askingPermission = useDelayedFlag(phase === VOICE_PHASE.requesting, PERMISSION_PROMPT_DELAY_MS);
  const elapsed = useElapsed(recording);
  const downloading = voice.modelProgress !== null;
  const percent = Math.round((voice.modelProgress ?? 0) * 100);

  const preparing = downloading && percent >= 100;
  const status = askingPermission
    ? t('chat.voiceRecorderBar.allowMic')
    : phase === VOICE_PHASE.transcribing
      ? downloading
        ? t('chat.voiceRecorderBar.preparingRecognition')
        : t('chat.voiceRecorderBar.recognizing')
      : null;

  return (
    <div
      className="tw-voice-bar"
      role="group"
      aria-label={t('chat.voiceRecorderBar.voiceRecording')}
      data-phase={phase ?? undefined}
    >
      <button
        type="button"
        className="tw-cbtn"
        onClick={cancel}
        aria-label={t('chat.voiceRecorderBar.cancel')}
        title={t('chat.voiceRecorderBar.cancel')}
      >
        <X aria-hidden="true" />
      </button>
      <div className="tw-voice-track">
        {status ? (
          <span className="tw-voice-status" aria-live="polite">
            <span className="tw-voice-dot" aria-hidden="true" />
            <span className="tw-voice-status-text">{status}</span>
            {downloading && !preparing && (
              <progress className="tw-voice-progress" aria-hidden="true" max={100} value={percent} />
            )}
          </span>
        ) : (
          <>
            <canvas className="tw-voice-wave" aria-hidden="true" ref={canvasRef} />
            {recording && !voice.recognitionReady && (
              <span className="tw-voice-prep" aria-live="polite">
                {t('chat.voiceRecorderBar.preparingShort')}
              </span>
            )}
            {downloading && (
              <span className="tw-voice-model" title={t('chat.voiceRecorderBar.modelHint')}>
                {preparing
                  ? t('chat.voiceRecorderBar.modelPreparing')
                  : t('chat.voiceRecorderBar.modelProgress', { percent })}
              </span>
            )}
            <span
              className="tw-voice-time"
              aria-label={t('chat.voiceRecorderBar.recordingFor', { elapsed: formatElapsed(elapsed) })}
            >
              <span className="tw-voice-dot" aria-hidden="true" />
              {formatElapsed(elapsed)}
            </span>
          </>
        )}
      </div>
      <button
        type="button"
        className="tw-cbtn tw-cbtn-primary"
        onClick={finish}
        disabled={phase === VOICE_PHASE.transcribing}
        aria-label={sendsOnFinish ? t('chat.voiceRecorderBar.doneSend') : t('chat.voiceRecorderBar.done')}
        title={sendsOnFinish ? t('chat.voiceRecorderBar.doneRecognizeSend') : t('chat.voiceRecorderBar.done')}
      >
        {sendsOnFinish ? <ArrowUp aria-hidden="true" /> : <Check aria-hidden="true" />}
      </button>
    </div>
  );
};

interface IVoiceLiveDraftProps {
  /** The field's text before the recording started. */
  value: string;
  /** What has been heard so far. */
  preview: string;
  className?: string;
}

/**
 * The words being dictated, written into the field as they come, the way ChatGPT and Claude do it: the
 * field shows its text and the draft after it, muted until «Done» makes it real text.
 */
export const VoiceLiveDraft = ({ value, preview, className }: IVoiceLiveDraftProps) => {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = ref.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [preview]);
  const gap = value !== '' && !/\s$/.test(value) ? ' ' : '';
  return (
    <div ref={ref} className={`tw-voice-live ${className ?? ''}`} aria-live="off">
      {value}
      {gap}
      <span className="tw-voice-live-draft">{preview}</span>
    </div>
  );
};

export default VoiceRecorderBar;
