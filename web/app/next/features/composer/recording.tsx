// What the composer shows while the voice is recorded: the time, the wave that breathes with the voice, and
// the field's text with the words just recognized fading in. The numbers of the wave are in wave.ts; here are the canvas
// that draws them and the layer over the field that shows what was said.
import { useEffect, useLayoutEffect, useRef, useState, type Ref } from 'react';
import { createWave, idleLevel, WAVE_MODE, type IWave, type TWaveMode } from '@/next/features/composer/wave';
import { elapsed } from '@/next/i18n/format';

export const RecordingTime = ({ startedAt }: { startedAt: number }) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  return <span className="hx-recbar__time">{elapsed(now - startedAt)}</span>;
};

/** A bar of the wave, and the step from one to the next, in CSS pixels. */
const BAR_W = 3;
const STEP = 5.5;
/** The composer's glow follows the voice: the envelope is written to --hx-voice on the field at most this often changed. */
const GLOW_DELTA = 0.02;
const COLORS_MS = 800;

const reducedMotion = (): boolean =>
  typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const smooth = (x: number): number => x * x * (3 - 2 * x);

// The wave: bars from the microphone's loudness, the newest at the right, the older ones gliding left. The envelope
// of the voice is smoothed in wave.ts so the bars breathe instead of jumping; a gradient of the design's blue and accent
// colours runs across them; the louder a bar the more it stands out.
export const RecordingWave = ({ level, listening }: { level: (() => number) | null; listening: boolean }) => {
  const rootRef = useRef<HTMLSpanElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const waveRef = useRef<IWave | null>(null);
  const feed = useRef<{ level: (() => number) | null; mode: TWaveMode }>({ level, mode: WAVE_MODE.live });
  useLayoutEffect(() => {
    let mode: TWaveMode = WAVE_MODE.busy;
    if (listening) mode = level ? WAVE_MODE.live : WAVE_MODE.idle;
    feed.current = { level, mode };
  });

  useEffect(() => {
    const root = rootRef.current;
    const canvas = canvasRef.current;
    let ctx: CanvasRenderingContext2D | null = null;
    try {
      ctx = canvas?.getContext('2d') ?? null;
    } catch {
      ctx = null;
    }
    if (!root || !canvas || !ctx) return;
    const paint = ctx;
    const wave = (waveRef.current ??= createWave(reducedMotion()));
    const host = root.closest<HTMLElement>('[data-rec]');
    let width = 0;
    let height = 0;
    const size = () => {
      const box = root.getBoundingClientRect();
      const ratio = window.devicePixelRatio || 1;
      width = box.width;
      height = box.height;
      canvas.width = Math.max(1, Math.round(width * ratio));
      canvas.height = Math.max(1, Math.round(height * ratio));
      paint.setTransform(ratio, 0, 0, ratio, 0, 0);
    };
    size();
    const observer = new ResizeObserver(size);
    observer.observe(root);

    let fill: CanvasGradient | string = '#7b61ff';
    let colorsAt = -Infinity;
    const colors = (now: number) => {
      if (now - colorsAt < COLORS_MS) return;
      colorsAt = now;
      const style = getComputedStyle(root);
      const from = style.getPropertyValue('--hx-info').trim() || '#6ea8ff';
      const to = style.getPropertyValue('--hx-accent').trim() || '#7b61ff';
      try {
        const gradient = paint.createLinearGradient(0, 0, width, 0);
        gradient.addColorStop(0, from);
        gradient.addColorStop(1, to);
        fill = gradient;
      } catch {
        fill = to;
      }
    };

    let glow = -1;
    let frame = requestAnimationFrame(function tick(now) {
      const { level: read, mode } = feed.current;
      wave.step(now, mode === WAVE_MODE.live && read ? read() : idleLevel(now), mode);
      colors(now);
      paint.clearRect(0, 0, width, height);
      paint.fillStyle = fill;
      const count = Math.ceil(width / STEP) + 2;
      for (const bar of wave.bars(count, now, mode)) {
        const x = width - bar.at * STEP - BAR_W;
        if (x < -BAR_W) continue;
        const tall = Math.max(BAR_W, bar.h * (height - 2));
        paint.globalAlpha =
          (0.42 + 0.58 * bar.h) * (0.35 + 0.65 * smooth(Math.min(1, Math.max(0, x / (width * 0.3)))));
        paint.beginPath();
        if (paint.roundRect) paint.roundRect(x, (height - tall) / 2, BAR_W, tall, BAR_W / 2);
        else paint.rect(x, (height - tall) / 2, BAR_W, tall);
        paint.fill();
      }
      if (host && Math.abs(wave.envelope - glow) >= GLOW_DELTA) {
        glow = wave.envelope;
        host.style.setProperty('--hx-voice', glow.toFixed(2));
      }
      frame = requestAnimationFrame(tick);
    });
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      host?.style.removeProperty('--hx-voice');
    };
  }, []);

  return (
    <span ref={rootRef} className="hx-wave" data-live={level ? 'true' : undefined} aria-hidden="true">
      <canvas ref={canvasRef} className="hx-wave__canvas" />
    </span>
  );
};

/** The last words kept apart one by one; what is before them is one piece of text. */
const TAIL_WORDS = 48;

interface IPart {
  text: string;
  space: boolean;
}

const partsOf = (text: string): IPart[] =>
  text
    .split(/(\s+)/)
    .filter((part) => part !== '')
    .map((part) => ({ text: part, space: /^\s/.test(part) }));

const keyOf = (index: number, part: IPart) => `${index}:${part.text}`;

/**
 * The field's text drawn over the field while it listens: the same words in the same places, the ones that were not there
 * when the recording began fading in as they are recognized (a word the recognizer takes back for another fades in again).
 * The field's own text is clear meanwhile; the person's caret, selection and keys are the field's. The composer keeps the
 * layer's scroll and width the field's.
 */
export const DictationMirror = ({ text, ref }: { text: string; ref: Ref<HTMLDivElement> }) => {
  // What was in the field when it began to listen: it does not fade in.
  const [before] = useState(
    () => new Set(partsOf(text).flatMap((part, i) => (part.space ? [] : [keyOf(i, part)]))),
  );
  const parts = partsOf(text);
  let words = 0;
  let tail = parts.length;
  while (tail > 0 && words < TAIL_WORDS) {
    tail -= 1;
    if (!parts[tail].space) words += 1;
  }
  return (
    <div ref={ref} className="hx-voice-mirror" aria-hidden="true">
      {parts
        .slice(0, tail)
        .map((part) => part.text)
        .join('')}
      {parts.slice(tail).map((part, i) => {
        if (part.space) return part.text;
        const key = keyOf(tail + i, part);
        return (
          <span key={key} className="hx-voice-word" data-fresh={before.has(key) ? undefined : 'true'}>
            {part.text}
          </span>
        );
      })}
      {'\u200b'}
    </div>
  );
};
