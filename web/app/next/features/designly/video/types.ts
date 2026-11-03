// A video: a design project of the kind `video` — a deck folder with meta/video.json, one board per scene in the order
// of its pages. These are the shapes the runtime's video routes answer (`/design/video/*`,
// internal/design/video_types.go). Times are integer ms everywhere; durations are computed by the runtime only, the
// page lays out what it is given. No React here.
import type { DesignVideoFormat } from '@/next/protocol/v2';
import type { IDeckComment, IDeckSettings, TDeckJobState } from '@/next/features/designly/deck/types';

export const VIDEO_FORMAT = { landscape: '16:9', portrait: '9:16' } as const satisfies Record<
  string,
  DesignVideoFormat
>;
export type TVideoFormat = DesignVideoFormat;
/** Every format, in the order the page shows them. */
export const VIDEO_FORMATS = [VIDEO_FORMAT.landscape, VIDEO_FORMAT.portrait] as const;
/** A format's frame size, px (a draft is half of it). */
export const VIDEO_SIZE = {
  '16:9': { w: 1920, h: 1080 },
  '9:16': { w: 1080, h: 1920 },
} as const satisfies Record<TVideoFormat, { w: number; h: number }>;

export const VIDEO_SECONDS_MIN = 15;
export const VIDEO_SECONDS_MAX = 300;
/** The create screen's lengths, s; null is «Не важно». */
export const VIDEO_SECONDS_CHOICES = [null, 15, 30, 60, 180] as const;
export const VIDEO_SCENE_DUR_MIN = 1000;
export const VIDEO_SCENE_DUR_MAX = 60000;
export const VIDEO_TRANSITION_MS_MAX = 2000;
export const VIDEO_FPS = 30;

/** meta/video.json. */
export interface IVideoSettings {
  v: number;
  /** One or both, in the order of VIDEO_FORMATS. */
  formats: TVideoFormat[];
  fps: number;
  /** null: the length does not matter. */
  seconds: number | null;
  voice: 'none';
  subs: boolean;
}

export const VIDEO_TRANSITION = { none: 'none', fade: 'fade' } as const;
export type TVideoTransitionKind = (typeof VIDEO_TRANSITION)[keyof typeof VIDEO_TRANSITION];

export interface IVideoTransition {
  kind: TVideoTransitionKind;
  /** 0..2000. */
  ms: number;
}

export const VIDEO_BUILD_IN = { fade: 'fade', rise: 'rise', pop: 'pop' } as const;
export type TVideoBuildIn = (typeof VIDEO_BUILD_IN)[keyof typeof VIDEO_BUILD_IN];

/** A timed block of a scene, read from the board's source. */
export interface IVideoMoment {
  /** The block's data-block. */
  block: string;
  /** Its data-dc-tpl; null when not known. */
  tpl: number | null;
  /** ms from the scene's start. */
  at: number;
  /** null: the block stays to the scene's end. */
  out: number | null;
  /** '' when the block has none. */
  buildIn: TVideoBuildIn | '';
  /** The time was derived (from data-step), not written on the board: drawn dashed. */
  derived: boolean;
}

export const VIDEO_OVERLAY_KINDS = [
  'title',
  'caption',
  'callout',
  'highlight',
  'key',
  'counter',
  'badge',
] as const;
export type TVideoOverlayKind = (typeof VIDEO_OVERLAY_KINDS)[number];
export const VIDEO_OVERLAY_ENTERS = [
  'fade',
  'rise',
  'pop',
  'typewriter',
  'mask',
  'words',
  'scramble',
] as const;
export type TVideoOverlayEnter = (typeof VIDEO_OVERLAY_ENTERS)[number];
export const VIDEO_OVERLAY_LEAVES = ['none', 'fade', 'fall', 'shrink'] as const;
export type TVideoOverlayLeave = (typeof VIDEO_OVERLAY_LEAVES)[number];
export const VIDEO_OVERLAY_THEMES = ['auto', 'accent', 'dark', 'light'] as const;
export type TVideoOverlayTheme = (typeof VIDEO_OVERLAY_THEMES)[number];
export const VIDEO_OVERLAY_ANCHORS = ['left', 'center', 'right'] as const;
export type TVideoOverlayAnchor = (typeof VIDEO_OVERLAY_ANCHORS)[number];
export const VIDEO_OVERLAY_TEXT_MAX = 200;
export const VIDEO_OVERLAY_MIN_MS = 100;

/** Where an overlay stands in a format: a box in fractions of the frame (0..1) and, for a callout, its arrow's end. */
export interface IVideoOverlayPlace {
  x: number;
  y: number;
  w: number;
  h: number;
  tx: number | null;
  ty: number | null;
}

/** One element of a scene's overlay layer (internal/design/video_overlays.go). at/out: ms from the scene's start. */
export interface IVideoOverlay {
  id: string;
  kind: TVideoOverlayKind;
  text: string;
  at: number;
  /** null: to the scene's end. */
  out: number | null;
  x: number;
  y: number;
  w: number;
  h: number;
  tx: number | null;
  ty: number | null;
  /** The 9:16 place of its own; null: the base place moved into the safe zone. */
  portrait: IVideoOverlayPlace | null;
  anchor: TVideoOverlayAnchor;
  enter: TVideoOverlayEnter;
  leave: TVideoOverlayLeave;
  theme: TVideoOverlayTheme;
  value: number | null;
  from: number;
  decimals: number;
  author: 'human' | 'agent';
  updatedAt: string;
}

export interface IVideoScene {
  /** The page's id: the key of its notes, comments and meta/scenes file. */
  id: string;
  path: string;
  title: string;
  /** ms from the video's start: the sum of the earlier scenes' durations. */
  start: number;
  dur: number;
  /** dur came from the runtime's automatic rule, not from the scene's own setting. */
  auto: boolean;
  transition: IVideoTransition;
  moments: IVideoMoment[];
  overlays: IVideoOverlay[];
  words: number;
  /** The scene's text (meta/notes), which is also the subtitles. */
  notes: string;
  version: string;
  blank: boolean;
  /** The scene clip's cache key per enabled format. */
  keys: Partial<Record<TVideoFormat, string>>;
}

/** A format's published MP4 against today's inputs. */
export interface IVideoRenderInfo {
  format: TVideoFormat;
  published: boolean;
  /** The published file's inputs key; null when there is none. */
  key: string | null;
  currentKey: string;
  /** key === currentKey: the MP4 is current. */
  fresh: boolean;
  durMs: number | null;
  at: string | null;
}

export interface IVideoRenderJob {
  id: string;
  format: TVideoFormat;
  /** A single scene's clip; null for the whole video. */
  scene: string | null;
  draft: boolean;
  state: TDeckJobState;
  /** 0..1. */
  progress: number;
  file: string | null;
  /** Worded for the person. */
  error: string | null;
  at: string;
}

/** A video's part of a team comment: on a moment (t1 null) or a stretch of one scene. */
export interface IVideoCommentAt {
  /** ms from the scene's (the comment's page's) start. */
  t0: number;
  t1: number | null;
  format: TVideoFormat;
  /** The data-block it points at; '' when none. */
  block: string;
  /** The cached frame's path under the project; '' when none. */
  frame: string;
}

/** A team comment as a video reads it: a deck comment, with `video` null for one made on the board, not on a time. */
export interface IVideoComment extends IDeckComment {
  video: IVideoCommentAt | null;
}

/** GET /design/video. A video not read yet has no state at all (the store has no entry): loading ≠ empty. */
export interface IVideoState {
  rev: number;
  settings: IVideoSettings;
  deck: IDeckSettings;
  chrome: boolean;
  ffmpeg: boolean;
  durMs: number;
  scenes: IVideoScene[];
  comments: IVideoComment[];
  guestResolved: string[];
  /** One per enabled format. */
  renders: IVideoRenderInfo[];
  /** The queued or running render; null when none. */
  job: IVideoRenderJob | null;
}

/** The body of PUT /design/video/settings: only the keys that change. */
export type TVideoSettingsPatch = Partial<Pick<IVideoSettings, 'formats' | 'subs' | 'seconds'>>;

/** The body of POST /design/video/comments, less the artifact. */
export interface IVideoCommentDraft {
  page: string;
  t0: number;
  t1: number | null;
  format: TVideoFormat;
  /** Percent of the frame, 0..100; null when the comment has no point. */
  x: number | null;
  y: number | null;
  block: string;
  label: string;
  text: string;
}

// ---- the page's own states the video's screens share (the view holds them, the parts take them as props)

/** A stretch of the video, ms of the whole video, t0 ≤ t1. */
export interface IVideoRange {
  t0: number;
  t1: number;
}

/** What the player shows: the scenes live in frames, or the published MP4 in a `<video>`. */
export const VIDEO_SOURCE = { live: 'live', mp4: 'mp4' } as const;
export type TVideoSource = (typeof VIDEO_SOURCE)[keyof typeof VIDEO_SOURCE];

/** A block picked in a scene (`tk:video-hit`) for a comment. */
export interface IVideoPick {
  page: string;
  block: string | null;
  /** Percent of the frame, 0..100. */
  x: number;
  y: number;
}

/** What a comment is about, as the composer's chip says it: «Сцена 3 · 0:12–0:15 · Заголовок · 9:16». */
export interface IVideoCommentTarget {
  page: string;
  /** ms from the scene's start; t1 null: a moment. */
  t0: number;
  t1: number | null;
  format: TVideoFormat;
  pick: IVideoPick | null;
}
