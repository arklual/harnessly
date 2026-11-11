// The changed files as a tree on the left of the review: folders with their sums, files with their status, their
// counts and a «viewed» mark, a filter by name on top. A folder folds with an animation; `badge` is where the count of
// a file's comments goes.
import { type ReactNode } from 'react';
import { FILE_STATUS, type IReviewFile, type TFileStatus } from '@/next/features/review/model';
import type { IDirNode, TTreeNode } from '@/next/features/review/tree';
import Icon from '@/next/ui/icon';
import { usePresentFlag, type TPresenceState } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const FOLD_MS = 160;

const STATUS_MARK: Record<TFileStatus, string> = {
  [FILE_STATUS.added]: 'A',
  [FILE_STATUS.deleted]: 'D',
  [FILE_STATUS.renamed]: 'R',
  [FILE_STATUS.modified]: 'M',
};

export const Counts = ({ add, del }: { add: number; del: number }) => (
  <span className="hx-review__counts">
    {add > 0 && <span className="hx-review__add">+{add}</span>}
    {del > 0 && <span className="hx-review__del">−{del}</span>}
  </span>
);

interface ITreeRowsProps {
  nodes: TTreeNode[];
  depth: number;
  open: ReadonlySet<string>;
  selected: string | null;
  onToggleDir: (path: string) => void;
  onSelect: (file: IReviewFile) => void;
  isViewed: (file: IReviewFile) => boolean;
  onViewed: (file: IReviewFile, viewed: boolean) => void;
  badge?: (file: IReviewFile) => ReactNode;
}

const DirChildren = (props: ITreeRowsProps & { shown: boolean }) => {
  const { mounted, state } = usePresentFlag(props.shown, FOLD_MS);
  if (!mounted) return null;
  return (
    <div className="hx-review__fold" data-state={state} role="group">
      <TreeRows {...props} />
    </div>
  );
};

const TreeRows = (props: ITreeRowsProps) => {
  const { nodes, depth, open, selected, onToggleDir, onSelect, isViewed, onViewed, badge } = props;
  return (
    <>
      {nodes.map((node) => {
        if (node.kind === 'dir') {
          const shown = open.has(node.path);
          return (
            <div key={`d:${node.path}`}>
              <button
                type="button"
                className="hx-review__row"
                data-kind="dir"
                data-depth={depth}
                aria-expanded={shown}
                onClick={() => onToggleDir(node.path)}
              >
                <Icon name="chevron-right" size="sm" className="hx-review__chev" />
                <Icon name={shown ? 'folder-open' : 'folder'} size="sm" />
                <span className="hx-review__name">{node.name}</span>
                <Counts add={node.additions} del={node.deletions} />
              </button>
              <DirChildren {...props} nodes={node.children} depth={depth + 1} shown={shown} />
            </div>
          );
        }
        const { file } = node;
        const viewed = isViewed(file);
        return (
          <div
            key={`f:${file.path}`}
            className="hx-review__row"
            data-kind="file"
            data-depth={depth}
            data-selected={file.path === selected || undefined}
            data-viewed={viewed || undefined}
          >
            <button
              type="button"
              className="hx-review__open"
              title={file.status === FILE_STATUS.renamed ? `${file.oldPath} → ${file.path}` : file.path}
              aria-current={file.path === selected ? 'true' : undefined}
              onClick={() => onSelect(file)}
            >
              <span className="hx-review__status" data-status={file.status}>
                {STATUS_MARK[file.status]}
              </span>
              <span className="hx-review__name">{node.name}</span>
              {badge?.(file)}
              <Counts add={file.additions} del={file.deletions} />
            </button>
            <button
              type="button"
              className="hx-review__viewed"
              aria-pressed={viewed}
              aria-label={t('next.review.viewed')}
              title={t('next.review.viewed')}
              onClick={() => onViewed(file, !viewed)}
            >
              <Icon name="check" size="sm" />
            </button>
          </div>
        );
      })}
    </>
  );
};

interface IReviewTreeProps extends Omit<ITreeRowsProps, 'nodes' | 'depth'> {
  root: IDirNode;
  filter: string;
  onFilter: (filter: string) => void;
  total: number;
  viewedCount: number;
  /** Open, or leaving after «hide the files». */
  state?: TPresenceState;
}

const ReviewTree = ({ root, filter, onFilter, total, viewedCount, state, ...rows }: IReviewTreeProps) => (
  <nav className="hx-review__tree" data-state={state} aria-label={t('next.review.files')}>
    <label className="hx-review__filter">
      <Icon name="search" size="sm" />
      <input
        type="search"
        value={filter}
        placeholder={t('next.review.filter')}
        aria-label={t('next.review.filter')}
        onChange={(event) => onFilter(event.target.value)}
      />
    </label>
    <p className="hx-review__progress">{t('next.review.viewedOf', { n: viewedCount, total })}</p>
    <div className="hx-review__rows">
      {root.children.length ? (
        <TreeRows nodes={root.children} depth={0} {...rows} />
      ) : (
        <p className="hx-review__empty">{t('next.review.noMatch')}</p>
      )}
    </div>
  </nav>
);

export default ReviewTree;
