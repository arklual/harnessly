// The 3D scenes the page has open, keyed by the project's id: the runtime's state (`GET /design/scene3d`) as last read and
// the changes the page made since (a pin added or resolved) laid over it until the next read.
import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type { IScene3dPin, IScene3dState } from '@/next/features/designly/scene3d/types';

export const scene3dStore = createKeyedStore<string, IScene3dState>();

export const useScene3d = (artifact: string | null): IScene3dState | undefined =>
  useKey(scene3dStore, artifact);

/** A fresh read: an older `rev` than the one held changes nothing (a poll that crossed a write). */
export function setScene3d(artifact: string, state: IScene3dState): void {
  scene3dStore.patch(artifact, (have) => (have && have.rev > state.rev ? have : state));
  flush();
}

export function changeScene3d(artifact: string, change: (have: IScene3dState) => IScene3dState): void {
  scene3dStore.patch(artifact, (have) => (have ? change(have) : undefined));
  flush();
}

/** Adds a pin or replaces the one with its id. */
export const putScene3dPin = (artifact: string, pin: IScene3dPin): void =>
  changeScene3d(artifact, (s) => {
    const at = s.pins.findIndex((p) => p.id === pin.id);
    return { ...s, pins: at < 0 ? [...s.pins, pin] : s.pins.map((p, i) => (i === at ? pin : p)) };
  });

export const resolveScene3dPin = (artifact: string, id: string, resolved: boolean): void =>
  changeScene3d(artifact, (s) => ({ ...s, pins: s.pins.map((p) => (p.id === id ? { ...p, resolved } : p)) }));
