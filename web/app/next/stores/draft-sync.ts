// What each draft was when this device last agreed with the machine about it (its base: the revision, the text and the
// files of that revision), and the rules that decide between the field and a draft written on another device. The
// field is pristine while its text and files are still its base's: only then may a draft from elsewhere replace it. A
// field the person changed keeps what it has, takes the newer revision as its base, and its next write wins. Kept in
// IndexedDB with the draft, so a reload knows which revision its field came from.
//
// The files go with the text: a send on one device clears both everywhere, and a field elsewhere that still held the
// files (a second tab, a reload from IndexedDB) does not keep them under an empty text.

import { createKeyedStore } from '@/next/core/store';
import type { AttachmentRef, Draft, MessageRef } from '@/next/protocol/v2';
import type { DraftKey } from '@/next/stores/drafts';

/** A field as the rules see it. */
export interface DraftField {
  text: string;
  attachments: readonly AttachmentRef[];
  /** The places the field points at (a design, a document…); none when absent. */
  refs?: readonly MessageRef[];
}

export interface DraftBase extends DraftField {
  rev: number;
}

export const draftBaseStore = createKeyedStore<DraftKey, DraftBase>();

/** The field of no revision: a field this device never synced counts as pristine while it is empty. */
const NOTHING: DraftField = { text: '', attachments: [] };

const sameFiles = (a: readonly AttachmentRef[], b: readonly AttachmentRef[]): boolean =>
  a.length === b.length && a.every((f, i) => f.id === b[i].id);

const NO_REFS: readonly MessageRef[] = [];
const refKey = (r: MessageRef): string => `${r.kind}:${r.id}`;
const sameRefs = (a: readonly MessageRef[] = NO_REFS, b: readonly MessageRef[] = NO_REFS): boolean =>
  a.length === b.length && a.every((r, i) => refKey(r) === refKey(b[i]));

export const sameField = (a: DraftField, b: DraftField): boolean =>
  a.text === b.text && sameFiles(a.attachments, b.attachments) && sameRefs(a.refs, b.refs);

/** The field is as its base left it: nothing typed or attached here since. */
export const isPristine = (local: DraftField, base: DraftBase | undefined): boolean =>
  sameField(local, base ?? NOTHING);

/** A draft of the machine as a field: no `attachments` is none. */
export const fieldOf = (d: Pick<Draft, 'text' | 'attachments' | 'refs'>): DraftField => ({
  text: d.text,
  attachments: d.attachments ?? [],
  ...(d.refs && d.refs.length > 0 ? { refs: d.refs } : {}),
});

export const REMOTE = {
  /** Older than or the same as what this device already has: nothing to do. */
  ignore: 'ignore',
  /** The field had not changed: it takes the other device's text and files. */
  take: 'take',
  /** The field changed here: it keeps what it has, the remote revision becomes the base; write it again if it differs. */
  keep: 'keep',
} as const;
export type RemoteVerdict = (typeof REMOTE)[keyof typeof REMOTE];

/**
 * A draft from the machine against the field. `busy`: a write of this field is on its way, so the field is not
 * pristine whatever it holds (it holds what that write carries).
 */
export function judgeRemote(
  local: DraftField,
  base: DraftBase | undefined,
  remote: Pick<Draft, 'rev'>,
  busy: boolean,
): RemoteVerdict {
  if (base && remote.rev <= base.rev) return REMOTE.ignore;
  if (!busy && isPristine(local, base)) return REMOTE.take;
  return REMOTE.keep;
}

/** The write the field needs, or null when the machine already has it. */
export function nextWrite(
  local: DraftField,
  base: DraftBase | undefined,
): (DraftField & { rev: number }) | null {
  if (isPristine(local, base)) return null;
  return {
    text: local.text,
    attachments: [...local.attachments],
    ...(local.refs && local.refs.length > 0 ? { refs: [...local.refs] } : {}),
    rev: (base ? base.rev : 0) + 1,
  };
}
