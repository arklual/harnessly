// The review screen: the change's head, the tree of changed files on the left and one file's diff in the middle. The
// same for a merge request, a commit and a chat's local work (sources.ts). A file's whole versions load when it is
// opened; until they come, or when the source cannot give them, the diff is drawn from the patch. F7 / Shift+F7 walk
// the changes and go on to the next file; the screen can take the whole window and come back to the pane.
// `commentLayer` and `fileBadge` are where comments on lines plug in.
import type { Extension } from '@codemirror/state';
import { useCallback, useMemo, useState, type KeyboardEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { DIFF_LAYOUT, type TDiffLayout } from '@/next/features/pane/editor/diff-view';
import { autoExpandTree } from '@/next/features/review/limits';
import {
  LINE_SIDE,
  type IReviewFile,
  type IReviewSource,
  type TLineSide,
} from '@/next/features/review/model';
import { NAV_DIR } from '@/next/features/review/navigation';
import { setReviewPrefs, useReviewPrefs } from '@/next/features/review/prefs';
import ReviewDiff from '@/next/features/review/review-diff';
import ReviewHeader from '@/next/features/review/review-header';
import ReviewTree, { Counts } from '@/next/features/review/review-tree';
import { ancestorsOf, buildTree, dirPaths, treeOrder, type IDirNode } from '@/next/features/review/tree';
import { useReviewFile, type TStepDir } from '@/next/features/review/use-review-file';
import { useViewed } from '@/next/features/review/viewed';
import { personKey, useMe } from '@/next/stores/me';
import { Button, RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresentFlag } from '@/next/ui/presence';
import Segmented from '@/next/ui/segmented';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';
import '@/next/styles/editor.css';
import '@/next/styles/review.css';

const FULL_EXIT_MS = 200;
const TREE_EXIT_MS = 160;
const LOCAL_PERSON = 'local';

export interface IReviewScreenProps {
  source: IReviewSource;
  /** Buttons in the head (stage actions: send to the agent, approve). */
  actions?: ReactNode;
  /** What goes under the head: a description, checks, a discussion. */
  intro?: ReactNode;
  /** Editor extensions per file and side: the layer of comments on lines. */
  commentLayer?: (file: IReviewFile, side: TLineSide) => Extension[];
  /** A mark next to a file in the tree: its comments. */
  fileBadge?: (file: IReviewFile) => ReactNode;
}

const scopeOf = (source: IReviewSource): string => {
  const { meta } = source;
  if (meta.kind === 'local') return meta.root;
  return meta.project;
};

const copy = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text);
    toast(t('next.review.linkCopied'), { description: text, tone: 'success' });
  } catch {
    toast(t('next.review.copyFailed'), { description: text, tone: 'warning' });
  }
};

/** The folders open at first: all of them, or past the limit only those above the first file. */
const initialOpen = (tree: IDirNode, files: number, first: string | null): ReadonlySet<string> => {
  if (autoExpandTree(files)) return new Set(dirPaths(tree));
  return new Set(first ? ancestorsOf(tree, first) : []);
};

const ReviewScreen = ({ source, actions, intro, commentLayer, fileBadge }: IReviewScreenProps) => {
  const prefs = useReviewPrefs();
  const me = useMe();
  const person = me.k === 'ready' ? personKey(me.me) : LOCAL_PERSON;
  const { viewed, toggle } = useViewed(person, scopeOf(source));

  const [filter, setFilter] = useState('');
  const [full, setFull] = useState(false);
  const [whole, setWhole] = useState(false);
  const [treeShown, setTreeShown] = useState(true);

  const allTree = useMemo(() => buildTree(source.files), [source.files]);
  const order = useMemo(() => treeOrder(allTree), [allTree]);
  const tree = useMemo(() => buildTree(source.files, filter), [source.files, filter]);
  const visibleOrder = useMemo(() => treeOrder(tree), [tree]);

  const first = order[0]?.path ?? null;
  const [open, setOpen] = useState<ReadonlySet<string>>(() =>
    initialOpen(allTree, source.files.length, first),
  );
  const openAt = useCallback(
    (target: IReviewFile) => setOpen((set) => new Set([...set, ...ancestorsOf(allTree, target.path)])),
    [allTree],
  );
  const {
    file,
    selected,
    entry,
    shown,
    heavy,
    force,
    handle,
    onHandle,
    select,
    step: walk,
  } = useReviewFile(source, first, openAt);

  // A new change starts over: its first file's folders, no filter.
  const [sourceKey, setSourceKey] = useState(source.key);
  if (sourceKey !== source.key) {
    setSourceKey(source.key);
    setOpen(initialOpen(allTree, source.files.length, first));
    setFilter('');
  }

  const step = (dir: TStepDir) => walk(dir, visibleOrder);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'F7') {
      event.preventDefault();
      step(event.shiftKey ? NAV_DIR.prev : NAV_DIR.next);
    } else if (event.key === 'Escape' && full) {
      event.preventDefault();
      setFull(false);
    }
  };

  const onLine = useCallback(
    (line: number, side: TLineSide) => {
      if (file && source.lineLink) void copy(source.lineLink(file, line, side));
    },
    [file, source],
  );

  const extensions = useMemo(
    () => (file && commentLayer ? (side: TLineSide) => commentLayer(file, side) : undefined),
    [file, commentLayer],
  );

  const size = useMemo(
    () => ({
      additions: source.files.reduce((s, f) => s + f.additions, 0),
      deletions: source.files.reduce((s, f) => s + f.deletions, 0),
      files: source.files.length,
      truncated: source.truncated,
    }),
    [source.files, source.truncated],
  );
  const viewedCount = source.files.filter(viewed).length;

  const treePresence = usePresentFlag(treeShown, TREE_EXIT_MS);
  const portal = usePortalContainer();
  const { mounted: fullMounted, state: fullState } = usePresentFlag(full, FULL_EXIT_MS);

  const layoutOptions = [
    { value: DIFF_LAYOUT.split, label: t('next.review.split') },
    { value: DIFF_LAYOUT.inline, label: t('next.review.inline') },
  ] as const;

  const toolbar = file && (
    <div className="hx-review__bar">
      <RoundButton
        icon="panel-left"
        label={treeShown ? t('next.review.hideTree') : t('next.review.showTree')}
        size="sm"
        variant="ghost"
        aria-pressed={treeShown}
        onClick={() => setTreeShown(!treeShown)}
      />
      <span className="hx-review__path" title={file.path}>
        {file.path}
      </span>
      <Counts add={file.additions} del={file.deletions} />
      {entry?.k === 'loading' && <span className="hx-review__muted">{t('next.review.loading')}</span>}
      {entry?.k === 'ready' && !entry.content.complete && (
        <span className="hx-review__muted" title={t('next.review.patchOnlyHint')}>
          {t('next.review.patchOnly')}
        </span>
      )}
      <span className="hx-review__spacer" />
      <Segmented<TDiffLayout>
        label={t('next.review.layout')}
        value={prefs.layout}
        options={layoutOptions}
        onChange={(layout) => setReviewPrefs({ layout })}
      />
      <RoundButton
        icon="pilcrow"
        label={t('next.review.ignoreWhitespace')}
        size="sm"
        variant={prefs.ignoreWhitespace ? 'accent' : 'ghost'}
        aria-pressed={prefs.ignoreWhitespace}
        onClick={() => setReviewPrefs({ ignoreWhitespace: !prefs.ignoreWhitespace })}
      />
      <RoundButton
        icon="fold-vertical"
        label={whole ? t('next.review.foldUnchanged') : t('next.review.wholeFile')}
        size="sm"
        variant={whole ? 'accent' : 'ghost'}
        aria-pressed={whole}
        onClick={() => setWhole(!whole)}
      />
      <RoundButton
        icon="arrow-up"
        label={t('next.review.prevChange')}
        keys={['⇧', 'F7']}
        size="sm"
        variant="ghost"
        onClick={() => step(NAV_DIR.prev)}
      />
      <RoundButton
        icon="arrow-down"
        label={t('next.review.nextChange')}
        keys={['F7']}
        size="sm"
        variant="ghost"
        onClick={() => step(NAV_DIR.next)}
      />
      {source.lineLink && (
        <RoundButton
          icon="link"
          label={t('next.review.copyLineLink')}
          size="sm"
          variant="ghost"
          onClick={() => {
            const line = handle.current?.cursorLine() ?? 0;
            if (line > 0) onLine(line, LINE_SIDE.after);
            else toast(t('next.review.pickLine'));
          }}
        />
      )}
      <RoundButton
        icon={full ? 'minimize-2' : 'maximize-2'}
        label={full ? t('next.review.exitFull') : t('next.review.full')}
        keys={full ? ['Esc'] : undefined}
        size="sm"
        variant="ghost"
        onClick={() => setFull(!full)}
      />
    </div>
  );

  const body = (() => {
    if (!file) return <p className="hx-review__empty">{t('next.review.noFiles')}</p>;
    if (file.binary) return <p className="hx-review__empty">{t('next.review.binary')}</p>;
    if (heavy)
      return (
        <div className="hx-review__empty">
          <p>{t('next.review.heavy', { n: file.additions + file.deletions })}</p>
          <Button size="sm" onClick={force}>
            {t('next.review.showAnyway')}
          </Button>
        </div>
      );
    if (!shown) return <p className="hx-review__empty">{t('next.review.loading')}</p>;
    return (
      <ReviewDiff
        path={file.path}
        content={shown}
        layout={prefs.layout}
        ignoreWhitespace={prefs.ignoreWhitespace}
        full={whole}
        onHandle={onHandle}
        onLine={source.lineLink ? onLine : undefined}
        extensions={extensions}
      />
    );
  })();

  const frame = (mode: 'pane' | 'full') => (
    <div
      className="hx-review"
      // Focus stays in the screen when the read-only diff is clicked, so F7 reaches it.
      tabIndex={-1}
      data-mode={mode}
      data-state={mode === 'full' ? fullState : undefined}
      data-tree={treeShown ? 'shown' : 'hidden'}
      onKeyDown={onKeyDown}
    >
      <ReviewHeader meta={source.meta} size={size} actions={actions} />
      {intro && <div className="hx-review__intro">{intro}</div>}
      {source.files.length > 0 ? (
        <div className="hx-review__body">
          {treePresence.mounted && (
            <ReviewTree
              state={treePresence.state}
              root={tree}
              filter={filter}
              onFilter={setFilter}
              total={source.files.length}
              viewedCount={viewedCount}
              open={open}
              selected={selected}
              onToggleDir={(path) =>
                setOpen((set) => {
                  const next = new Set(set);
                  if (!next.delete(path)) next.add(path);
                  return next;
                })
              }
              onSelect={(target) => select(target)}
              isViewed={viewed}
              onViewed={toggle}
              badge={fileBadge}
            />
          )}
          <section className="hx-review__main" aria-label={t('next.review.diff')}>
            {toolbar}
            {body}
          </section>
        </div>
      ) : (
        <p className="hx-review__empty">
          <Icon name="git-compare" /> {t('next.review.noFiles')}
        </p>
      )}
    </div>
  );

  return (
    <>
      {!full && frame('pane')}
      {fullMounted && createPortal(frame('full'), portal?.current ?? document.body)}
    </>
  );
};

export default ReviewScreen;
