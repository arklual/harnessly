// How a call delivers and thinks: the person switches both by voice, with no settings — «помолчи, скажи
// просто результат» / «рассказывай по ходу», «думай лучше» / «быстрее» — or with the effort steps beside the
// microphone. The choice holds until the call ends; the defaults come from the voice's settings.
import { createStore, useStore } from '@/next/core/store';

export const DELIVERY = { narrate: 'narrate', result: 'result' } as const;
export type TDelivery = (typeof DELIVERY)[keyof typeof DELIVERY];

/** The five steps of how hard the voice thinks, ChatGPT's Instant … Pro (internal/voice/realtime Efforts). */
export const EFFORT_STEPS = ['instant', 'low', 'medium', 'high', 'max'] as const;
export type TEffort = (typeof EFFORT_STEPS)[number];
export const EFFORT_DEFAULT: TEffort = 'instant';

export interface ICallStyle {
  delivery: TDelivery;
  effort: TEffort;
}

export const callStyle = createStore<ICallStyle>({ delivery: DELIVERY.narrate, effort: EFFORT_DEFAULT });

export const useCallStyle = (): ICallStyle => useStore(callStyle);

export const effortOf = (v: string | undefined): TEffort =>
  EFFORT_STEPS.find((x) => x === v) ?? EFFORT_DEFAULT;

export const deliveryOf = (v: string | undefined): TDelivery =>
  v === DELIVERY.result ? DELIVERY.result : DELIVERY.narrate;

/** A new call starts from the settings' defaults. */
export const resetCallStyle = (defaults: { delivery?: string; effort?: string }): void =>
  callStyle.set({ delivery: deliveryOf(defaults.delivery), effort: effortOf(defaults.effort) });

/** «Помолчи», «скажи просто результат», «без комментариев», «тише»: results only. */
const QUIET_RE =
  /(помолч|молчи\b|не комментир|без комментари|(только|просто)\s+(скажи\s+)?(результат|итог)|(скажи|говори)\s+(только|просто)\s+(результат|итог)|не рассказывай)/i;
/** «Рассказывай по ходу», «комментируй», «говори, что делаешь»: narrate again. */
const NARRATE_RE =
  /(рассказывай|говори|комментируй|озвучивай)[^.!?]{0,20}(по ходу|что делаешь|шаги)|^комментируй/i;
/** «Думай лучше», «подумай хорошенько», «умнее»: a step up. */
const DEEPER_RE =
  /(думай|подумай)\s+(лучше|получше|глубже|хорошенько|как следует)|\bумнее\b|включи\s+(умн|максимал)/i;
/** «Быстрее», «отвечай быстро», «мгновенно»: back to instant. */
const FASTER_RE = /(отвечай\s+)?быстре|(отвечай|давай)\s+быстро|мгновенн|не думай долго/i;

export interface IStyleChange {
  style: ICallStyle;
  /** What changed, for the voice to know: a quiet note. */
  note: string;
}

/** What the person's words switch, or null. Only the person's own turn is read, never the voice's. */
export const styleFromWords = (text: string, was: ICallStyle): IStyleChange | null => {
  const t = text.trim();
  if (!t) return null;
  if (QUIET_RE.test(t) && was.delivery !== DELIVERY.result) {
    return {
      style: { ...was, delivery: DELIVERY.result },
      note: 'The person asked for results only: from now on say nothing about the steps, only the final result.',
    };
  }
  if (NARRATE_RE.test(t) && was.delivery !== DELIVERY.narrate) {
    return {
      style: { ...was, delivery: DELIVERY.narrate },
      note: "The person asked to hear the work as it goes: tell the agent's steps in a few words again.",
    };
  }
  if (DEEPER_RE.test(t)) {
    const i = EFFORT_STEPS.indexOf(was.effort);
    const effort = EFFORT_STEPS[Math.min(EFFORT_STEPS.length - 1, Math.max(i + 1, 2))];
    if (effort === was.effort) return null;
    return { style: { ...was, effort }, note: effortNote(effort) };
  }
  if (FASTER_RE.test(t) && was.effort !== EFFORT_DEFAULT) {
    return { style: { ...was, effort: EFFORT_DEFAULT }, note: effortNote(EFFORT_DEFAULT) };
  }
  return null;
};

/** The quiet note a step change gives the voice in a live call (its instructions can not change once it is made). */
export const effortNote = (effort: TEffort): string =>
  effort === EFFORT_DEFAULT
    ? 'Speed is now instant: answer small general questions yourself at once; the rest goes to the agent.'
    : 'Speed is now thorough: answer yourself only small talk; every question that needs thought goes to the agent.';
