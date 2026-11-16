// The context ring by the microphone (the design's .hx-ctx): how full the agent's window is, and on hover a card with
// the share, what fills it, where it compacts by itself and «Compact now». The page knows the window's total, not its
// parts: the card shows the conversation and what is free. The card stays while the pointer travels to it, so «Compact
// now» is in reach (ui/use-hover-intent.ts); a click pins it, a tap toggles it on touch.
import { useLayoutEffect, useRef } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import CtxCard from '@/next/features/composer/ctx-card';

/** The share of the window the agent compacts at when the runtime does not say (the card's mark). */
const AUTO_COMPACT = 0.95;
const WARN_PCT = 80;
const MID_PCT = 60;

export interface IContextRingProps {
  used: number;
  limit: number;
  /** Where the agent compacts by itself, in tokens, by its settings (Claude's autoCompactWindow, Codex's limit). */
  compactAt?: number;
  compacting: boolean;
  /** Compacting is offered while the agent waits; undefined while it works. */
  onCompact?: () => void;
}

const thousands = (n: number) => {
  const k = n / 1000;
  return k < 10 ? k.toFixed(1).replace('.', ',') : String(Math.round(k));
};

const toneOf = (pct: number) => {
  if (pct >= WARN_PCT) return 'warn';
  if (pct >= MID_PCT) return 'mid';
  return undefined;
};

const ContextRing = ({ used, limit, compactAt, compacting, onCompact }: IContextRingProps) => {
  const chipRef = useRef<HTMLSpanElement>(null);
  const barRef = useRef<HTMLDivElement>(null);
  const pct = Math.min(100, Math.round((used / limit) * 100));
  const free = Math.max(0, limit - used);
  const at = compactAt && compactAt > 0 ? compactAt : limit * AUTO_COMPACT;
  const atPct = Math.round((at / limit) * 100);
  const left = Math.max(0, Math.round((at - used) / 1000));

  useLayoutEffect(() => {
    chipRef.current?.style.setProperty('--p', String(pct));
    const parts = barRef.current?.querySelectorAll<HTMLElement>('i');
    parts?.[0]?.style.setProperty('--w', `${(used / limit) * 100}%`);
    parts?.[1]?.style.setProperty('--w', `${(free / limit) * 100}%`);
    barRef.current?.style.setProperty('--at', `${Math.min(100, (at / limit) * 100)}%`);
  }, [pct, used, free, limit, at]);

  let foot = { state: 'idle', icon: 'gauge', text: t('next.ctx.autoCompact', { left, at: atPct }) };
  if (compacting) foot = { state: 'live', icon: 'loader-circle', text: t('next.ctx.compacting') };

  const rows = [
    { k: 'msg', label: t('next.ctx.conversation'), v: used },
    { k: 'free', label: t('next.ctx.free'), v: free },
  ];

  return (
    <CtxCard
      label={t('next.ctx.aria', { pct })}
      title={t('next.ctx.title')}
      tone={toneOf(pct)}
      chipRef={(el) => {
        chipRef.current = el;
      }}
      chip={<span className="hx-ctx__ring" />}
    >
      <div className="hx-ctx__head">
        <span className="hx-ctx__title">{t('next.ctx.title')}</span>
        <span className="hx-ctx__pct">{pct}%</span>
        <span className="hx-spacer" />
        <button
          type="button"
          className="hx-ctx__compact hx-tipped"
          aria-disabled={!onCompact || compacting}
          aria-label={t('next.ctx.compact')}
          onClick={() => onCompact && !compacting && onCompact()}
        >
          <Icon name="fold-vertical" size="sm" />
          <span className="hx-tip" data-align="end">
            {onCompact ? t('next.ctx.compactNow') : t('next.ctx.compactBusy')}
            <kbd>/compact</kbd>
          </span>
        </button>
      </div>
      <div className="hx-ctx__used">
        {t('next.ctx.used', { used: thousands(used), limit: thousands(limit) })}
      </div>
      <div className="hx-ctx__bar" ref={barRef}>
        <i data-k="msg" />
        <i data-k="free" />
        <span className="hx-ctx__limit" title={t('next.ctx.limitMark')} />
      </div>
      <div className="hx-ctx__rows">
        {rows.map((row, j) => (
          <div key={row.k} className="hx-ctx__row" data-j={j}>
            <i className="hx-ctx__dot" data-k={row.k} />
            <span className="hx-ctx__label">{row.label}</span>
            <span className="hx-ctx__val">{t('next.ctx.thousands', { n: thousands(row.v) })}</span>
          </div>
        ))}
      </div>
      <div className="hx-ctx__foot" data-state={foot.state}>
        <Icon name={foot.icon} size="sm" />
        <span>{foot.text}</span>
      </div>
    </CtxCard>
  );
};

export default ContextRing;
