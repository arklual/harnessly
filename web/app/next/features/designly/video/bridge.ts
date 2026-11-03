// The player's words with a scene's own script (internal/design/texts/video-runtime.js, which carries the same table
// in its header comment). The host seeks, plays, pauses and hit-tests; the scene says it is ready with its moments, its
// time while it plays (at most 4 per second), what is at a point, and its errors. The scene is foreign code: every field
// is checked and clipped here. The scene takes a message only from window.parent; where a message may come from (the
// frame's window and origin) is the caller's check on this side. No React here.
//
// The headless side (the render, the check) talks to the same script through
// `window.tkVideo = {v: 1, ready(): Promise, seek(ms): Promise<{same: boolean}>, audit(): Promise<issue[]>, moments()}`.

import type { IVideoTransition } from '@/next/features/designly/video/types';

/** The scene's pixels: x, y, width, height. */
export type TVideoRect = [number, number, number, number];

/** Host → scene. */
export type TVideoHostMessage =
  | { type: 'tk:video-seek'; t: number }
  | { type: 'tk:video-play'; t: number; rate: number }
  | { type: 'tk:video-pause' }
  /** Which block is at a point (scene px); answered by `tk:video-hit` with the same `id`. */
  | { type: 'tk:video-hit'; id: number; x: number; y: number }
  /** The scene's transition as the runtime has it (it wins over the board's own); null: the board's own. */
  | {
      type: 'tk:video-scene';
      transition: IVideoTransition | null;
      /** The scene's overlay layer as the runtime draws it (omitted: left as it is). */
      overlays?: readonly Record<string, unknown>[];
    };

/** A moment as the scene's script tells it (ms from the scene's start). */
export interface IVideoBoardMoment {
  block: string;
  at: number;
  out: number | null;
}

/** Scene → host, as read. */
export type TVideoBoardMessage =
  | { k: 'ready'; moments: IVideoBoardMoment[] }
  | { k: 'time'; t: number }
  | { k: 'hit'; id: number; block: string | null; rect: TVideoRect | null }
  | { k: 'error'; msg: string };

export const VIDEO_MSG = {
  seek: 'tk:video-seek',
  play: 'tk:video-play',
  pause: 'tk:video-pause',
  hit: 'tk:video-hit',
  scene: 'tk:video-scene',
  ready: 'tk:video-ready',
  time: 'tk:video-time',
  error: 'tk:video-error',
} as const;

/** Times past an hour are not a scene's. */
const T_MAX = 3_600_000;
const MOMENTS_MAX = 200;

const finite = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);
const time = (v: unknown): number | null => {
  const n = finite(v);
  return n !== null && n >= 0 && n <= T_MAX ? Math.round(n) : null;
};
const clip = (v: unknown, max: number): string | null => (typeof v === 'string' ? v.slice(0, max) : null);

const readRect = (v: unknown): TVideoRect | null => {
  if (!Array.isArray(v) || v.length !== 4) return null;
  const [x, y, w, h] = v.map(finite);
  if (x === null || y === null || w === null || h === null) return null;
  if (x === undefined || y === undefined || w === undefined || h === undefined) return null;
  return w >= 0 && h >= 0 ? [x, y, w, h] : null;
};

const readMoment = (v: unknown): IVideoBoardMoment | null => {
  if (!v || typeof v !== 'object') return null;
  const r = v as Record<string, unknown>;
  const at = time(r.at);
  if (at === null) return null;
  const out = r.out === null || r.out === undefined ? null : time(r.out);
  return { block: clip(r.block, 120) ?? '', at, out };
};

/** A scene's message as data; null for anything that is not one of the scene's words. */
export const readVideoMessage = (data: unknown): TVideoBoardMessage | null => {
  if (!data || typeof data !== 'object') return null;
  const r = data as Record<string, unknown>;
  switch (r.type) {
    case VIDEO_MSG.ready: {
      if (!Array.isArray(r.moments)) return null;
      return { k: 'ready', moments: r.moments.slice(0, MOMENTS_MAX).flatMap((m) => readMoment(m) ?? []) };
    }
    case VIDEO_MSG.time: {
      const t = time(r.t);
      return t === null ? null : { k: 'time', t };
    }
    case VIDEO_MSG.hit: {
      const id = finite(r.id);
      if (id === null || !Number.isInteger(id)) return null;
      return { k: 'hit', id, block: clip(r.block, 120), rect: readRect(r.rect) };
    }
    case VIDEO_MSG.error:
      return { k: 'error', msg: clip(r.msg, 400) ?? '' };
  }
  return null;
};

/** Sends a message to the scene in a frame, to the frame's own origin only. */
export const postVideo = (
  frame: HTMLIFrameElement | null,
  src: string | null,
  message: TVideoHostMessage,
) => {
  if (!frame?.contentWindow || !src) return;
  frame.contentWindow.postMessage(message, new URL(src, window.location.href).origin);
};
