import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from 'react';
import { cx, focusElement } from './dom';
import { TwCheckIcon, TwChevronDownIcon, TwSpinner } from './icons';
import { LAYER_ALIGN, LAYER_SIDE } from './placement';
import {
  EMPTY_TYPEAHEAD,
  firstNavigable,
  isTypeaheadKey,
  LISTBOX_ACTION,
  listboxKeyAction,
  typeaheadIndex,
  typeaheadStep,
} from './roving-focus';
import { LAYER_KIND, TwLayer } from './tw-layer';
import { t } from '@/lib/i18n';

export interface ITwOption<V extends string = string> {
  value: V;
  label: string;
  description?: string;
  icon?: ReactNode;
  /** Right-hand slot: a badge, a shortcut, a count. */
  meta?: ReactNode;
  disabled?: boolean;
}

const idSafe = (value: string) =>
  value.replace(/[^A-Za-z0-9_-]/g, (char) => `_${char.charCodeAt(0).toString(16)}`);

/** DOM id of an option row: the value a combobox puts into aria-activedescendant. */
export const twOptionDomId = (listId: string, value: string): string => `${listId}-option-${idSafe(value)}`;

const effectiveActive = <V extends string>(
  options: readonly ITwOption<V>[] | null,
  wanted: V | null,
): V | null => {
  if (!options?.length) return null;
  if (wanted !== null && options.some((option) => option.value === wanted && !option.disabled)) return wanted;
  const index = firstNavigable(options.length, (i) => Boolean(options[i].disabled));
  return index === -1 ? null : options[index].value;
};

export interface IUseTwListboxNavOptions<V extends string> {
  listId: string;
  options: readonly ITwOption<V>[] | null;
  loop?: boolean;
  /** The keys come from a text field: Home/End and Space stay with the caret. Default true. */
  textInput?: boolean;
}

export type TTwListboxNavResult<V extends string> =
  { type: typeof LISTBOX_ACTION.select; value: V } | { type: typeof LISTBOX_ACTION.dismiss };

interface IKeyEventLike {
  key: string;
  preventDefault: () => void;
  stopPropagation?: () => void;
  nativeEvent?: { isComposing?: boolean };
}

/**
 * Keyboard model for a text field that drives a TwListbox (the slash-command list):
 * focus stays in the field, rows are pointed at with aria-activedescendant.
 * The active row falls back to the first enabled one when the list changes under it.
 */
export const useTwListboxNav = <V extends string>({
  listId,
  options,
  loop = true,
  textInput = true,
}: IUseTwListboxNavOptions<V>) => {
  const [wanted, setActiveValue] = useState<V | null>(null);
  const activeValue = effectiveActive(options, wanted);

  const handleKeyDown = (event: IKeyEventLike): TTwListboxNavResult<V> | null => {
    if (event.nativeEvent?.isComposing || !options) return null;
    const active = activeValue === null ? -1 : options.findIndex((option) => option.value === activeValue);
    const action = listboxKeyAction(event.key, {
      count: options.length,
      active,
      loop,
      isSkipped: (index) => Boolean(options[index].disabled),
      spaceSelects: !textInput,
      textInput,
    });
    if (action.type === LISTBOX_ACTION.none) return null;
    event.preventDefault();
    if (action.type === LISTBOX_ACTION.move) {
      setActiveValue(options[action.index].value);
      return null;
    }
    if (action.type === LISTBOX_ACTION.select)
      return { type: LISTBOX_ACTION.select, value: options[action.index].value };
    // A consumed Escape must not reach window-level shortcuts (the permission prompt answers "n" on it).
    event.stopPropagation?.();
    return { type: LISTBOX_ACTION.dismiss };
  };

  return {
    activeValue,
    setActiveValue,
    activeDescendant: activeValue === null ? undefined : twOptionDomId(listId, activeValue),
    handleKeyDown,
  } as const;
};

export interface ITwListboxProps<V extends string> {
  /** null while loading: an explicit loading state, never an empty list. */
  options: readonly ITwOption<V>[] | null;
  value: V | null;
  onChange: (value: V) => void;
  id?: string;
  'aria-label'?: string;
  'aria-labelledby'?: string;
  emptyText?: string;
  loadingText?: string;
  /** Controlled active row, for a text field driving the list (see useTwListboxNav). */
  activeValue?: V | null;
  onActiveValueChange?: (value: V | null) => void;
  /** false when focus stays in a text field (combobox). Default true. */
  focusable?: boolean;
  autoFocus?: boolean;
  /** Escape inside a standalone list. */
  onDismiss?: () => void;
  className?: string;
}

/**
 * Single-select list: arrows, Home/End, PageUp/PageDown, typeahead, Enter/Space, aria-activedescendant.
 * Used standalone, inside TwSelect, or driven by a text field through useTwListboxNav.
 */
export const TwListbox = <V extends string>({
  options,
  value,
  onChange,
  id,
  'aria-label': ariaLabel,
  'aria-labelledby': ariaLabelledby,
  emptyText = t('ui.twListbox.empty'),
  loadingText = t('ui.twListbox.loading'),
  activeValue: controlledActive,
  onActiveValueChange,
  focusable = true,
  autoFocus = false,
  onDismiss,
  className,
}: ITwListboxProps<V>) => {
  const ownId = useId();
  const listId = id ?? ownId;
  const listRef = useRef<HTMLDivElement>(null);
  const [ownActive, setOwnActive] = useState<V | null>(value);
  const typeahead = useRef(EMPTY_TYPEAHEAD);
  const fromPointer = useRef(false);
  const controlled = controlledActive !== undefined;
  const active = effectiveActive(options, controlled ? controlledActive : ownActive);
  const activeDomId = active === null ? undefined : twOptionDomId(listId, active);

  const setActive = (next: V | null, pointer: boolean) => {
    fromPointer.current = pointer;
    if (controlled) onActiveValueChange?.(next);
    else setOwnActive(next);
  };

  useLayoutEffect(() => {
    if (autoFocus) focusElement(listRef.current);
  }, [autoFocus]);

  useEffect(() => {
    if (fromPointer.current) {
      fromPointer.current = false;
      return;
    }
    if (activeDomId) document.getElementById(activeDomId)?.scrollIntoView({ block: 'nearest' });
  }, [activeDomId]);

  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!focusable || !options || event.nativeEvent.isComposing) return;
    const now = event.timeStamp;
    if (isTypeaheadKey(event, typeahead.current, now)) {
      event.preventDefault();
      typeahead.current = typeaheadStep(typeahead.current, event.key, now);
      const current = options.findIndex((option) => option.value === active);
      const match = typeaheadIndex(
        options.map((option) => option.label),
        typeahead.current.query,
        current,
        (index) => Boolean(options[index].disabled),
      );
      if (match !== -1) setActive(options[match].value, false);
      return;
    }
    const action = listboxKeyAction(event.key, {
      count: options.length,
      active: options.findIndex((option) => option.value === active),
      loop: false,
      isSkipped: (index) => Boolean(options[index].disabled),
      spaceSelects: true,
      textInput: false,
    });
    if (action.type === LISTBOX_ACTION.move) {
      event.preventDefault();
      setActive(options[action.index].value, false);
    } else if (action.type === LISTBOX_ACTION.select) {
      event.preventDefault();
      onChange(options[action.index].value);
    } else if (action.type === LISTBOX_ACTION.dismiss && onDismiss) {
      event.preventDefault();
      event.stopPropagation();
      onDismiss();
    }
  };

  const status = (() => {
    if (options === null) return loadingText;
    if (!options.length) return emptyText;
    return null;
  })();

  return (
    <div className={cx('tw-ui-listbox-frame', className)}>
      <div
        ref={listRef}
        id={listId}
        role="listbox"
        tabIndex={focusable ? 0 : -1}
        aria-label={ariaLabel}
        aria-labelledby={ariaLabelledby}
        aria-activedescendant={focusable ? activeDomId : undefined}
        aria-busy={options === null || undefined}
        className="tw-ui-listbox"
        onKeyDown={onKeyDown}
      >
        {options?.map((option) => {
          const selected = option.value === value;
          return (
            <div
              key={option.value}
              id={twOptionDomId(listId, option.value)}
              role="option"
              aria-selected={selected}
              aria-disabled={option.disabled || undefined}
              data-active={option.value === active ? '' : undefined}
              className="tw-ui-option"
              onPointerMove={(event) => {
                if (event.pointerType !== 'touch' && !option.disabled && option.value !== active)
                  setActive(option.value, true);
              }}
              onMouseDown={(event) => {
                if (!focusable) event.preventDefault();
              }}
              onClick={() => {
                if (!option.disabled) onChange(option.value);
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
                {selected && <TwCheckIcon />}
              </span>
            </div>
          );
        })}
      </div>
      {status && (
        <p className="tw-ui-listbox-status" role="status" data-loading={options === null ? '' : undefined}>
          {status}
        </p>
      )}
    </div>
  );
};

export interface ITwSelectProps<V extends string> extends Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  'onChange' | 'onClick' | 'onKeyDown' | 'value' | 'type' | 'children' | 'aria-label'
> {
  options: readonly ITwOption<V>[] | null;
  value: V | null;
  onChange: (value: V) => void;
  'aria-label': string;
  placeholder?: string;
  disabled?: boolean;
  /** A pessimistic save is in flight: spinner, no interaction. */
  pending?: boolean;
  side?: typeof LAYER_SIDE.top | typeof LAYER_SIDE.bottom;
  id?: string;
  className?: string;
  listClassName?: string;
}

/** Replacement for a native select: a button opening a TwListbox in a popup. */
export const TwSelect = <V extends string>({
  options,
  value,
  onChange,
  'aria-label': ariaLabel,
  placeholder = t('ui.twListbox.choose'),
  disabled = false,
  pending = false,
  side = LAYER_SIDE.bottom,
  id,
  className,
  listClassName,
  ...rest
}: ITwSelectProps<V>) => {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const baseId = useId();
  const listId = `${baseId}-list`;
  const labelId = `${baseId}-label`;
  const valueId = `${baseId}-value`;
  const selected = options?.find((option) => option.value === value) ?? null;

  return (
    <>
      <button
        {...rest}
        ref={triggerRef}
        id={id}
        type="button"
        disabled={disabled}
        aria-disabled={pending || undefined}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-labelledby={`${labelId} ${valueId}`}
        aria-busy={pending || undefined}
        className={cx('tw-ui-select', className)}
        onClick={() => {
          if (!pending) setOpen((current) => !current);
        }}
        onKeyDown={(event) => {
          if (pending || (event.key !== 'ArrowDown' && event.key !== 'ArrowUp')) return;
          event.preventDefault();
          setOpen(true);
        }}
      >
        <span id={labelId} className="tw-ui-visually-hidden">
          {ariaLabel}
        </span>
        {selected?.icon && (
          <span className="tw-ui-select-icon" aria-hidden="true">
            {selected.icon}
          </span>
        )}
        <span id={valueId} className="tw-ui-select-value" data-placeholder={selected ? undefined : ''}>
          {selected?.label ?? placeholder}
        </span>
        <span className="tw-ui-select-chevron" aria-hidden="true">
          {pending ? <TwSpinner /> : <TwChevronDownIcon />}
        </span>
      </button>
      <TwLayer
        open={open && !disabled && !pending}
        anchor={triggerRef}
        triggerRef={triggerRef}
        onDismiss={() => setOpen(false)}
        kind={LAYER_KIND.listbox}
        side={side}
        align={LAYER_ALIGN.start}
        matchAnchorWidth
        tabIndex={-1}
      >
        <TwListbox
          id={listId}
          options={options}
          value={value}
          aria-labelledby={labelId}
          autoFocus
          className={listClassName}
          onChange={(next) => {
            setOpen(false);
            if (next !== value) onChange(next);
          }}
        />
      </TwLayer>
    </>
  );
};
