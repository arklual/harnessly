// The building blocks of the screens, in the look of the mockup. They hold no data of their own.
import { type ReactNode } from 'react';

import { Button } from '@kit';

import { useCopy } from '../lib/hooks';
import { t } from '../lib/i18n';
import { Check, CircleCheck, Copy, Info, TriangleAlert } from './icons';

export const Label = ({ children, required }: { children: ReactNode; required?: boolean }) => (
  <span className="hub-label">
    {children}
    {required && (
      <span className="hub-req" aria-hidden="true">
        *
      </span>
    )}
  </span>
);

export const Note = ({ children }: { children: ReactNode }) => <p className="hub-note">{children}</p>;

export const Or = ({ children }: { children: ReactNode }) => <div className="hub-or">{children}</div>;

export type MsgTone = 'danger' | 'warn' | 'success' | 'info';

const MSG_ICON: Record<MsgTone, ReactNode> = {
  danger: <TriangleAlert size={16} />,
  warn: <TriangleAlert size={16} />,
  success: <CircleCheck size={16} />,
  info: <Info size={16} />,
};

/** A line of feedback: what went wrong (or right) and, when there is one, what to do. */
export const Msg = ({
  tone,
  children,
  action,
}: {
  tone: MsgTone;
  children: ReactNode;
  action?: ReactNode;
}) => (
  <div className="hub-msg" data-tone={tone} role={tone === 'danger' ? 'alert' : 'status'}>
    {MSG_ICON[tone]}
    <div className="hub-msg__body">
      <span>{children}</span>
      {action}
    </div>
  </div>
);

export const Card = ({ children }: { children: ReactNode }) => <div className="hub-card">{children}</div>;

/** A command or a key in a mono box with a copy button that turns into a check for a moment. */
export const CopyBox = ({
  text,
  label,
  wrap,
  onCopied,
}: {
  text: string;
  label?: string;
  wrap?: boolean;
  onCopied?: () => void;
}) => {
  const { copied, copy } = useCopy();
  return (
    <div className="hub-cmd" data-wrap={wrap || undefined}>
      <code>{text}</code>
      <Button
        appearance="ghost"
        size="xs"
        className="k-btn--icon"
        aria-label={label ?? t('common.copy')}
        title={label ?? t('common.copy')}
        onClick={() => {
          copy(text);
          onCopied?.();
        }}
      >
        {copied ? <Check size={16} className="hub-copied" /> : <Copy size={16} />}
      </Button>
    </div>
  );
};

/** A command with no copy button, e.g. the one the person types in a console. */
export const CodeLine = ({ text }: { text: string }) => (
  <div className="hub-cmd" data-wrap>
    <code>{text}</code>
  </div>
);

export const Waiting = ({ children }: { children: ReactNode }) => (
  <span className="hub-wait" role="status">
    <span className="hub-pulse" />
    {children}
  </span>
);

/** A row of mutually exclusive choices drawn as one pill (not a native select). */
export const Segmented = <V extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: V;
  options: { value: V; label: ReactNode; disabled?: boolean }[];
  onChange: (v: V) => void;
  label: string;
}) => (
  <div className="hub-seg2" role="group" aria-label={label}>
    {options.map((o) => (
      <button
        key={o.value}
        type="button"
        aria-pressed={value === o.value}
        disabled={o.disabled}
        onClick={() => onChange(o.value)}
      >
        {o.label}
      </button>
    ))}
  </div>
);

export const Pill = ({ children, tone }: { children: ReactNode; tone?: 'muted' | 'warn' }) => (
  <span className="hub-pill" data-tone={tone}>
    {children}
  </span>
);

export const Sure = ({
  on,
  onToggle,
  children,
}: {
  on: boolean;
  onToggle: () => void;
  children: ReactNode;
}) => (
  <button type="button" className="hub-sure" aria-pressed={on} onClick={onToggle}>
    <span className="hub-sure__box">
      <Check size={14} />
    </span>
    <span>{children}</span>
  </button>
);

export const ListSkeleton = ({ rows = 2 }: { rows?: number }) => (
  <div className="hub-skel" aria-busy="true">
    {Array.from({ length: rows }, (_, i) => (
      <span key={i} className="k-skeleton" />
    ))}
  </div>
);
