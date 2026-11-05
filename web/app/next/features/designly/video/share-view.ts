// What a video's guest link carries in its `view` (D8): the gateway keeps it as is and hands it back in the guest
// config. `deck: 'video'` tells the guest page to open the player, `video: true` lets the gateway serve the MP4s,
// `format` is the one the link starts in. Pure: the share popover writes it, the guest page reads it.
import type { DesignVideoFormat } from '@/next/protocol/v2';
import type { IGuestLink } from '@/next/features/designly/access/api';
import { VIDEO_FORMATS, type TVideoFormat } from '@/next/features/designly/video/types';

export interface IVideoLinkView {
  v: 1;
  deck: 'video';
  video: true;
  stats: boolean;
  format: DesignVideoFormat;
}

export interface IVideoLinkFlags {
  /** Who looked and for how long is counted. */
  stats: boolean;
  /** The format the link opens in. */
  format: TVideoFormat;
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

const FORMATS: readonly string[] = VIDEO_FORMATS;

/** The `view` of a link for these flags. */
export const videoViewOf = (flags: IVideoLinkFlags): IVideoLinkView => ({
  v: 1,
  deck: 'video',
  video: true,
  stats: flags.stats,
  format: flags.format,
});

/** The flags a link's `view` holds; null where the link is not a video's. A format that is not valid reads as null (the guest picks). */
export const readVideoLinkView = (view: unknown): { stats: boolean; format: TVideoFormat | null } | null => {
  if (!isRecord(view) || view.deck !== 'video') return null;
  const format = typeof view.format === 'string' && FORMATS.includes(view.format) ? view.format : null;
  return { stats: view.stats === true, format: format as TVideoFormat | null };
};

/** The video starts by itself on the guest page (muted, as browsers allow it) — the link's «Автозапуск». */
export const autoplayOn = (view: unknown): boolean => isRecord(view) && view.autoplay === true;

/** Comments show on the timeline unless the link turned them off; an older link has them. */
export const timelineCommentsOn = (view: unknown): boolean => !isRecord(view) || view.tlComments !== false;

/** The format a guest starts in: the link's, if it is published; else 16:9 when it is; else the first published one. Null when nothing is. */
export const startFormat = (view: unknown, published: readonly TVideoFormat[]): TVideoFormat | null => {
  const wanted = readVideoLinkView(view)?.format ?? null;
  if (wanted !== null && published.includes(wanted)) return wanted;
  return VIDEO_FORMATS.find((f) => published.includes(f)) ?? null;
};

/** The video's link among the project's live links: the first whose `view` is a video's. */
export const videoLinkOf = (live: readonly IGuestLink[]): IGuestLink | null =>
  live.find((l) => readVideoLinkView(l.view) !== null) ?? null;

/** A video's formats for its share popover: null when it has none (the settings always have one). */
export const shareFormatsOf = (
  formats: readonly TVideoFormat[],
): readonly [TVideoFormat, ...TVideoFormat[]] | null => {
  const [first, ...rest] = formats;
  return first === undefined ? null : [first, ...rest];
};
