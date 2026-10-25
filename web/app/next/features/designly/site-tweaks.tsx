// «Живые настройки» of an open design: a panel over the page with what the board offers to change — its theme, screen
// states, properties. The board's own script in the page names the settings and takes the changes (page-bridge); the
// panel only draws them, so a switch is seen on the page at once. It is moved by its head and keeps the place.
import { Fragment, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import type { ITweak, TTweakValue } from '@/next/features/designly/page-bridge';
import type { IPoint } from '@/next/features/designly/site-drag';
import {
  rangeShare,
  tweakGroups,
  tweakKind,
  tweakValueText,
  TWEAK_KIND,
} from '@/next/features/designly/tweaks';
import Icon from '@/next/ui/icon';
import { usePresence, type TPresenceState } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const MENU_MS = 200;

interface IControlProps {
  tweak: ITweak;
  onChange: (value: TTweakValue) => void;
}

const Segments = ({ tweak, onChange }: IControlProps) => (
  <div className="hx-tweak__opts">
    {tweak.options.map((o) => (
      <button
        key={String(o.value)}
        type="button"
        className="hx-tweak__opt"
        aria-pressed={o.value === tweak.value}
        onClick={() => o.value !== tweak.value && onChange(o.value)}
      >
        {o.text}
      </button>
    ))}
  </div>
);

const Swatch = ({
  color,
  on,
  label,
  onPick,
}: {
  color: string;
  on: boolean;
  label: string;
  onPick: () => void;
}) => {
  const ref = useRef<HTMLButtonElement>(null);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--c', color);
  }, [color]);
  return (
    <button
      ref={ref}
      type="button"
      className="hx-tweak__color"
      aria-pressed={on}
      aria-label={label}
      onClick={onPick}
    />
  );
};

const Colors = ({ tweak, onChange }: IControlProps) => (
  <div className="hx-tweak__colors">
    {tweak.options.map((o) => (
      <Swatch
        key={String(o.value)}
        color={String(o.value)}
        label={o.text}
        on={o.value === tweak.value}
        onPick={() => o.value !== tweak.value && onChange(o.value)}
      />
    ))}
  </div>
);

const Select = ({ tweak, onChange }: IControlProps) => {
  const [open, setOpen] = useState(false);
  const menu = usePresence(open ? true : null, MENU_MS);
  return (
    <div className="hx-tweak__sel">
      <button
        type="button"
        className="hx-tweak__selbtn"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((on) => !on)}
        onBlur={(event) => {
          if (!event.currentTarget.parentElement?.contains(event.relatedTarget)) setOpen(false);
        }}
      >
        <span className="hx-tweak__selval">{tweakValueText(tweak)}</span>
        <Icon name="chevron-down" size="sm" />
      </button>
      {menu.value !== null && (
        <div className="hx-site-menu hx-tweak__menu" data-state={menu.state} role="listbox">
          {tweak.options.map((o) => (
            <button
              key={String(o.value)}
              type="button"
              role="option"
              className="hx-option hx-devopt"
              aria-selected={o.value === tweak.value}
              onClick={() => {
                setOpen(false);
                if (o.value !== tweak.value) onChange(o.value);
              }}
            >
              <Icon name="check" size="sm" className="hx-devopt__check" />
              <span className="hx-devopt__name">{o.text}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

/** A slider: while it is dragged it shows where the hand is, the page's answer takes over afterwards. */
const Range = ({ tweak, onChange }: IControlProps) => {
  const ref = useRef<HTMLInputElement>(null);
  const [held, setHeld] = useState<number | null>(null);
  const min = tweak.min ?? 0;
  const max = tweak.max ?? 0;
  const value = held ?? (typeof tweak.value === 'number' ? tweak.value : min);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--p', rangeShare(value, min, max));
  }, [value, min, max]);
  return (
    <div className="hx-tweak__range">
      <input
        ref={ref}
        className="hx-tweak__slider"
        type="range"
        min={min}
        max={max}
        step={tweak.step ?? 1}
        value={value}
        aria-label={tweak.label}
        onChange={(event) => {
          const next = Number(event.currentTarget.value);
          setHeld(next);
          onChange(next);
        }}
        onPointerUp={() => setHeld(null)}
        onBlur={() => setHeld(null)}
      />
      <span className="hx-tweak__val">{value}</span>
    </div>
  );
};

/** A field: what is typed stays in it while the page takes it. */
const Field = ({ tweak, onChange, numeric }: IControlProps & { numeric: boolean }) => {
  const [typed, setTyped] = useState<string | null>(null);
  const shown = typed ?? (tweak.value === null ? '' : String(tweak.value));
  return (
    <input
      className="hx-tweak__text"
      type={numeric ? 'number' : 'text'}
      value={shown}
      step={numeric ? (tweak.step ?? 1) : undefined}
      aria-label={tweak.label}
      autoComplete="off"
      onChange={(event) => {
        const raw = event.currentTarget.value;
        setTyped(raw);
        if (!numeric) onChange(raw);
        else if (raw.trim() !== '' && Number.isFinite(Number(raw))) onChange(Number(raw));
      }}
      onBlur={() => setTyped(null)}
    />
  );
};

const Control = ({ tweak, onChange }: IControlProps) => {
  const kind = tweakKind(tweak);
  if (kind === TWEAK_KIND.seg) return <Segments tweak={tweak} onChange={onChange} />;
  if (kind === TWEAK_KIND.colors) return <Colors tweak={tweak} onChange={onChange} />;
  if (kind === TWEAK_KIND.select) return <Select tweak={tweak} onChange={onChange} />;
  if (kind === TWEAK_KIND.range) return <Range tweak={tweak} onChange={onChange} />;
  if (kind === TWEAK_KIND.toggle)
    return (
      <button
        type="button"
        className="hx-switch"
        role="switch"
        aria-checked={tweak.value === true}
        aria-label={tweak.label}
        onClick={() => onChange(tweak.value !== true)}
      >
        <i />
      </button>
    );
  return <Field tweak={tweak} onChange={onChange} numeric={kind === TWEAK_KIND.number} />;
};

export interface ISiteTweaksProps {
  controls: readonly ITweak[];
  /** `closing` while the panel leaves. */
  state: TPresenceState;
  /** Where the person moved it, in the browser's pixels; null while it stands in its own place. */
  pos: IPoint | null;
  onChange: (name: string, value: TTweakValue) => void;
  onClose: () => void;
  /** The head takes the pointer: the panel moves with it. */
  onGrab: (event: ReactPointerEvent<HTMLElement>) => void;
}

const SiteTweaks = ({ controls, state, pos, onChange, onClose, onGrab }: ISiteTweaksProps) => {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !pos) return;
    el.style.setProperty('--tx', `${pos.x}px`);
    el.style.setProperty('--ty', `${pos.y}px`);
  }, [pos]);
  const groups = tweakGroups(controls);
  return (
    <div
      ref={ref}
      className="hx-tweaks"
      data-state={state}
      data-moved={pos !== null}
      role="dialog"
      aria-label={t('next.site.tweaks.title')}
    >
      <div className="hx-tweaks__head" title={t('next.site.tweaks.move')} onPointerDown={onGrab}>
        <span className="hx-tweaks__title">{t('next.site.tweaks.title')}</span>
        <span className="hx-spacer" />
        <button
          type="button"
          className="hx-head-btn"
          aria-label={t('next.site.tweaks.close')}
          onClick={onClose}
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      <div className="hx-tweaks__body">
        {groups.length === 0 && <div className="hx-tweaks__empty">{t('next.site.tweaks.none')}</div>}
        {groups.map((group) => (
          <Fragment key={group.section}>
            {group.section !== '' && <div className="hx-tweaks__group">{group.section}</div>}
            {group.items.map((tweak) => (
              <div key={tweak.name} className="hx-tweak" data-kind={tweakKind(tweak)}>
                <span className="hx-tweak__label">{tweak.label}</span>
                <Control tweak={tweak} onChange={(value) => onChange(tweak.name, value)} />
              </div>
            ))}
          </Fragment>
        ))}
      </div>
    </div>
  );
};

export default SiteTweaks;
