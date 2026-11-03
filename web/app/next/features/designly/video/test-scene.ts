// A scene for the timeline's and the comments' tests.
import type { IVideoOverlayEditor } from '@/next/features/designly/video/use-video-overlays';
import type { IVideoScene } from '@/next/features/designly/video/types';

export const scene = (
  id: string,
  start: number,
  dur: number,
  moments: IVideoScene['moments'] = [],
): IVideoScene => ({
  id,
  path: `${id}.dc.html`,
  title: id,
  start,
  dur,
  auto: false,
  transition: { kind: 'none', ms: 0 },
  moments,
  overlays: [],
  words: 0,
  notes: '',
  version: 'v1',
  blank: false,
  keys: {},
});

/** An overlay editor that edits nothing: for the player's tests. */
export const noOverlays: IVideoOverlayEditor = {
  on: false,
  setOn: () => undefined,
  selected: null,
  select: () => undefined,
  editing: null,
  setEditing: () => undefined,
  overlaysOf: (s) => s.overlays,
  preview: () => undefined,
  discard: () => undefined,
  save: () => Promise.resolve(true),
  add: () => Promise.resolve(null),
  remove: () => Promise.resolve(true),
  error: null,
};
