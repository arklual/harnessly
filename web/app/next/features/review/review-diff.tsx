// One file's diff in the review: two columns that scroll together, or one text with the removed lines above the added
// ones; the file's language colours both; changes inside a line are marked; long unchanged runs fold into a row that
// opens on a click, unless the whole file is asked for. A click on a line number selects the line and offers its link.
// The screen drives it through a handle (F7 walks the changes); `extensions` is where a layer of comments on lines
// plugs in.
import { getChunks, MergeView, unifiedMergeView, type Chunk } from '@codemirror/merge';
import { EditorSelection, type Extension } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { useEffect, useRef } from 'react';
import { DIFF_LAYOUT, type TDiffLayout } from '@/next/features/pane/editor/diff-view';
import { loadLanguage } from '@/next/features/pane/editor/language';
import { language, readOnlyLook } from '@/next/features/pane/editor/setup';
import { LINE_SIDE, type IFileContent, type TLineSide } from '@/next/features/review/model';
import { ignoreWhitespaceDiff } from '@/next/features/review/whitespace';

const COLLAPSE = { margin: 3, minSize: 6 };

export interface IReviewDiffHandle {
  /** The first line (in the new version) of every change, in order. */
  changeLines: () => number[];
  /** The line the person is on: 0 until they click into the file or F7 moves there. */
  cursorLine: () => number;
  /** Puts the cursor on a line of the new version and scrolls it to the middle. */
  goTo: (line: number) => void;
}

export interface IReviewDiffProps {
  path: string;
  content: IFileContent;
  layout: TDiffLayout;
  ignoreWhitespace: boolean;
  /** The whole file, nothing folded. */
  full: boolean;
  /** The handle when the diff is drawn (alive) and again when it goes away. */
  onHandle: (handle: IReviewDiffHandle, alive: boolean) => void;
  /** A click on a line number. */
  onLine?: (line: number, side: TLineSide) => void;
  /** More editor extensions per side (the layer of comments). */
  extensions?: (side: TLineSide) => Extension[];
}

const firstLines = (chunks: readonly Chunk[], view: EditorView): number[] =>
  chunks.map((chunk) => view.state.doc.lineAt(Math.min(chunk.fromB, view.state.doc.length)).number);

const ReviewDiff = ({
  path,
  content,
  layout,
  ignoreWhitespace,
  full,
  onHandle,
  onLine,
  extensions,
}: IReviewDiffProps) => {
  const host = useRef<HTMLDivElement>(null);
  // The latest callbacks, read by handlers made once per view.
  const onLineRef = useRef(onLine);
  const onHandleRef = useRef(onHandle);
  const extensionsRef = useRef(extensions);
  useEffect(() => {
    onLineRef.current = onLine;
    onHandleRef.current = onHandle;
    extensionsRef.current = extensions;
  });

  useEffect(() => {
    const parent = host.current;
    if (!parent) return undefined;
    let alive = true;
    let cursor = 0;
    const track = EditorView.updateListener.of((update) => {
      if (update.selectionSet && update.transactions.some((tr) => tr.isUserEvent('select')))
        cursor = update.state.doc.lineAt(update.state.selection.main.head).number;
    });
    const diffConfig = ignoreWhitespace ? { override: ignoreWhitespaceDiff } : undefined;
    const collapseUnchanged = full ? undefined : COLLAPSE;
    const more = (side: TLineSide) => extensionsRef.current?.(side) ?? [];
    let newer: EditorView;
    let views: EditorView[];
    let sideOf: (view: EditorView) => TLineSide;
    let chunks: () => readonly Chunk[];
    let destroy: () => void;
    if (layout === DIFF_LAYOUT.split) {
      const merge = new MergeView({
        a: { doc: content.before, extensions: [readOnlyLook(), more(LINE_SIDE.before)] },
        b: { doc: content.after, extensions: [readOnlyLook(), track, more(LINE_SIDE.after)] },
        parent,
        collapseUnchanged,
        diffConfig,
        gutter: true,
      });
      newer = merge.b;
      views = [merge.a, merge.b];
      sideOf = (view) => (view === merge.a ? LINE_SIDE.before : LINE_SIDE.after);
      chunks = () => merge.chunks;
      destroy = () => merge.destroy();
    } else {
      const view = new EditorView({
        doc: content.after,
        parent,
        extensions: [
          readOnlyLook(),
          track,
          unifiedMergeView({
            original: content.before,
            mergeControls: false,
            collapseUnchanged,
            diffConfig,
            gutter: true,
          }),
          more(LINE_SIDE.after),
        ],
      });
      newer = view;
      views = [view];
      sideOf = () => LINE_SIDE.after;
      chunks = () => getChunks(view.state)?.chunks ?? [];
      destroy = () => view.destroy();
    }

    const onGutterClick = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      const number = target?.closest('.cm-lineNumbers .cm-gutterElement');
      const editor = target?.closest('.cm-editor');
      if (!number || !editor) return;
      const view = EditorView.findFromDOM(editor as HTMLElement);
      if (!view) return;
      const block = view.lineBlockAtHeight(event.clientY - view.documentTop);
      const line = view.state.doc.lineAt(block.from);
      view.dispatch({ selection: EditorSelection.range(line.from, line.to), userEvent: 'select' });
      onLineRef.current?.(line.number, sideOf(view));
    };
    parent.addEventListener('click', onGutterClick);

    const handle: IReviewDiffHandle = {
      changeLines: () => firstLines(chunks(), newer),
      cursorLine: () => cursor,
      goTo: (line) => {
        const doc = newer.state.doc;
        const at = doc.line(Math.max(1, Math.min(line, doc.lines))).from;
        cursor = line;
        newer.dispatch({
          selection: { anchor: at },
          effects: EditorView.scrollIntoView(at, { y: 'center' }),
        });
      },
    };
    onHandleRef.current(handle, true);

    void loadLanguage(path).then((support) => {
      if (!alive || !support) return;
      for (const view of views) view.dispatch({ effects: language.reconfigure(support) });
    });
    return () => {
      alive = false;
      parent.removeEventListener('click', onGutterClick);
      onHandleRef.current(handle, false);
      destroy();
    };
  }, [path, content, layout, ignoreWhitespace, full]);

  return <div ref={host} className="hx-diff hx-review__diff" data-layout={layout} />;
};

export default ReviewDiff;
