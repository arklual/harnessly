// The open file of a review and its diff's state, shared by the review screen of the pane and the «Ревью кода» app:
// which file is selected, its two versions loaded once per version (the patch stands in until they come), whether it is
// too heavy to draw until asked, and the walk over the changes with F7 that goes on to the next file.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { isHeavyContent, isHeavyFile } from '@/next/features/review/limits';
import type { IFileContent, IReviewFile, IReviewSource } from '@/next/features/review/model';
import { navigate, NAV_DIR, FILE_EDGE, type TFileEdge } from '@/next/features/review/navigation';
import { sidesFromPatch } from '@/next/features/review/patch';
import type { IReviewDiffHandle } from '@/next/features/review/review-diff';

export type TLoaded = { k: 'loading'; patch: IFileContent } | { k: 'ready'; content: IFileContent };
export type TStepDir = typeof NAV_DIR.next | typeof NAV_DIR.prev;

const contentKey = (file: IReviewFile) => `${file.path}\0${file.version}`;

export interface IReviewFileState {
  file: IReviewFile | null;
  selected: string | null;
  /** The loading or loaded versions of the open file; undefined before the ask. */
  entry: TLoaded | undefined;
  /** What the diff draws: the whole versions, or the patch while they load. */
  shown: IFileContent | null;
  /** Too big to draw until the person asks for it. */
  heavy: boolean;
  force: () => void;
  handle: { current: IReviewDiffHandle | null };
  onHandle: (next: IReviewDiffHandle, alive: boolean) => void;
  select: (target: IReviewFile, edge?: TFileEdge | null) => void;
  /** F7 / Shift+F7 over `order`: the next change, then the next file. */
  step: (dir: TStepDir, order: readonly IReviewFile[]) => void;
}

/**
 * `first` is the file selected when a change opens; `onSelect` hears every selection (the tree opens its folders).
 * A new `source.key` starts over.
 */
export const useReviewFile = (
  source: IReviewSource,
  first: string | null,
  onSelect?: (file: IReviewFile) => void,
): IReviewFileState => {
  const [selected, setSelected] = useState<string | null>(first);
  const [forced, setForced] = useState<ReadonlySet<string>>(() => new Set());
  const [loaded, setLoaded] = useState<ReadonlyMap<string, TLoaded>>(() => new Map());

  const [sourceKey, setSourceKey] = useState(source.key);
  if (sourceKey !== source.key) {
    setSourceKey(source.key);
    setSelected(first);
    setLoaded(new Map());
    setForced(new Set());
  }

  const file = useMemo(() => source.files.find((f) => f.path === selected) ?? null, [source.files, selected]);
  const heavyFile = file !== null && isHeavyFile(file) && !forced.has(file.path);

  const load = source.loadContent;
  useEffect(() => {
    if (!file || file.binary || heavyFile) return;
    const key = contentKey(file);
    if (loaded.has(key)) return;
    const patch = sidesFromPatch(file.patch);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the loading entry marks the request as made
    setLoaded((map) =>
      new Map(map).set(key, load ? { k: 'loading', patch } : { k: 'ready', content: patch }),
    );
    if (!load) return;
    void load(file).then((content) =>
      setLoaded((map) => new Map(map).set(key, { k: 'ready', content: content ?? patch })),
    );
  }, [file, heavyFile, loaded, load]);

  const entry = file ? loaded.get(contentKey(file)) : undefined;
  let shown: IFileContent | null = null;
  if (entry) shown = entry.k === 'ready' ? entry.content : entry.patch;
  const heavy =
    heavyFile || (shown !== null && file !== null && isHeavyContent(shown) && !forced.has(file.path));

  const handle = useRef<IReviewDiffHandle | null>(null);
  const pendingEdge = useRef<TFileEdge | null>(null);
  const onHandle = useCallback((next: IReviewDiffHandle, alive: boolean) => {
    if (!alive) {
      // A diff that goes away (another file, a full-window copy leaving) lets go of its own handle only.
      if (handle.current === next) handle.current = null;
      return;
    }
    handle.current = next;
    const edge = pendingEdge.current;
    if (!edge) return;
    const lines = next.changeLines();
    const line = edge === FILE_EDGE.first ? lines[0] : lines.at(-1);
    if (line !== undefined) next.goTo(line);
  }, []);

  const select = useCallback(
    (target: IReviewFile, edge: TFileEdge | null = null) => {
      pendingEdge.current = edge;
      setSelected(target.path);
      onSelect?.(target);
    },
    [onSelect],
  );

  const step = (dir: TStepDir, order: readonly IReviewFile[]) => {
    const current = handle.current;
    const target = navigate({
      changeLines: current?.changeLines() ?? [],
      cursor: current?.cursorLine() ?? 0,
      order,
      current: selected,
      dir,
    });
    if (!target) return;
    if (target.kind === 'line') {
      pendingEdge.current = null;
      current?.goTo(target.line);
      return;
    }
    const next = order.find((f) => f.path === target.path);
    if (next) select(next, target.edge);
  };

  const force = () => {
    if (file) setForced((set) => new Set([...set, file.path]));
  };

  return { file, selected, entry, shown, heavy, force, handle, onHandle, select, step };
};
