import { Fragment, useEffect, type ReactNode } from 'react';
import { CircleHelp, TriangleAlert, X } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import type { TTone } from '@/components/agents/efficiency/families-math';
import { t } from '@/lib/i18n';

export type TChipTone = TTone | 'accent';

export const CHIP_TONE: Record<TChipTone, string> = {
  ok: 'bg-(--t-accent-green-bg) text-(--t-status-success)',
  bad: 'bg-(--t-accent-rose-bg) text-(--t-status-danger)',
  warn: 'bg-(--t-accent-amber-bg) text-(--t-status-warning)',
  flat: 'bg-(--t-surface-input) text-(--t-text-secondary)',
  accent: 'bg-(--t-accent-purple-bg) text-(--t-accent-purple)',
};

export const TONE_TEXT: Record<TTone, string> = {
  ok: 'text-(--t-status-success)',
  bad: 'text-(--t-status-danger)',
  warn: 'text-(--t-status-warning)',
  flat: 'text-(--t-text-secondary)',
};

export const TONE_FILL: Record<TTone, string> = {
  ok: 'fill-(--t-status-success)',
  bad: 'fill-(--t-status-danger)',
  warn: 'fill-(--t-status-warning)',
  flat: 'fill-(--t-text-secondary)',
};

export const TONE_BG: Record<TTone, string> = {
  ok: 'bg-(--t-status-success)',
  bad: 'bg-(--t-status-danger)',
  warn: 'bg-(--t-status-warning)',
  flat: 'bg-(--t-text-muted)',
};

export const Chip = ({
  tone = 'flat',
  children,
  className,
}: {
  tone?: TChipTone;
  children: ReactNode;
  className?: string;
}) => (
  <span
    className={cx(
      'inline-flex h-5 items-center whitespace-nowrap rounded-full px-2 text-[11px] font-medium',
      CHIP_TONE[tone],
      className,
    )}
  >
    {children}
  </span>
);

/** A dictionary text whose `{name}` placeholders are filled with elements: a bold number, a link. */
export const Rich = ({ text, parts }: { text: string; parts: Record<string, ReactNode> }) => (
  <>
    {text
      .split(/\{(\w+)\}/g)
      .map((piece, i) => (i % 2 ? <Fragment key={i}>{parts[piece] ?? `{${piece}}`}</Fragment> : piece))}
  </>
);

export const Hint = ({ text }: { text: string }) => (
  <span className="ts-hint" tabIndex={0} aria-label={text} data-tip={text}>
    <CircleHelp aria-hidden="true" />
  </span>
);

interface ICardProps {
  title: string;
  sub?: ReactNode;
  hint?: string;
  actions?: ReactNode;
  tone?: 'plain' | 'alert';
  children: ReactNode;
}

/** A full-width card of the efficiency screens: same frame as a stats panel. */
export const Card = ({ title, sub, hint, actions, tone = 'plain', children }: ICardProps) => (
  <section className={cx('ts-panel mb-4', tone === 'alert' && 'border-(--t-status-danger)')}>
    <header className="ts-panel-head">
      <div>
        <h2 className="ts-panel-title">
          {title}
          {hint && <Hint text={hint} />}
        </h2>
        {sub && <p className="ts-panel-sub">{sub}</p>}
      </div>
      {actions}
    </header>
    <div className="ts-panel-body">{children}</div>
  </section>
);

interface ISegmentedProps<T extends string> {
  label: string;
  value: T;
  options: { value: T; label: string; dot?: boolean }[];
  onChange: (value: T) => void;
}

export const Segmented = <T extends string>({ label, value, options, onChange }: ISegmentedProps<T>) => (
  <div className="ts-segmented" role="radiogroup" aria-label={label}>
    {options.map((o) => (
      <button
        key={o.value}
        type="button"
        role="radio"
        aria-checked={o.value === value}
        className={cx(o.value === value && 'ts-seg-on', 'relative')}
        onClick={() => onChange(o.value)}
      >
        {o.label}
        {o.dot && (
          <span
            className="absolute right-1 top-1 size-2 rounded-full bg-(--t-status-danger)"
            role="img"
            aria-label={t('efficiency.primitives.alertForYou')}
          />
        )}
      </button>
    ))}
  </div>
);

export const Empty = ({ text, className }: { text: string; className?: string }) => (
  <div
    className={cx(
      'flex min-h-24 items-center justify-center text-center text-[13px] text-(--t-text-secondary)',
      className,
    )}
  >
    {text}
  </div>
);

export const Skeleton = ({ className }: { className?: string }) => (
  <div className={cx('ts-skeleton rounded-(--t-radius-xl)', className)} />
);

export const Notice = ({
  tone = 'accent',
  children,
}: {
  tone?: 'accent' | 'warn' | 'alert';
  children: ReactNode;
}) => (
  <div
    role="status"
    className={cx(
      'mb-4 flex items-start gap-3 rounded-xl px-3.5 py-2.5 text-[13px] text-(--t-text)',
      tone === 'accent' && 'bg-(--t-accent-purple-bg)',
      tone === 'warn' && 'bg-(--t-accent-amber-bg)',
      tone === 'alert' && 'border border-(--t-status-danger) bg-(--t-accent-rose-bg)',
    )}
  >
    {children}
  </div>
);

export const LoadFailed = ({ text, onRetry }: { text: string; onRetry: () => void }) => (
  <div className="ts-state">
    <TriangleAlert /> {text}
    <button type="button" className="ts-btn" onClick={onRetry}>
      {t('efficiency.primitives.retry')}
    </button>
  </div>
);

interface IKpiProps {
  label: string;
  value: string;
  sub?: string;
  hint?: string;
}

export const Kpi = ({ label, value, sub, hint }: IKpiProps) => (
  <div className="ts-tile">
    <span className="ts-tile-label">
      {label}
      {hint && <Hint text={hint} />}
    </span>
    <strong className="ts-tile-value">{value}</strong>
    <span className="ts-tile-foot">{sub && <span className="ts-tile-sub">{sub}</span>}</span>
  </div>
);

export const KpiRow = ({ children }: { children: ReactNode }) => (
  <div className="mb-4 grid grid-cols-2 gap-px overflow-hidden rounded-(--t-radius-xl) border border-(--t-border-subtle) bg-(--t-border-subtle) md:grid-cols-4">
    {children}
  </div>
);

export const KeyValues = ({ rows }: { rows: { label: string; value: string }[] }) => (
  <dl className="ts-kv">
    {rows.map((r) => (
      <div key={r.label}>
        <dt>{r.label}</dt>
        <dd>{r.value}</dd>
      </div>
    ))}
  </dl>
);

interface IPaneProps {
  chip?: ReactNode;
  title: string;
  onClose: () => void;
  children: ReactNode;
}

/** Details drawer: a column on the right edge on a wide screen, the whole screen on a phone. Esc closes it. */
export const Pane = ({ chip, title, onClose, children }: IPaneProps) => {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  return (
    <aside
      className="fixed inset-y-0 right-0 z-40 flex w-full max-w-[440px] flex-col overflow-y-auto border-l border-(--t-border-default) bg-(--t-surface) p-5 text-[13px] text-(--t-text) shadow-(--t-shadow-popover)"
      aria-label={t('efficiency.primitives.details')}
    >
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="min-w-0">
          {chip}
          <h3 className="mt-2 text-lg font-bold leading-tight">{title}</h3>
        </div>
        <button
          type="button"
          className="ts-icon-btn"
          aria-label={t('efficiency.primitives.close')}
          onClick={onClose}
        >
          <X />
        </button>
      </div>
      {children}
    </aside>
  );
};

export const PaneHeading = ({ children }: { children: ReactNode }) => (
  <h4 className="mb-2 mt-5 text-[12px] font-semibold uppercase tracking-wide text-(--t-text-secondary)">
    {children}
  </h4>
);

export const Note = ({ children }: { children: ReactNode }) => (
  <p className="mt-2 text-[12px] leading-relaxed text-(--t-text-secondary)">{children}</p>
);

export const RowButton = ({
  onClick,
  children,
  className,
}: {
  onClick: () => void;
  children: ReactNode;
  className?: string;
}) => (
  <button
    type="button"
    onClick={onClick}
    className={cx('w-full cursor-pointer text-left hover:bg-(--t-surface-hover)', className)}
  >
    {children}
  </button>
);

export const KeyValuesRow = ({ rows }: { rows: { label: string; value: string }[] }) => (
  <dl className="grid grid-cols-2 gap-2">
    {rows.map((r) => (
      <div key={r.label} className="rounded-xl border border-(--t-border-subtle) px-3 py-2">
        <dt className="text-[11px] text-(--t-text-secondary)">{r.label}</dt>
        <dd className="m-0 text-[15px] font-semibold tabular-nums">{r.value}</dd>
      </div>
    ))}
  </dl>
);
