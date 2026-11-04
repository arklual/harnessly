// Two states of a file side by side or as one text with the changes marked (.hx-diff): what git has and what the
// editor has now, or two files picked in «Файлы». Read only, with the editor's look and the file's language; long runs
// of unchanged lines fold.
import { MergeView, unifiedMergeView } from '@codemirror/merge';
import { EditorView } from '@codemirror/view';
import { useEffect, useRef } from 'react';
import { language, readOnlyLook } from '@/next/features/pane/editor/setup';
import { loadLanguage } from '@/next/features/pane/editor/language';

export const DIFF_LAYOUT = { split: 'split', inline: 'inline' } as const;
export type TDiffLayout = (typeof DIFF_LAYOUT)[keyof typeof DIFF_LAYOUT];

export interface IDiffViewProps {
  /** The file whose language colours both sides. */
  path: string;
  before: string;
  after: string;
  layout: TDiffLayout;
}

const COLLAPSE = { margin: 3, minSize: 6 };

const DiffView = ({ path, before, after, layout }: IDiffViewProps) => {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const parent = host.current;
    if (!parent) return undefined;
    let alive = true;
    const views: EditorView[] = [];
    let destroy: () => void;
    if (layout === DIFF_LAYOUT.split) {
      const merge = new MergeView({
        a: { doc: before, extensions: readOnlyLook() },
        b: { doc: after, extensions: readOnlyLook() },
        parent,
        collapseUnchanged: COLLAPSE,
        gutter: true,
      });
      views.push(merge.a, merge.b);
      destroy = () => merge.destroy();
    } else {
      const view = new EditorView({
        doc: after,
        parent,
        extensions: [
          readOnlyLook(),
          unifiedMergeView({ original: before, mergeControls: false, collapseUnchanged: COLLAPSE }),
        ],
      });
      views.push(view);
      destroy = () => view.destroy();
    }
    void loadLanguage(path).then((support) => {
      if (!alive || !support) return;
      for (const view of views) view.dispatch({ effects: language.reconfigure(support) });
    });
    return () => {
      alive = false;
      destroy();
    };
  }, [path, before, after, layout]);
  return <div ref={host} className="hx-diff" data-layout={layout} />;
};

export default DiffView;
