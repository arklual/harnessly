import type { TVoiceEngineKind } from '@/lib/voice-engine';
import type { TVoiceEngine } from '@/lib/voice-settings';

/**
 * Which dictation engine runs. «Auto» goes for the server engine: the machine's recognition service
 * answers partials in under a second on any device. In-tab Whisper (`local`) processes a 30-second window per
 * pass, which takes seconds without a fast GPU and hundreds of megabytes of memory a phone does not have, so
 * «Auto» only picks it on a computer where it has already proven fast, and otherwise keeps it as a fallback
 * for when the server engine cannot run. The browser's own recognizer goes to its vendor, often unreachable
 * from here, and comes last.
 */

export interface IVoiceEngineEnvironment {
  local: boolean;
  browser: boolean;
  server: boolean;
  mobile: boolean;
  /** In-tab Whisper already ran fast enough on this browser (see {@link recordLocalVoiceSpeed}). */
  localProven: boolean;
}

export const resolveVoiceEngine = (
  engine: TVoiceEngine,
  env: IVoiceEngineEnvironment,
  failed: ReadonlySet<TVoiceEngineKind> = new Set(),
): TVoiceEngineKind | null => {
  if (engine !== 'auto') return env[engine] ? engine : null;
  let order: TVoiceEngineKind[];
  if (env.mobile) order = ['server', 'browser'];
  else if (env.localProven) order = ['local', 'server', 'browser'];
  else order = ['server', 'local', 'browser'];
  return order.find((kind) => env[kind] && !failed.has(kind)) ?? null;
};

export const isMobileDevice = (): boolean => {
  if (typeof navigator === 'undefined') return false;
  const ua = navigator.userAgent;
  // iPadOS reports a desktop Safari user agent; touch points give it away.
  return (
    /iPhone|iPad|iPod|Android|Mobile/i.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)
  );
};

const LOCAL_SPEED_KEY = 'hl:voice-local-pass-ms';
/** A partial pass slower than this makes live dictation lag behind speech. */
export const LOCAL_FAST_PASS_MS = 1500;

/** Median time of one in-tab Whisper pass in the last dictation on this browser. */
export const recordLocalVoiceSpeed = (medianPassMs: number): void => {
  try {
    localStorage.setItem(LOCAL_SPEED_KEY, String(Math.round(medianPassMs)));
  } catch {
    // storage unavailable: «Auto» keeps preferring the server engine
  }
};

export const isLocalVoiceProven = (): boolean => {
  try {
    const value = Number(localStorage.getItem(LOCAL_SPEED_KEY));
    return value > 0 && value <= LOCAL_FAST_PASS_MS;
  } catch {
    return false;
  }
};
