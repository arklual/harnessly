// Looking at a project or editing it. A project opened from a chat (`?chat=` in its address) is that chat's work: its
// mode is kept with the chat's pane (stores/pane-state), as the pane's width and tabs are, so a reload, coming back to
// the chat or another window on the same chat finds the mode the person left; the first time it opens for editing.
// Opened any other way (a link, the gallery, a guest) the address says, and without `?edit=1` it is looked at.
import { useEffect } from 'react';
import { useChatPaneView } from '@/next/stores/pane-state';

/** The key of a project's mode in its chat's pane state. */
export const modeKey = (id: string) => `mode:${id}`;

interface IDesignModeArgs {
  /** The chat the project was opened from; null: not from a chat. */
  conv: string | null;
  id: string;
  /** `?edit=1` of the address. */
  edit: boolean;
  /** Writes the mode into the address; absent where there is no switch (the chat's pane always edits). */
  onMode?: (edit: boolean) => void;
}

export const useDesignMode = ({
  conv,
  id,
  edit,
  onMode,
}: IDesignModeArgs): [boolean, (edit: boolean) => void] => {
  const [kept, setKept] = useChatPaneView<boolean | null>(conv, modeKey(id), null);
  const editing = conv !== null ? (kept ?? true) : edit;
  // The address follows the chat's mode, so a link copied from it says the same.
  useEffect(() => {
    if (conv !== null && onMode && edit !== editing) onMode(editing);
  }, [conv, edit, editing, onMode]);
  const set = (next: boolean) => {
    if (conv !== null) setKept(next);
    onMode?.(next);
  };
  return [editing, set];
};

/** What a project is, for the words of its mode. */
export const MODE_KIND = { site: 'site', slides: 'slides', video: 'video', doc: 'doc' } as const;
export type TModeKind = (typeof MODE_KIND)[keyof typeof MODE_KIND];

/** The kind of a library project by its design's kind: a deck, a video, otherwise a design. */
export const modeKindOf = (kind: string | null | undefined): TModeKind => {
  if (kind === 'slides') return MODE_KIND.slides;
  if (kind === 'video') return MODE_KIND.video;
  return MODE_KIND.site;
};

const VIEW_KEYS = {
  site: 'next.site.stateView.site',
  slides: 'next.site.stateView.slides',
  video: 'next.site.stateView.video',
  doc: 'next.site.stateView.doc',
} as const satisfies Record<TModeKind, string>;

const EDIT_KEYS = {
  site: 'next.site.stateEdit.site',
  slides: 'next.site.stateEdit.slides',
  video: 'next.site.stateEdit.video',
  doc: 'next.site.stateEdit.doc',
} as const satisfies Record<TModeKind, string>;

/** The words of the mode: «Редактирование макета» while editing, «Макет» while looking. */
export const modeStateKey = (kind: TModeKind, editing: boolean) =>
  editing ? EDIT_KEYS[kind] : VIEW_KEYS[kind];
