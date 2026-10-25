// The text being edited in the pane's file tabs. An editor is mounted only while its tab is on screen; what it holds
// unsaved goes here when it leaves and comes back when the tab does, so a switch of tabs or a closed pane loses nothing.
import { useCallback, useRef, useState } from 'react';
import type { IDraft, IFileEditorHandle } from '@/next/features/pane/editor/file-editor';

interface IKept extends IDraft {
  path: string;
}

export interface IFileEdits {
  /** Tabs whose file differs from the disk: the editor on screen or the text it left. */
  unsaved: ReadonlySet<string>;
  /** The editor on screen. */
  live: { tab: string; saving: boolean } | null;
  /** The editor on screen hands over its save through this ref. */
  attach: (handle: IFileEditorHandle | null) => void;
  /** Saves the editor on screen; true when the disk has its text afterwards. */
  save: () => Promise<boolean>;
  draftOf: (tab: string, path: string) => IDraft | null;
  onState: (tab: string, state: { dirty: boolean; saving: boolean }) => void;
  onLeave: (tab: string, path: string, draft: IDraft | null) => void;
  /** The text of these tabs is given up: no draft stays, and an editor on screen does not hand its text over. */
  discard: (tabs: readonly string[]) => void;
}

export const useFileEdits = (): IFileEdits => {
  const kept = useRef(new Map<string, IKept>());
  const handle = useRef<IFileEditorHandle | null>(null);
  const given = useRef(new Set<string>());
  const [unsaved, setUnsaved] = useState<ReadonlySet<string>>(() => new Set());
  const [live, setLive] = useState<IFileEdits['live']>(null);

  const mark = (tab: string, on: boolean) =>
    setUnsaved((current) => {
      if (current.has(tab) === on) return current;
      const next = new Set(current);
      if (on) next.add(tab);
      else next.delete(tab);
      return next;
    });

  const draftOf = useCallback((tab: string, path: string) => {
    const draft = kept.current.get(tab);
    return draft?.path === path ? draft : null;
  }, []);

  return {
    unsaved,
    live,
    attach: (next) => {
      handle.current = next;
    },
    save: () => handle.current?.save() ?? Promise.resolve(false),
    draftOf,
    onState: (tab, state) => {
      mark(tab, state.dirty);
      setLive({ tab, saving: state.saving });
    },
    onLeave: (tab, path, draft) => {
      setLive((current) => (current?.tab === tab ? null : current));
      if (given.current.delete(tab) || draft === null) {
        kept.current.delete(tab);
        mark(tab, false);
        return;
      }
      kept.current.set(tab, { ...draft, path });
      mark(tab, true);
    },
    discard: (tabs) => {
      for (const tab of tabs) {
        kept.current.delete(tab);
        if (live?.tab === tab) given.current.add(tab);
      }
      setUnsaved((current) => {
        const next = new Set(current);
        for (const tab of tabs) next.delete(tab);
        return next;
      });
    },
  };
};
