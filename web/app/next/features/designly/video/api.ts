// The video's runtime routes (`<platform>/design/video/*`, cmd/runtime/routes_design_video.go): every call the video's
// screens make, answered as read by ./wire. A refusal in the runtime's words is a thrown RuntimeError. The deck's routes
// (style, notes, share) work on a video too: use deck/api.ts for them.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { RuntimeError, postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import {
  readVideoComment,
  readVideoOverlays,
  readVideoRenderJob,
  readVideoScene,
  readVideoSettings,
  readVideoState,
} from '@/next/features/designly/video/wire';
import type {
  IVideoComment,
  IVideoCommentDraft,
  IVideoOverlay,
  IVideoRenderJob,
  IVideoScene,
  IVideoSettings,
  IVideoState,
  IVideoTransition,
  TVideoFormat,
  TVideoSettingsPatch,
} from '@/next/features/designly/video/types';

type TPath = `/${string}`;

const video = (path: string): TPath => platformPath(`/design/video${path}` as TPath);
const q = (params: Record<string, string | number | boolean>) =>
  Object.entries(params)
    .map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`)
    .join('&');

const jsonInit = (method: 'PUT' | 'PATCH', body: unknown): RequestInit => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

/** An answer the reader could not take is the runtime not speaking the video's language (an older one). */
const must = <T>(v: T | null): T => {
  if (v === null) throw new RuntimeError(t('next.failure.http'));
  return v;
};

// ---- what the person sees and what the agent asks of the player

export interface IVideoViewCmd {
  seq: number;
  kind: 'seek' | 'play' | 'pause' | 'format';
  t?: number;
  t1?: number;
  loop?: boolean;
  format?: TVideoFormat;
  offer?: boolean;
}

export const reportVideoView = async (
  m: MachineId,
  artifact: string,
  report: { t: number; playing: boolean; format: TVideoFormat; touchedAgo: number; acked: number },
): Promise<readonly IVideoViewCmd[]> => {
  const answer = await runtimeJson<{ cmds: readonly IVideoViewCmd[] }>(
    m,
    video('/view'),
    postJson({ artifact, ...report }),
  );
  return answer.cmds;
};

// ---- the video, settings, scenes, comments (WP1's routes)

export const fetchVideo = async (
  m: MachineId,
  artifact: string,
  signal?: AbortSignal,
): Promise<IVideoState> =>
  must(readVideoState(await runtimeJson(m, video(`?${q({ artifact })}`), { signal, cache: 'no-store' })));

export const putVideoSettings = async (
  m: MachineId,
  artifact: string,
  settings: TVideoSettingsPatch,
): Promise<IVideoSettings> => {
  const answer = await runtimeJson<{ settings: unknown }>(
    m,
    video('/settings'),
    jsonInit('PUT', { artifact, settings }),
  );
  return readVideoSettings(answer.settings);
};

/** A scene's duration (ms, null: automatic) and transition (null: the board's own). */
export const putVideoScene = async (
  m: MachineId,
  artifact: string,
  page: string,
  dur: number | null,
  transition: IVideoTransition | null,
): Promise<IVideoScene> => {
  const answer = await runtimeJson<{ scene: unknown }>(
    m,
    video('/scene'),
    jsonInit('PUT', { artifact, page, dur, transition }),
  );
  return must(readVideoScene(answer.scene));
};

export const addVideoComment = async (
  m: MachineId,
  artifact: string,
  draft: IVideoCommentDraft,
): Promise<IVideoComment> =>
  must(readVideoComment(await runtimeJson(m, video('/comments'), postJson({ artifact, ...draft }))));

/** `id` may be a guest comment's `g-<id>`: the answer then is only `{id, resolved}`. */
export const resolveVideoComment = async (
  m: MachineId,
  artifact: string,
  id: string,
  resolved: boolean,
): Promise<{ id: string; resolved: boolean; comment: IVideoComment | null }> => {
  const answer = await runtimeJson<Record<string, unknown>>(
    m,
    video('/comments'),
    jsonInit('PATCH', { artifact, id, resolved }),
  );
  return { id, resolved: answer.resolved === true, comment: readVideoComment(answer) };
};

// ---- frames (WP2a's route)

/** A scene's frame at `at` ms as a JPEG, for an `<img>`. */
export const videoFrameHref = (
  m: MachineId,
  artifact: string,
  page: string,
  at: number,
  format: TVideoFormat,
  draft: boolean,
) => runtimePath(video(`/frame?${q({ artifact, page, at, format, ...(draft ? { draft: 1 } : {}) })}`), m);

// ---- renders (WP2b's routes)

/** Starts a render (or joins the same one running). `page`: one scene's clip only, never published. */
export const startVideoRender = async (
  m: MachineId,
  artifact: string,
  format: TVideoFormat,
  draft: boolean,
  page?: string,
): Promise<IVideoRenderJob> =>
  must(
    readVideoRenderJob(
      await runtimeJson(
        m,
        video('/render'),
        postJson({ artifact, format, draft, ...(page ? { page } : {}) }),
      ),
    ),
  );

export const videoRenderJob = async (
  m: MachineId,
  artifact: string,
  id: string,
  signal?: AbortSignal,
): Promise<IVideoRenderJob> =>
  must(
    readVideoRenderJob(
      await runtimeJson(m, video(`/render?${q({ artifact, id })}`), { signal, cache: 'no-store' }),
    ),
  );

/** Where a format's published MP4 downloads from (an attachment), or plays in a `<video>` (served with Range). */
export const videoFileHref = (m: MachineId, artifact: string, format: TVideoFormat) =>
  runtimePath(video(`/file?${q({ artifact, format })}`), m);

// ---- the overlay layer

/** What a call to the overlay route may name: the overlay's fields (a null clears an optional one). */
export type TVideoOverlayPatch = Record<string, unknown>;

/** Adds (`add`, with a kind in the patch), changes (`set`) or removes (`rm`) an overlay as a person; the answer is the scene's list. */
export const postVideoOverlay = async (
  m: MachineId,
  artifact: string,
  page: string,
  action: 'add' | 'set' | 'rm',
  id: string | null,
  patch: TVideoOverlayPatch,
): Promise<{ id: string | null; overlays: IVideoOverlay[] }> => {
  const answer = await runtimeJson<{ overlay?: { id?: unknown } | null; overlays?: unknown }>(
    m,
    video('/overlay'),
    postJson({ artifact, page, action, ...(id !== null ? { id } : {}), ...patch }),
  );
  const made = typeof answer.overlay?.id === 'string' ? answer.overlay.id : id;
  return { id: made, overlays: readVideoOverlays(answer.overlays) };
};
