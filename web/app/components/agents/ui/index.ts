/**
 * Hl UI primitives. Own components on --t-* tokens (styles in src/styles/hl-ui.css):
 * no UI libraries, no inline styles, no native select, one responsive implementation.
 * The folder imports nothing outside itself except react and react-dom.
 */
export { cx } from './dom';
export { LAYER_ALIGN, LAYER_SIDE, computePlacement, type TLayerAlign, type TLayerSide } from './placement';
export {
  LISTBOX_ACTION,
  MENU_INTENT,
  NAV_KEY,
  NAV_ORIENTATION,
  listboxKeyAction,
  menuKeyIntent,
  nextIndex,
  typeaheadIndex,
} from './roving-focus';
export {
  DISMISS_REASON,
  useDismissable,
  type IDismissableOptions,
  type TDismissReason,
} from './use-dismissable';
export { type ITwPoint, type TTwAnchor } from './use-anchored-position';
export { TwLayerHostContext } from './tw-layer';
export { POPOVER_SIDE, TwPopover, twPopoverTriggerProps, type ITwPopoverProps } from './tw-popover';
export {
  MENU_CLOSE_REASON,
  MENU_INITIAL_FOCUS,
  TW_MENU_KIND,
  TwMenu,
  TwMenuButton,
  useTwContextMenu,
  type ITwMenuAction,
  type ITwMenuButtonProps,
  type ITwMenuProps,
  type ITwMenuSubmenu,
  type ITwMenuToggle,
  type TMenuCloseReason,
  type TTwMenuEntry,
} from './tw-menu';
export {
  TwListbox,
  TwSelect,
  twOptionDomId,
  useTwListboxNav,
  type ITwListboxProps,
  type ITwOption,
  type ITwSelectProps,
  type TTwListboxNavResult,
} from './tw-listbox';
export { TwSwitch, type ITwSwitchProps } from './tw-switch';
export {
  CHOICE_ACTIVATION,
  SEGMENTED_MODE,
  TwRadioList,
  TwSegmented,
  type ITwChoice,
  type ITwRadioListProps,
  type ITwSegmentedProps,
} from './tw-segmented';
export {
  CONFIRM_TONE,
  DIALOG_SIZE,
  TwConfirm,
  TwDialog,
  type ITwConfirmProps,
  type ITwDialogProps,
} from './tw-confirm';
export { METER_SIZE, METER_TONE, TwMeter, meterTone, type ITwMeterProps } from './tw-meter';
export { TOOLTIP_SIDE, TwTooltip, type ITwTooltipProps } from './tw-tooltip';
export { CHIP_TONE, TwBadge, TwChip, TwKbd, type ITwBadgeProps, type ITwChipProps } from './tw-chip';
export { BUTTON_SIZE, BUTTON_VARIANT, TwButton, type ITwButtonProps } from './tw-button';
export { TwSpinner } from './icons';
