// The videos the page has open, keyed by the project's id: the runtime's state (`GET /design/video`) as last read, and
// the changes the page made since (a scene's duration set, a comment added or resolved, a render started) laid over it
// until the next read. A video not read yet has no entry: its screens show their loading state, not an empty video.
// Kept for the page only.
import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type {
  IVideoComment,
  IVideoRenderJob,
  IVideoScene,
  IVideoSettings,
  IVideoState,
} from '@/next/features/designly/video/types';

export const videoStore = createKeyedStore<string, IVideoState>();

/** The video as last read; undefined until the first read. */
export const useVideo = (artifact: string | null): IVideoState | undefined => useKey(videoStore, artifact);

/** A fresh read: an older `rev` than the one held changes nothing (a poll that crossed a write). */
export function setVideo(artifact: string, state: IVideoState): void {
  videoStore.patch(artifact, (have) => (have && have.rev > state.rev ? have : state));
  flush();
}

/** Changes a video the page holds; nothing for one not read yet. */
export function changeVideo(artifact: string, change: (have: IVideoState) => IVideoState): void {
  videoStore.patch(artifact, (have) => (have ? change(have) : undefined));
  flush();
}

/**
 * Replaces a scene the runtime answered. The starts and the total are not recomputed here: durations are the runtime's
 * (the next read brings them).
 */
export const putVideoSceneLocal = (artifact: string, scene: IVideoScene): void =>
  changeVideo(artifact, (v) => ({ ...v, scenes: v.scenes.map((s) => (s.id === scene.id ? scene : s)) }));

export const putVideoSettingsLocal = (artifact: string, settings: IVideoSettings): void =>
  changeVideo(artifact, (v) => ({ ...v, settings }));

/** Adds a comment or replaces the one with its id. */
export const putVideoComment = (artifact: string, comment: IVideoComment): void =>
  changeVideo(artifact, (v) => {
    const at = v.comments.findIndex((c) => c.id === comment.id);
    const comments = at < 0 ? [...v.comments, comment] : v.comments.map((c, i) => (i === at ? comment : c));
    return { ...v, comments };
  });

/** Marks a guest comment (`g-<id>`) resolved or not. */
export const putVideoGuestResolved = (artifact: string, id: string, resolved: boolean): void =>
  changeVideo(artifact, (v) => {
    const rest = v.guestResolved.filter((x) => x !== id);
    return { ...v, guestResolved: resolved ? [...rest, id] : rest };
  });

/** The render the page started or polled; null once none runs. */
export const putVideoJob = (artifact: string, job: IVideoRenderJob | null): void =>
  changeVideo(artifact, (v) => ({ ...v, job }));

export const dropVideo = (artifact: string): void => {
  videoStore.delete(artifact);
  flush();
};

/** Replaces a scene's overlay layer with the list the runtime answered. */
export const putVideoOverlays = (artifact: string, page: string, overlays: IVideoScene['overlays']): void =>
  changeVideo(artifact, (v) => ({
    ...v,
    scenes: v.scenes.map((s) => (s.id === page ? { ...s, overlays } : s)),
  }));
