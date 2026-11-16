// The chat's limits right in the field, by the context ring: thin bars filled by what is left — the plan's
// windows it has now (the five hours when the plan has them, the week) and, when the chat's model has a limit of its
// own («Fable»), that one too; on hover the words, «Лимиты Claude: неделя 42% · Fable · неделя 80%». The
// popover of the model pill has the whole card.
import { memo, useLayoutEffect, useRef } from 'react';
import { slotsOf, type ISlot } from '@/next/features/limits/limits-brief';
import { useNow } from '@/next/features/limits/limits-chip';
import { WINDOW, profileName, type LimitsView } from '@/next/features/limits/model';
import { t } from '@/next/i18n';
import '@/next/styles/limits.css';

/** A model's own limit is the chat's when the model's id names it («claude-fable-5-2» and «Fable»). */
const ofModel = (slot: ISlot, model: string | null): boolean =>
  slot.model !== null && model !== null && model.toLowerCase().includes(slot.model.toLowerCase());

interface ILimitsMiniProps {
  view: LimitsView;
  /** The chat's model id; null when unknown. */
  model?: string | null;
}

const LimitsMini = memo(({ view, model = null }: ILimitsMiniProps) => {
  const now = useNow();
  const ref = useRef<HTMLSpanElement>(null);
  const slots =
    view.state === 'ready'
      ? slotsOf(view.limits, now / 1000).filter(
          (s) => s.kind !== WINDOW.month && (s.model === null || ofModel(s, model)),
        )
      : [];
  const fills = slots.map((s) => s.left).join(',');

  useLayoutEffect(() => {
    const bars = ref.current?.querySelectorAll<HTMLElement>('i');
    fills.split(',').forEach((f, i) => bars?.[i]?.style.setProperty('--fill', `${f}%`));
  }, [fills]);

  if (slots.length === 0) return null;
  const provider = view.profile ? profileName(view.profile) : t(`next.agent.${view.provider}`);
  const words = slots.map((s) => `${s.label} ${s.left}%`).join(' · ');
  const tip = t('next.limits.mini.tip', { provider, windows: words });
  return (
    <span ref={ref} className="hx-limmini hx-tipped" role="img" aria-label={tip}>
      <span className="hx-tip">{tip}</span>
      {slots.map((s) => (
        <i key={s.key} data-level={s.level} />
      ))}
    </span>
  );
});

export default LimitsMini;
