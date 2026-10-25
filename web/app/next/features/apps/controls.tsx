// The controls of the Apps screen's toolbar: a search field, and menus that pick one value or several. A menu opens
// under its button on a computer and as a sheet from the bottom on a phone; a long list gets a field to narrow it.
import { useDeferredValue, useState, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import Segmented from '@/next/ui/segmented';
import Sheet from '@/next/ui/sheet';
import Switch from '@/next/ui/switch';
import { usePhone } from '@/next/ui/use-media';

/** A list with more options than this gets a field to narrow it. */
const NARROW_FROM = 8;

interface ISearchFieldProps {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}

export const SearchField = ({ value, onChange, placeholder }: ISearchFieldProps) => (
  <label className="hx-hubfield">
    <Icon name="search" size="sm" />
    <input
      className="hx-hubfield__input"
      type="search"
      value={value}
      placeholder={placeholder}
      aria-label={placeholder}
      autoComplete="off"
      spellCheck={false}
      onChange={(event) => onChange(event.currentTarget.value)}
    />
    {value !== '' && (
      <button
        type="button"
        className="hx-hubfield__x"
        aria-label={t('next.apps.search.clear')}
        onClick={() => onChange('')}
      >
        <Icon name="x" size="sm" />
      </button>
    )}
  </label>
);

export interface IChoice<V extends string> {
  value: V;
  label: string;
  /** A second line under the label. */
  sub?: string | null;
}

interface IMenuBase<V extends string> {
  /** What the menu is about: the button's name when nothing is chosen, and the menu's accessible name. */
  label: string;
  icon: string;
  choices: readonly IChoice<V>[];
}

interface IPickProps<V extends string> extends IMenuBase<V> {
  value: V;
  onChange: (value: V) => void;
}

interface IPickManyProps<V extends string> extends IMenuBase<V> {
  value: readonly V[];
  onChange: (value: V[]) => void;
}

interface IPanelProps<V extends string> {
  label: string;
  choices: readonly IChoice<V>[];
  isOn: (value: V) => boolean;
  onPick: (value: V) => void;
  /** Several can be on: the panel offers to clear them. */
  onClear?: () => void;
  role: 'menuitemradio' | 'menuitemcheckbox';
}

const Panel = <V extends string>({ label, choices, isOn, onPick, onClear, role }: IPanelProps<V>) => {
  const [text, setText] = useState('');
  const query = useDeferredValue(text).trim().toLowerCase();
  const shown =
    query === '' ? choices : choices.filter((choice) => choice.label.toLowerCase().includes(query));
  return (
    <div className="hx-hubmenu__in">
      {choices.length >= NARROW_FROM && (
        <label className="hx-hubmenu__find">
          <Icon name="search" size="sm" />
          <input
            type="search"
            value={text}
            placeholder={t('next.apps.menu.find')}
            aria-label={t('next.apps.menu.find')}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => setText(event.currentTarget.value)}
          />
        </label>
      )}
      <div className="hx-hubmenu__list" role="menu" aria-label={label}>
        {shown.map((choice) => (
          <button
            key={choice.value}
            type="button"
            className="hx-hubmenu__item"
            role={role}
            aria-checked={isOn(choice.value)}
            onClick={() => onPick(choice.value)}
          >
            <span className="hx-hubmenu__tick" aria-hidden="true">
              <Icon name="check" size="sm" />
            </span>
            <span className="hx-hubmenu__text">
              <span className="hx-hubmenu__label">{choice.label}</span>
              {choice.sub && <span className="hx-hubmenu__sub">{choice.sub}</span>}
            </span>
          </button>
        ))}
        {shown.length === 0 && <p className="hx-hubmenu__none">{t('next.apps.menu.none')}</p>}
      </div>
      {onClear && (
        <button type="button" className="hx-hubmenu__clear" onClick={onClear}>
          {t('next.apps.menu.clear')}
        </button>
      )}
    </div>
  );
};

interface IMenuProps {
  label: string;
  icon: string;
  /** What the button says: the label, or what is chosen. */
  text: string;
  active: boolean;
  children: (close: () => void) => ReactNode;
  align?: 'start' | 'end';
}

/** A button that opens its panel: a popover under it, or a sheet on a phone. */
const Menu = ({ label, icon, text, active, children, align = 'start' }: IMenuProps) => {
  const phone = usePhone();
  const [open, setOpen] = useState(false);
  const trigger = (
    <button
      type="button"
      className="hx-hubchip-btn"
      data-active={active || undefined}
      aria-haspopup="menu"
      aria-expanded={open}
      onClick={phone ? () => setOpen(true) : undefined}
    >
      <Icon name={icon} size="sm" />
      <span className="hx-hubchip-btn__text">{text}</span>
      <Icon name="chevron-down" size="sm" className="hx-hubchip-btn__chev" />
    </button>
  );
  const close = () => setOpen(false);
  if (phone)
    return (
      <>
        {trigger}
        <Sheet open={open} onClose={close} label={label} className="hx-hubmenu-sheet">
          <p className="hx-hubmenu__title">{label}</p>
          {children(close)}
        </Sheet>
      </>
    );
  return (
    <Popover
      trigger={trigger}
      open={open}
      onOpenChange={setOpen}
      label={label}
      className="hx-hubmenu"
      side="bottom"
      align={align}
    >
      {children(close)}
    </Popover>
  );
};

/** One of a few, chosen from a menu. */
export const PickMenu = <V extends string>({ label, icon, choices, value, onChange }: IPickProps<V>) => {
  const current = choices.find((choice) => choice.value === value);
  return (
    <Menu label={label} icon={icon} text={current?.label ?? label} active={false}>
      {(close) => (
        <Panel
          label={label}
          choices={choices}
          role="menuitemradio"
          isOn={(item) => item === value}
          onPick={(item) => {
            onChange(item);
            close();
          }}
        />
      )}
    </Menu>
  );
};

/** Any of the choices, ticked in a menu that stays open; nothing ticked is no narrowing. */
export const FilterMenu = <V extends string>({
  label,
  icon,
  choices,
  value,
  onChange,
}: IPickManyProps<V>) => {
  const first = choices.find((choice) => choice.value === value[0]);
  let text = label;
  if (first) text = value.length > 1 ? `${first.label} +${value.length - 1}` : first.label;
  return (
    <Menu label={label} icon={icon} text={text} active={value.length > 0}>
      {() => (
        <Panel
          label={label}
          choices={choices}
          role="menuitemcheckbox"
          isOn={(item) => value.includes(item)}
          onPick={(item) =>
            onChange(value.includes(item) ? value.filter((one) => one !== item) : [...value, item])
          }
          onClear={value.length > 0 ? () => onChange([]) : undefined}
        />
      )}
    </Menu>
  );
};

interface IViewMenuProps {
  label: string;
  /** The panel is the caller's: switches and choices of the app's own. */
  children: ReactNode;
}

/** The gear of a page: its own settings in a panel. */
export const SettingsMenu = ({ label, children }: IViewMenuProps) => {
  const phone = usePhone();
  const [open, setOpen] = useState(false);
  const trigger = (
    <button
      type="button"
      className="hx-head-btn"
      aria-label={label}
      aria-expanded={open}
      onClick={phone ? () => setOpen(true) : undefined}
    >
      <Icon name="sliders-horizontal" size="sm" />
    </button>
  );
  if (phone)
    return (
      <>
        {trigger}
        <Sheet open={open} onClose={() => setOpen(false)} label={label} className="hx-hubmenu-sheet">
          <p className="hx-hubmenu__title">{label}</p>
          <div className="hx-hubprefs">{children}</div>
        </Sheet>
      </>
    );
  return (
    <Popover
      trigger={trigger}
      open={open}
      onOpenChange={setOpen}
      label={label}
      className="hx-hubmenu hx-hubmenu--prefs"
      side="bottom"
      align="end"
      hint={label}
    >
      <div className="hx-hubprefs">{children}</div>
    </Popover>
  );
};

interface IPrefSwitchProps {
  label: string;
  hint: string;
  on: boolean;
  onChange: (on: boolean) => void;
}

/** A line of a page's settings: a switch with what it does. */
export const PrefSwitch = ({ label, hint, on, onChange }: IPrefSwitchProps) => (
  <div className="hx-hubprefs__row">
    <span className="hx-hubprefs__text">
      <span className="hx-hubprefs__label">{label}</span>
      <span className="hx-hubprefs__hint">{hint}</span>
    </span>
    <Switch label={label} checked={on} onChange={onChange} />
  </div>
);

interface IPrefChoiceProps<V extends string> {
  label: string;
  hint?: string;
  value: V;
  options: readonly { value: V; label: string }[];
  onChange: (value: V) => void;
}

/** A line of a page's settings: one of a few, side by side. */
export const PrefChoice = <V extends string>({
  label,
  hint,
  value,
  options,
  onChange,
}: IPrefChoiceProps<V>) => (
  <div className="hx-hubprefs__row" data-wrap>
    <span className="hx-hubprefs__text">
      <span className="hx-hubprefs__label">{label}</span>
      {hint && <span className="hx-hubprefs__hint">{hint}</span>}
    </span>
    <Segmented label={label} value={value} options={options} onChange={onChange} />
  </div>
);
