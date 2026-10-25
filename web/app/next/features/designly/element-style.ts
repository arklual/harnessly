// The look of a picked block that the note's card lets the person try on: the fill, the corner radius and the opacity.
// The page shows it at once (the bridge's `tk:styles`) and the note carries it to the agent in the annotation block, which
// is how the source gets changed. An edit holds only what differs from the block as the page has it; null is «as it is».
import type { IPickedLook } from '@/next/features/designly/page-bridge';

export const STYLE_KEYS = ['background', 'radius', 'opacity'] as const;
export type TStyleKey = (typeof STYLE_KEYS)[number];

export interface IStyleEdit {
  /** `#rrggbb`; null leaves the fill as the page has it. */
  background: string | null;
  /** Pixels. */
  radius: number | null;
  /** Percent. */
  opacity: number | null;
}

export const NO_EDIT: IStyleEdit = { background: null, radius: null, opacity: null };

export const RADIUS = { min: 0, max: 40, step: 2 } as const;
export const OPACITY = { min: 10, max: 100, step: 5 } as const;
export const SWATCHES = [
  '#FFFFFF',
  '#0B0B0F',
  '#7C5CFF',
  '#2F80ED',
  '#16A34A',
  '#F59E0B',
  '#E5484D',
] as const;

/** What the block's opacity is in percent. */
export const percentOf = (opacity: number) => Math.round(opacity * 100);

const sameColor = (a: string | null, b: string | null) => a?.toLowerCase() === b?.toLowerCase();

/** The edit with `key` set; a value the block already has is no edit. */
export const withValue = <K extends TStyleKey>(
  edit: IStyleEdit,
  look: IPickedLook,
  key: K,
  value: IStyleEdit[K],
): IStyleEdit => {
  const next = { ...edit, [key]: value };
  if (key === 'radius' && next.radius === look.radius) next.radius = null;
  if (key === 'opacity' && next.opacity === percentOf(look.opacity)) next.opacity = null;
  if (key === 'background' && next.background !== null && sameColor(next.background, look.background))
    next.background = null;
  return next;
};

export const changedKeys = (edit: IStyleEdit) => STYLE_KEYS.filter((key) => edit[key] !== null);
export const hasEdit = (edit: IStyleEdit) => changedKeys(edit).length > 0;

/** What a control shows: the edit, else the block's own value (null: unknown, or the fill is as the theme gives it). */
export const shownValues = (edit: IStyleEdit, look: IPickedLook) => ({
  background: edit.background,
  radius: edit.radius ?? look.radius,
  opacity: edit.opacity ?? percentOf(look.opacity),
});

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/** One step of the radius stepper; a radius the page gives in other units starts from nothing. */
export const stepRadius = (current: number | null, direction: -1 | 1) =>
  clamp((current ?? 0) + direction * RADIUS.step, RADIUS.min, RADIUS.max);

/** A look as the bridge takes it. */
export interface IStyleItem {
  id: number;
  background: string | null;
  radius: number | null;
  /** 0 to 1. */
  opacity: number | null;
}

export const toItem = (id: number, edit: IStyleEdit): IStyleItem => ({
  id,
  background: edit.background,
  radius: edit.radius,
  opacity: edit.opacity === null ? null : edit.opacity / 100,
});

/** The looks the page should wear: of every note, the card's own edit in place of its note's. */
export const itemsOf = (
  notes: readonly { id: string; picked: { id: number | null }; style: IStyleEdit }[],
  draft: { id?: string; picked: { id: number | null }; style: IStyleEdit } | null,
): IStyleItem[] => {
  const items: IStyleItem[] = [];
  for (const note of notes) {
    const own = draft?.id === note.id ? draft : note;
    if (own.picked.id !== null && hasEdit(own.style)) items.push(toItem(own.picked.id, own.style));
  }
  if (draft && draft.id === undefined && draft.picked.id !== null && hasEdit(draft.style))
    items.push(toItem(draft.picked.id, draft.style));
  return items;
};

/** The line of the annotation block: what is tried on the page and was before, in CSS the agent can put into the source. */
export const styleLine = (edit: IStyleEdit, look: IPickedLook | null): string => {
  const parts: string[] = [];
  if (edit.background !== null) {
    const was = look?.background ? ` (was ${look.background})` : '';
    parts.push(`background: ${edit.background}${was}`);
  }
  if (edit.radius !== null) {
    const was = look && look.radius !== null ? ` (was ${look.radius}px)` : '';
    parts.push(`border-radius: ${edit.radius}px${was}`);
  }
  if (edit.opacity !== null) {
    const was = look ? ` (was ${Number(look.opacity.toFixed(2))})` : '';
    parts.push(`opacity: ${edit.opacity / 100}${was}`);
  }
  return parts.join('; ');
};
