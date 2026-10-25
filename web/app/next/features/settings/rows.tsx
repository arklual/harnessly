// The parts the Settings pages are made of, after the design's .hx-set: a group with a title and a card of rows, a row
// with its label, hint and control (a switch, side-by-side choices, a menu, a link), and the buttons. A row that waits
// for the machine turns a ring and keeps the old value until the answer comes.
import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { IChoice } from '@/next/features/settings/options';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import Switch from '@/next/ui/switch';

/** A menu's exit, as long as hx-set-menu-out in settings.css. */
const MENU_EXIT_MS = 150;

interface IGroupProps {
  title?: string;
  foot?: ReactNode;
  /** The card's own class (rules, environment). */
  card?: string;
  children: ReactNode;
}

/** A titled card of rows with an optional note under it. */
export const Group = ({ title, foot, card, children }: IGroupProps) => (
  <div className="hx-set__group">
    {title && <div className="hx-set__group-title">{title}</div>}
    <div className={card ? `hx-set__card ${card}` : 'hx-set__card'}>{children}</div>
    {foot && <div className="hx-set__foot">{foot}</div>}
  </div>
);

export const Spin = () => (
  <span className="hx-set__spin" aria-hidden="true">
    <Icon name="loader-circle" size="sm" />
  </span>
);

interface IRowProps {
  type: 'switch' | 'seg' | 'menu' | 'link';
  label: string;
  hint?: string | null;
  busy?: boolean;
  /** The setting cannot be changed from here: the row is shown dimmed, its hint says why. */
  off?: boolean;
  children: ReactNode;
}

export const Row = ({ type, label, hint, busy = false, off = false, children }: IRowProps) => (
  <div
    className="hx-set__row"
    data-type={type}
    data-busy={busy || undefined}
    data-off={off || undefined}
    aria-busy={busy || undefined}
  >
    <div className="hx-set__row-text">
      <span className="hx-set__row-label">{label}</span>
      {hint && <span className="hx-set__row-hint">{hint}</span>}
    </div>
    <Spin />
    {children}
  </div>
);

interface IChoiceProps<V extends string | number | null> {
  label: string;
  value: V;
  /** `icon`: a glyph before the label (side-by-side choices only). */
  choices: readonly { value: V; label: string; sub?: string | null; icon?: string }[];
  onPick: (value: V) => void;
  disabled?: boolean;
}

export const SegControl = <V extends string | number>({
  label,
  value,
  choices,
  onPick,
  disabled = false,
}: IChoiceProps<V>) => (
  <div className="hx-set__seg" role="radiogroup" aria-label={label} aria-disabled={disabled || undefined}>
    {choices.map((choice) => (
      <button
        key={choice.value}
        type="button"
        className="hx-set__seg-item"
        role="radio"
        aria-checked={choice.value === value}
        disabled={disabled}
        onClick={() => choice.value !== value && onPick(choice.value)}
      >
        {choice.icon && <Icon name={choice.icon} size="sm" />}
        {choice.label}
      </button>
    ))}
  </div>
);

/** One of a list, opened under its button (a sheet from the bottom edge on a phone). */
export const MenuControl = ({
  label,
  value,
  choices,
  onPick,
  disabled = false,
}: IChoiceProps<string | number | null>) => {
  const [open, setOpen] = useState(false);
  const menu = usePresentFlag(open, MENU_EXIT_MS);
  const root = useRef<HTMLDivElement>(null);
  const current = choices.find((choice) => choice.value === value);

  useEffect(() => {
    if (!open) return;
    const away = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', away);
    document.addEventListener('keydown', escape);
    return () => {
      document.removeEventListener('pointerdown', away);
      document.removeEventListener('keydown', escape);
    };
  }, [open]);

  return (
    <div className="hx-set__pick" ref={root}>
      <button
        type="button"
        className="hx-set__pick-btn"
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen((now) => !now)}
      >
        <span>{current?.label ?? t('next.settings.unchosen')}</span>
        <Icon name="chevron-down" size="sm" />
      </button>
      {menu.mounted && (
        <div className="hx-set-menu" data-state={menu.state} role="listbox" aria-label={label}>
          {choices.map((choice) => (
            <button
              key={String(choice.value)}
              type="button"
              className="hx-set-menu__item"
              role="option"
              aria-selected={choice.value === value}
              onClick={() => {
                setOpen(false);
                if (choice.value !== value) onPick(choice.value);
              }}
            >
              <span className="hx-set-menu__text">
                <span>{choice.label}</span>
                {choice.sub && <span className="hx-set-menu__sub">{choice.sub}</span>}
              </span>
              <Icon name="check" size="sm" />
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

interface ISwitchRowProps {
  label: string;
  hint?: string | null;
  on: boolean;
  busy?: boolean;
  onChange: (on: boolean) => void;
}

export const SwitchRow = ({ label, hint, on, busy, onChange }: ISwitchRowProps) => (
  <Row type="switch" label={label} hint={hint} busy={busy}>
    <Switch label={label} checked={on} onChange={onChange} />
  </Row>
);

interface ISegRowProps<V extends string | number> {
  label: string;
  hint?: string | null;
  value: V | null;
  choices: readonly { value: V; label: string; icon?: string }[];
  busy?: boolean;
  off?: boolean;
  onPick: (value: V) => void;
}

export const SegRow = <V extends string | number>({
  label,
  hint,
  value,
  choices,
  busy,
  off,
  onPick,
}: ISegRowProps<V>) => (
  <Row type="seg" label={label} hint={hint} busy={busy} off={off}>
    <SegControl label={label} value={value as V} choices={choices} onPick={onPick} disabled={off} />
  </Row>
);

interface IMenuRowProps {
  label: string;
  hint?: string | null;
  value: string | number | null;
  choices: readonly IChoice[];
  busy?: boolean;
  off?: boolean;
  onPick: (value: string | number | null) => void;
}

export const MenuRow = ({ label, hint, value, choices, busy, off, onPick }: IMenuRowProps) => (
  <Row type="menu" label={label} hint={hint} busy={busy} off={off}>
    <MenuControl label={label} value={value} choices={choices} onPick={onPick} disabled={off} />
  </Row>
);

interface ILinkRowProps {
  label: string;
  hint?: string | null;
  action: string;
  onAction: () => void;
}

export const LinkRow = ({ label, hint, action, onAction }: ILinkRowProps) => (
  <Row type="link" label={label} hint={hint}>
    <button type="button" className="hx-set__link" onClick={onAction}>
      {action}
      <Icon name="chevron-right" size="sm" />
    </button>
  </Row>
);

interface IButtonProps {
  tone: 'primary' | 'ghost';
  busy?: boolean;
  disabled?: boolean;
  icon?: string;
  onClick: () => void;
  children: ReactNode;
}

/** The design's .hx-set__btn: a pill whose label stays while a ring turns beside it. */
export const SetButton = ({
  tone,
  busy = false,
  disabled = false,
  icon,
  onClick,
  children,
}: IButtonProps) => (
  <button
    type="button"
    className="hx-set__btn"
    data-tone={tone}
    data-busy={busy || undefined}
    aria-busy={busy || undefined}
    disabled={disabled}
    onClick={onClick}
  >
    {icon && <Icon name={icon} size="sm" />}
    <span className="hx-set__btn-label">{children}</span>
    <Icon name="loader-circle" size="sm" className="hx-set__btn-spin" />
  </button>
);

/** A sentence in place of a card: why the section has nothing to show or cannot change something. */
export const Note = ({ children }: { children: ReactNode }) => (
  <div className="hx-set__note" role="status">
    {children}
  </div>
);

/** A form error line, as the design's key form. */
export const FormError = ({ className, children }: { className?: string; children: ReactNode }) => (
  <div className={className ? `hx-key-form__err ${className}` : 'hx-key-form__err'} role="alert">
    <Icon name="triangle-alert" size="sm" />
    {children}
  </div>
);
