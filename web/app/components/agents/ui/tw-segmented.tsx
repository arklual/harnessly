import { useRef, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react';
import { cx, focusElement } from './dom';
import { TwCheckIcon, TwSpinner } from './icons';
import { firstNavigable, NAV_ORIENTATION, nextIndex, type TValueOf } from './roving-focus';

export interface ITwChoice<V extends string = string> {
  value: V;
  label: string;
  icon?: ReactNode;
  /** Second line in TwRadioList rows. */
  description?: string;
  /** Right-hand slot in TwRadioList rows. */
  meta?: ReactNode;
  disabled?: boolean;
}

export const SEGMENTED_MODE = {
  /** One choice out of several: role="radiogroup", arrows move and pick. */
  radio: 'radio',
  /** Independent aria-pressed buttons sharing one frame. */
  pressed: 'pressed',
} as const;

export const CHOICE_ACTIVATION = {
  /** Arrow keys pick immediately (APG radio group). */
  auto: 'auto',
  /** Arrow keys only move focus; Space/Enter pick. For choices that trigger a save. */
  manual: 'manual',
} as const;

interface IChoiceGroupOptions<V extends string> {
  choices: readonly ITwChoice<V>[];
  value: V | null;
  onChange: (value: V) => void;
  orientation: TValueOf<typeof NAV_ORIENTATION>;
  activation: TValueOf<typeof CHOICE_ACTIVATION>;
}

/** Roving tabindex shared by TwSegmented and TwRadioList. */
const useChoiceGroup = <V extends string>({
  choices,
  value,
  onChange,
  orientation,
  activation,
}: IChoiceGroupOptions<V>) => {
  const refs = useRef(new Map<V, HTMLButtonElement>());
  const isSkipped = (index: number) => Boolean(choices[index].disabled);
  const selectedIndex = choices.findIndex((choice) => choice.value === value && !choice.disabled);
  const tabStop = selectedIndex === -1 ? firstNavigable(choices.length, isSkipped) : selectedIndex;

  const onKeyDown = (event: ReactKeyboardEvent<HTMLElement>) => {
    const current = choices.findIndex((choice) => refs.current.get(choice.value) === document.activeElement);
    const index = nextIndex(current, event.key, {
      count: choices.length,
      orientation,
      loop: true,
      isSkipped,
    });
    if (index === null || index === -1) return;
    event.preventDefault();
    const choice = choices[index];
    focusElement(refs.current.get(choice.value));
    if (activation === CHOICE_ACTIVATION.auto && choice.value !== value) onChange(choice.value);
  };

  const register = (choiceValue: V) => (element: HTMLButtonElement | null) => {
    if (element) refs.current.set(choiceValue, element);
    else refs.current.delete(choiceValue);
  };

  return { tabStop, onKeyDown, register } as const;
};

export interface ITwSegmentedProps<V extends string> {
  options: readonly ITwChoice<V>[];
  value: V | null;
  onChange: (value: V) => void;
  'aria-label': string;
  mode?: TValueOf<typeof SEGMENTED_MODE>;
  activation?: TValueOf<typeof CHOICE_ACTIVATION>;
  /** Option being saved (pessimistic): spinner on it, the whole control locked. */
  pendingValue?: V | null;
  disabled?: boolean;
  /**
   * Labels collapse to icons (still announced) when the nearest CSS container is 480px or less.
   * The host sets container-type: inline-size.
   */
  collapseLabels?: boolean;
  className?: string;
}

/** Compact segmented control: a radiogroup, or a row of aria-pressed buttons. */
export const TwSegmented = <V extends string>({
  options,
  value,
  onChange,
  'aria-label': ariaLabel,
  mode = SEGMENTED_MODE.radio,
  activation = CHOICE_ACTIVATION.auto,
  pendingValue = null,
  disabled = false,
  collapseLabels = false,
  className,
}: ITwSegmentedProps<V>) => {
  const radio = mode === SEGMENTED_MODE.radio;
  const pending = pendingValue !== null;
  const group = useChoiceGroup({
    choices: options,
    value,
    onChange,
    orientation: NAV_ORIENTATION.horizontal,
    activation: radio ? activation : CHOICE_ACTIVATION.manual,
  });

  return (
    <div
      role={radio ? 'radiogroup' : 'group'}
      aria-label={ariaLabel}
      aria-busy={pending || undefined}
      aria-disabled={disabled || undefined}
      className={cx('tw-ui-segmented', className)}
      onKeyDown={pending || disabled ? undefined : group.onKeyDown}
    >
      {options.map((option, index) => {
        const selected = option.value === value;
        return (
          <button
            key={option.value}
            ref={group.register(option.value)}
            type="button"
            role={radio ? 'radio' : undefined}
            aria-checked={radio ? selected : undefined}
            aria-pressed={radio ? undefined : selected}
            tabIndex={index === group.tabStop ? 0 : -1}
            disabled={option.disabled || disabled}
            aria-disabled={pending || undefined}
            data-selected={selected ? '' : undefined}
            className="tw-ui-segment"
            onClick={() => {
              if (pending || (selected && radio)) return;
              onChange(option.value);
            }}
          >
            {option.value === pendingValue ? <TwSpinner /> : option.icon}
            <span
              className="tw-ui-segment-label"
              data-collapsible={collapseLabels && option.icon ? '' : undefined}
            >
              {option.label}
            </span>
          </button>
        );
      })}
    </div>
  );
};

export interface ITwRadioListProps<V extends string> {
  options: readonly ITwChoice<V>[];
  value: V | null;
  onChange: (value: V) => void;
  'aria-label'?: string;
  'aria-labelledby'?: string;
  activation?: TValueOf<typeof CHOICE_ACTIVATION>;
  pendingValue?: V | null;
  disabled?: boolean;
  className?: string;
}

/** A vertical radiogroup of rich rows (icon, label, description, check). */
export const TwRadioList = <V extends string>({
  options,
  value,
  onChange,
  'aria-label': ariaLabel,
  'aria-labelledby': ariaLabelledby,
  activation = CHOICE_ACTIVATION.manual,
  pendingValue = null,
  disabled = false,
  className,
}: ITwRadioListProps<V>) => {
  const pending = pendingValue !== null;
  const group = useChoiceGroup({
    choices: options,
    value,
    onChange,
    orientation: NAV_ORIENTATION.vertical,
    activation,
  });

  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      aria-labelledby={ariaLabelledby}
      aria-busy={pending || undefined}
      className={cx('tw-ui-radio-list', className)}
      onKeyDown={pending || disabled ? undefined : group.onKeyDown}
    >
      {options.map((option, index) => {
        const selected = option.value === value;
        return (
          <button
            key={option.value}
            ref={group.register(option.value)}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={index === group.tabStop ? 0 : -1}
            disabled={option.disabled || disabled}
            aria-disabled={pending || undefined}
            data-selected={selected ? '' : undefined}
            className="tw-ui-radio-row"
            onClick={() => {
              if (!pending && !selected) onChange(option.value);
            }}
          >
            {option.icon && (
              <span className="tw-ui-option-icon" aria-hidden="true">
                {option.icon}
              </span>
            )}
            <span className="tw-ui-option-text">
              <span className="tw-ui-option-label">{option.label}</span>
              {option.description && <span className="tw-ui-option-description">{option.description}</span>}
            </span>
            {option.meta && <span className="tw-ui-option-meta">{option.meta}</span>}
            <span className="tw-ui-option-check" aria-hidden="true">
              {option.value === pendingValue ? <TwSpinner /> : selected && <TwCheckIcon />}
            </span>
          </button>
        );
      })}
    </div>
  );
};
