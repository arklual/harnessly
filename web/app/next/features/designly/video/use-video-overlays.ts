// The person's edit of a scene's overlay layer (HRN-653). A drag or a typed text is shown at once as a draft; the save is
// pessimistic: the draft goes away when the runtime has answered, and on a refusal it goes back to what was and a short
// sentence says so. The agent's changes come in the way every change of the scene does (the 3 s read of the video).
import { useCallback, useEffect, useRef, useState } from 'react';
import { postVideoOverlay, type TVideoOverlayPatch } from '@/next/features/designly/video/api';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { putVideoOverlays } from '@/next/stores/video';
import type { IVideoOverlay, IVideoScene } from '@/next/features/designly/video/types';

/** How long the sentence about a failed save stays, ms. */
const ERROR_MS = 4000;

export interface IVideoOverlayEditor {
  /** The mode is on: the layer of boxes is over the picture. */
  on: boolean;
  setOn: (on: boolean) => void;
  selected: string | null;
  select: (id: string | null) => void;
  /** The overlay whose text is typed in place. */
  editing: string | null;
  setEditing: (id: string | null) => void;
  /** A scene's overlays with the drafts laid over. */
  overlaysOf: (scene: IVideoScene) => readonly IVideoOverlay[];
  /** Shows a change at once, without saving it. */
  preview: (page: string, overlay: IVideoOverlay) => void;
  /** Drops a preview (a cancelled drag). */
  discard: (id: string) => void;
  /** Saves a change; the preview stays until the runtime has answered. */
  save: (page: string, id: string, patch: TVideoOverlayPatch) => Promise<boolean>;
  add: (page: string, patch: TVideoOverlayPatch) => Promise<string | null>;
  remove: (page: string, id: string) => Promise<boolean>;
  /** Why the last save failed, worded for the person; null when it did not. */
  error: string | null;
}

export const useVideoOverlays = (machine: MachineId, artifact: string): IVideoOverlayEditor => {
  const [on, setOnState] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [drafts, setDrafts] = useState<Readonly<Record<string, IVideoOverlay>>>({});
  const [error, setError] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const fail = useCallback(() => {
    setError(t('next.video.overlay.saveFailed'));
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setError(null), ERROR_MS);
  }, []);

  const setOn = useCallback((next: boolean) => {
    setOnState(next);
    if (!next) {
      setSelected(null);
      setEditing(null);
    }
  }, []);

  const select = useCallback((id: string | null) => {
    setSelected(id);
    setEditing((cur) => (cur === id ? cur : null));
  }, []);

  const overlaysOf = useCallback(
    (scene: IVideoScene): readonly IVideoOverlay[] => {
      if (Object.keys(drafts).length === 0) return scene.overlays;
      return scene.overlays.map((o) => drafts[o.id] ?? o);
    },
    [drafts],
  );

  const preview = useCallback((_page: string, overlay: IVideoOverlay) => {
    setDrafts((cur) => ({ ...cur, [overlay.id]: overlay }));
  }, []);

  const drop = useCallback((id: string) => {
    setDrafts((cur) => {
      if (!(id in cur)) return cur;
      const { [id]: _gone, ...rest } = cur;
      return rest;
    });
  }, []);

  const save = useCallback(
    async (page: string, id: string, patch: TVideoOverlayPatch): Promise<boolean> => {
      try {
        const answer = await postVideoOverlay(machine, artifact, page, 'set', id, patch);
        putVideoOverlays(artifact, page, answer.overlays);
        drop(id);
        return true;
      } catch {
        drop(id);
        fail();
        return false;
      }
    },
    [machine, artifact, drop, fail],
  );

  const add = useCallback(
    async (page: string, patch: TVideoOverlayPatch): Promise<string | null> => {
      try {
        const answer = await postVideoOverlay(machine, artifact, page, 'add', null, patch);
        putVideoOverlays(artifact, page, answer.overlays);
        return answer.id;
      } catch {
        fail();
        return null;
      }
    },
    [machine, artifact, fail],
  );

  const remove = useCallback(
    async (page: string, id: string): Promise<boolean> => {
      try {
        const answer = await postVideoOverlay(machine, artifact, page, 'rm', id, {});
        putVideoOverlays(artifact, page, answer.overlays);
        setSelected((cur) => (cur === id ? null : cur));
        setEditing((cur) => (cur === id ? null : cur));
        return true;
      } catch {
        fail();
        return false;
      }
    },
    [machine, artifact, fail],
  );

  return {
    on,
    setOn,
    selected,
    select,
    editing,
    setEditing,
    overlaysOf,
    preview,
    discard: drop,
    save,
    add,
    remove,
    error,
  };
};
