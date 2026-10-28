// The building blocks of the screens, in the look of the mockup. They hold no data of their own.
import { type ReactNode, useId, useState } from 'react';

import { Button, cx, Spinner } from '@kit';

import { useCopy } from '../lib/hooks';
import type { ApiError } from '../lib/api';
import { errorText } from '../lib/errors';
import { t } from '../lib/i18n';
import { Check, CircleCheck, CircleQuestionMark, Copy, ExternalLink, Info, TriangleAlert } from './icons';

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

export const LinkButton = ({
  children,
  onClick,
  disabled,
  danger,
  icon,
}: {
  children: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
  icon?: ReactNode;
}) => (
  <button
    type="button"
    className="hub-link"
    onClick={onClick}
    disabled={disabled}
    data-danger={danger || undefined}
  >
    {icon}
    {children}
  </button>
);

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

export const StepLine = ({ n, children }: { n: number; children: ReactNode }) => (
  <div className="hub-stepline">
    <b>{n}</b>
    <span>{children}</span>
  </div>
);

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

/** A command with an «open» button instead of «copy». */
export const LinkBox = ({ url, onOpen, label }: { url: string; onOpen: () => void; label: string }) => (
  <div className="hub-cmd" data-wrap>
    <code>{url}</code>
    <Button
      appearance="ghost"
      size="xs"
      className="k-btn--icon"
      aria-label={label}
      title={label}
      onClick={onOpen}
    >
      <ExternalLink size={16} />
    </Button>
  </div>
);

export const Waiting = ({ children }: { children: ReactNode }) => (
  <span className="hub-wait" role="status">
    <span className="hub-pulse" />
    {children}
  </span>
);

export type CheckState = 'wait' | 'run' | 'ok' | 'fail';

export interface CheckRow {
  id: string;
  label: string;
  state: CheckState;
  detail?: string | null;
}

const CHECK_ICON: Record<CheckState, ReactNode> = {
  wait: <CircleCheck size={16} />,
  run: <Spinner size="s" />,
  ok: <CircleCheck size={16} />,
  fail: <TriangleAlert size={16} />,
};

export const Checks = ({ rows }: { rows: CheckRow[] }) => (
  <ul className="hub-checks">
    {rows.map((r) => (
      <li key={r.id} className="hub-check" data-st={r.state}>
        {CHECK_ICON[r.state]}
        <span>
          {r.label}
          {r.detail && <span className="hub-check__d">{r.detail}</span>}
        </span>
      </li>
    ))}
  </ul>
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

export interface Tip {
  title: string;
  lines: string[];
}

/** The «?» of an option and the hint it opens above the row (on hover, focus or a tap). */
const TipButton = ({ tip, open, onToggle }: { tip: Tip; open: boolean; onToggle: () => void }) => {
  const id = useId();
  return (
    <>
      <button
        type="button"
        className="hub-help"
        aria-label={t('common.whenToChoose')}
        aria-expanded={open}
        aria-describedby={id}
        onClick={onToggle}
      >
        <CircleQuestionMark size={16} />
      </button>
      <span className="hub-pop" role="tooltip" id={id}>
        <b>{tip.title}</b>
        {tip.lines.map((l) => (
          <span key={l} className="hub-pop__i">
            {l}
          </span>
        ))}
      </span>
    </>
  );
};

export interface WayProps {
  icon: ReactNode;
  title: string;
  sub: string;
  on: boolean;
  off?: boolean;
  tag?: string;
  tip?: Tip;
  hero?: boolean;
  onPick: () => void;
}

/** One way to connect a machine: the big card or a row of the list. */
export const Way = ({ icon, title, sub, on, off, tag, tip, hero, onPick }: WayProps) => {
  const [tipOpen, setTipOpen] = useState(false);
  return (
    <div className="hub-optw" data-open={tipOpen || undefined}>
      <button
        type="button"
        className={hero ? 'hub-hero' : 'hub-opt'}
        aria-pressed={on}
        aria-disabled={off || undefined}
        onClick={() => {
          if (!off) onPick();
        }}
      >
        <span className="hub-ic">{icon}</span>
        <span className="hub-t">
          {hero ? <b>{title}</b> : <span className="hub-t__n">{title}</span>}
          <span>{sub}</span>
        </span>
        {off ? <Pill tone="muted">{t('common.unavailable')}</Pill> : tag && <Pill>{tag}</Pill>}
      </button>
      {tip && <TipButton tip={tip} open={tipOpen} onToggle={() => setTipOpen((v) => !v)} />}
    </div>
  );
};

/** A choice with an icon, a name and a line under it (the isolation profiles). */
export const Choice = ({
  icon,
  title,
  sub,
  on,
  tag,
  onPick,
}: {
  icon: ReactNode;
  title: string;
  sub: string;
  on: boolean;
  tag?: string;
  onPick: () => void;
}) => (
  <button type="button" className={cx('hub-opt', 'hub-opt--plain')} aria-pressed={on} onClick={onPick}>
    <span className="hub-ic">{icon}</span>
    <span className="hub-t">
      <span className="hub-t__n">{title}</span>
      <span>{sub}</span>
    </span>
    {tag && <Pill>{tag}</Pill>}
  </button>
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

/** What a list shows until its first answer: skeleton rows, or the failure with a retry when it did not come. */
export const Pending = ({
  error,
  onRetry,
  rows,
}: {
  error: ApiError | null;
  onRetry: () => void;
  rows: number;
}) => {
  if (!error) return <ListSkeleton rows={rows} />;
  return (
    <Msg
      tone="danger"
      action={
        <button type="button" className="hub-link" onClick={onRetry}>
          {t('common.retry')}
        </button>
      }
    >
      {errorText(error)}
    </Msg>
  );
};
