// The pieces of the quick voice settings, in the manner of the limits' and the context's cards (composer.css .hx-ctx):
// a row is its name on the left and its value on the right; a choice opens as a list in the same card with «‹ Back» and
// a tick at the current one; the quiet line at the foot leads on.
import { useEffect, useRef, type KeyboardEvent, type ReactNode, type RefObject } from 'react';
import { t } from '@/next/i18n';
import { goTo, ROUTE } from '@/next/routes/routes';
import Icon from '@/next/ui/icon';
import Switch from '@/next/ui/switch';

export interface IOption {
  value: string;
  label: string;
  /** A quiet line under the name. */
  sub?: string | null;
  /** Shown, not to be picked; `sub` says why. */
  disabled?: boolean;
}

interface IItemProps {
  /** What the list that this row opens is called; the focus comes back to the row when the list closes. */
  id: string;
  label: string;
  value: string;
  onOpen: () => void;
}

/** «Язык · Как интерфейс ›» */
export const QuickItem = ({ id, label, value, onOpen }: IItemProps) => (
  <button type="button" className="hx-vq__item" data-row={id} onClick={onOpen}>
    <span className="hx-vq__name">{label}</span>
    <span className="hx-vq__value">{value}</span>
    <Icon name="chevron-right" size="sm" />
  </button>
);

interface ISwitchProps {
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (on: boolean) => void;
}

/** A line with a small switch at its right. */
export const QuickSwitch = ({ label, checked, disabled, onChange }: ISwitchProps) => (
  <div className="hx-vq__item" data-kind="switch">
    <span className="hx-vq__name">{label}</span>
    <span className="hx-vq__tumbler">
      <Switch label={label} checked={checked} disabled={disabled} onChange={onChange} />
    </span>
  </div>
);

/** The quiet line at the foot: «⚙ Все настройки голоса ›». */
export const QuickFoot = ({
  section,
  onNavigate,
}: {
  section: 'voice' | 'accounts';
  onNavigate: () => void;
}) => (
  <button
    type="button"
    className="hx-ctx__foot hx-vq__foot"
    onClick={() => {
      onNavigate();
      goTo({ kind: ROUTE.settings, section });
    }}
  >
    <Icon name="settings" size="sm" />
    <span>{t('next.voice.quick.all')}</span>
    <Icon name="chevron-right" size="sm" />
  </button>
);

export const QuickRows = ({ children }: { children: ReactNode }) => (
  <div className="hx-vq__rows">{children}</div>
);

interface IListProps {
  title: string;
  options: readonly IOption[];
  value: string;
  onPick: (value: string) => void;
  onBack: () => void;
}

/** A choice in place of the settings: «‹ Назад», the name of the choice, the options (scrolling inside) with a tick. */
export const QuickList = ({ title, options, value, onPick, onBack }: IListProps) => {
  const box = useRef<HTMLDivElement>(null);
  // The current one first (the first one when none is), so Enter keeps or changes it at once.
  useEffect(() => {
    const current = box.current?.querySelector<HTMLElement>('[aria-checked="true"]');
    (current ?? box.current?.querySelector<HTMLElement>('button:not(:disabled)'))?.focus({
      preventScroll: true,
    });
    current?.scrollIntoView?.({ block: 'nearest' });
  }, []);
  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const all = [...(box.current?.querySelectorAll<HTMLElement>('button:not(:disabled)') ?? [])];
    const at = all.indexOf(document.activeElement as HTMLElement);
    all[(at + step + all.length) % all.length]?.focus();
  };
  return (
    <>
      <div className="hx-ctx__head">
        <button type="button" className="hx-vq__back" onClick={onBack}>
          <Icon name="chevron-left" size="sm" />
          {t('next.voice.quick.back')}
        </button>
        <span className="hx-spacer" />
        <span className="hx-ctx__title">{title}</span>
      </div>
      <div ref={box} className="hx-vq__list" role="radiogroup" aria-label={title} onKeyDown={onKey}>
        {options.map((o) => (
          <button
            key={o.value}
            type="button"
            role="radio"
            className="hx-vq__option"
            aria-checked={o.value === value}
            disabled={o.disabled}
            onClick={() => {
              onPick(o.value);
              onBack();
            }}
          >
            <span className="hx-vq__text">
              <span className="hx-vq__name">{o.label}</span>
              {o.sub && <span className="hx-vq__sub">{o.sub}</span>}
            </span>
            {o.value === value && <Icon name="check" size="sm" />}
          </button>
        ))}
      </div>
    </>
  );
};

/** After a list closes the focus goes back to the row that opened it. */
export const useReturnFocus = (sub: string | null, root: RefObject<HTMLElement | null>) => {
  const was = useRef<string | null>(null);
  useEffect(() => {
    const opener = was.current;
    was.current = sub;
    if (sub === null && opener !== null) {
      root.current?.querySelector<HTMLElement>(`[data-row="${opener}"]`)?.focus({ preventScroll: true });
    }
  }, [sub, root]);
};
