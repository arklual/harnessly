// An open change in the «Ревью кода» app (board «Главная — приложение «Ревью кода»»): a thin bar over the diff — the
// file's path, ‹ › to the files around it, «Просмотрено», «Вид» — the diff in the whole height under it, and at its foot
// «Отметить просмотренным и дальше». On the right, the «Ревью» panel: what the change is, how much of it is viewed, its
// files and its discussions. The files are walked tests first and lock files last (order.ts). On a narrow screen the
// panel slides over the diff and is called by the bar's button.
import { Menu } from '@base-ui/react/menu';
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react';
import type { Extension } from '@codemirror/state';
import { DIFF_LAYOUT } from '@/next/features/pane/editor/diff-view';
import { autoExpandTree } from '@/next/features/review/limits';
import {
  FILE_STATUS,
  LINE_SIDE,
  type IReviewFile,
  type IReviewSource,
  type IReviewThread,
  type TLineSide,
} from '@/next/features/review/model';
import { NAV_DIR } from '@/next/features/review/navigation';
import { newNoteId, linesOf, NOTE_STATE, placeNote } from '@/next/features/review/app/notes';
import {
  applyReplacement,
  gitlabSuggestion,
  reviewRequest,
  type IAgentSuggestion,
} from '@/next/features/review/app/agent-review';
import {
  AgentPanel,
  DiffSuggestions,
  type ISuggestionActions,
} from '@/next/features/review/app/agent-suggestions';
import type { IAgentReviewApi } from '@/next/features/review/app/use-agent-review';
import { CommitBox, FileEdit, type IReviewEdit } from '@/next/features/review/app/file-edit';
import { DefinitionPeek, useDefinitions } from '@/next/features/review/app/definition-peek';
import { NotesLayer } from '@/next/features/review/app/notes-layer';
import { DiffNotes, NotesFooter, NotesList } from '@/next/features/review/app/review-notes';
import {
  MrDiffThreads,
  MrFooter,
  MrThreadsPanel,
  type IMrContext,
} from '@/next/features/review/app/mr-threads';
import { draftAt, openThreadsByFile, type IMrThread } from '@/next/features/review/app/mr-review';
import type { IReviewNotesApi } from '@/next/features/review/app/use-review-notes';
import { neighbour, nextUnviewed, reviewOrder } from '@/next/features/review/app/order';
import { setReviewPrefs, useReviewPrefs } from '@/next/features/review/prefs';
import ReviewDiff from '@/next/features/review/review-diff';
import ReviewHeader from '@/next/features/review/review-header';
import ReviewTree from '@/next/features/review/review-tree';
import { ancestorsOf, buildTree, dirPaths, treeOrder } from '@/next/features/review/tree';
import { useReviewFile } from '@/next/features/review/use-review-file';
import { useViewed } from '@/next/features/review/viewed';
import { personKey, useMe } from '@/next/stores/me';
import { Button, RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { usePresentFlag } from '@/next/ui/presence';
import Segmented from '@/next/ui/segmented';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';
import '@/next/styles/editor.css';

const PANEL_EXIT_MS = 200;
const LOCAL_PERSON = 'local';
/** Narrower than this, the panel is a sheet over the diff (as the container query of review-app.css). */
const SHEET_BELOW = 900;
/** Narrower than this, two columns do not fit: the diff is one text whatever the choice in «Вид». */
const SPLIT_FROM = 640;

type TPanelTab = 'overview' | 'threads';

const BOTH_SIDES = [LINE_SIDE.after, LINE_SIDE.before] as const;
const NEW_SIDE = [LINE_SIDE.after] as const;

export interface IReviewAppProps {
  source: IReviewSource;
  /** The discussions of a merge request, read only for now; null when there are none to show. */
  threads?: readonly IReviewThread[] | null;
  /** Over the head of the panel: which worktree of a chat's work is reviewed. */
  pick?: ReactNode;
  /** The panel's foot: the actions on the change. */
  footer?: ReactNode;
  /** The person's remarks for the agent (a chat's work): drawn on the lines and sent from the panel's foot. */
  notes?: IReviewNotesApi | null;
  /** The review of a merge request in GitLab: its discussions at the lines, the draft review, approve. */
  mr?: IMrContext | null;
  /** The worktree reviewed, kept with each remark. */
  worktree?: string;
  /** Next to the title in the panel: the link out to the forge. */
  actions?: ReactNode;
  /** The person's own hand in the worktree: the new side edited and saved, the changes committed. */
  edit?: IReviewEdit | null;
  /** «Пройти ревью»: the agent beside goes through the change and its suggestions lie at the lines. */
  agent?: IAgentReviewApi | null;
  /** In a pane: opens the change in the «Ревью кода» app on the whole screen. */
  expand?: () => void;
}

const scopeOf = (source: IReviewSource): string =>
  source.meta.kind === 'local' ? source.meta.root : source.meta.project;

/** A new or deleted file has one side: its text is shown in one column. */
const oneSide = (file: IReviewFile) =>
  file.status === FILE_STATUS.added || file.status === FILE_STATUS.deleted;

const nameOf = (path: string) => path.split('/').at(-1) ?? path;
const dirOf = (path: string) => {
  const at = path.lastIndexOf('/');
  return at < 0 ? '' : path.slice(0, at + 1);
};

const copy = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text);
    toast(t('next.review.linkCopied'), { description: text, tone: 'success' });
  } catch {
    toast(t('next.review.copyFailed'), { description: text, tone: 'warning' });
  }
};

/** The share of viewed files as a bar: the width goes to the stylesheet as a variable. */
const Progress = ({ n, total }: { n: number; total: number }) => {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--rv-done', String(total > 0 ? n / total : 0));
  }, [n, total]);
  return (
    <div className="hx-rvapp__progress">
      <div ref={ref} className="hx-rvapp__meter" aria-hidden="true" />
      <span>{t('next.review.viewedOf', { n, total })}</span>
    </div>
  );
};

const ViewMenu = ({ whole, onWhole }: { whole: boolean; onWhole: (whole: boolean) => void }) => {
  const prefs = useReviewPrefs();
  const actions = useMenuActions();
  const item = (checked: boolean, label: string, onChange: (on: boolean) => void) => (
    <Menu.CheckboxItem
      className="hx-cmenu__item"
      checked={checked}
      closeOnClick={false}
      onCheckedChange={onChange}
    >
      <span className="hx-cmenu__txt">{label}</span>
      <Menu.CheckboxItemIndicator className="hx-rvapp__tick" keepMounted>
        <Icon name="check" size="sm" />
      </Menu.CheckboxItemIndicator>
    </Menu.CheckboxItem>
  );
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger className="hx-rvapp__barbtn">
        <Icon name="sliders-horizontal" size="sm" />
        <span className="hx-rvapp__barlabel">{t('next.reviewApp.view')}</span>
      </Menu.Trigger>
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        label={t('next.reviewApp.view')}
        side="bottom"
        align="end"
      >
        {item(prefs.layout === DIFF_LAYOUT.split, t('next.review.split'), (on) =>
          setReviewPrefs({ layout: on ? DIFF_LAYOUT.split : DIFF_LAYOUT.inline }),
        )}
        {item(prefs.ignoreWhitespace, t('next.review.ignoreWhitespace'), (on) =>
          setReviewPrefs({ ignoreWhitespace: on }),
        )}
        {item(whole, t('next.review.wholeFile'), onWhole)}
      </MenuPanel>
    </Menu.Root>
  );
};

const Threads = ({
  threads,
  files,
  onOpen,
}: {
  threads: readonly IReviewThread[];
  files: readonly IReviewFile[];
  onOpen: (file: IReviewFile, line: number | null) => void;
}) => (
  <ol className="hx-rvapp__threads">
    {threads.map((thread) => {
      const head = thread.notes[0];
      const file = thread.path ? files.find((f) => f.path === thread.path) : undefined;
      const body = (
        <>
          <span className="hx-rvapp__thread-head">
            <span className="hx-rvapp__thread-who">{head?.author ?? t('next.reviewApp.someone')}</span>
            {thread.path && (
              <code className="hx-rvapp__thread-at">
                {nameOf(thread.path)}
                {thread.line ? `:${thread.line}` : ''}
              </code>
            )}
            {thread.resolved && <span className="hx-rvapp__thread-tag">{t('next.reviewApp.resolved')}</span>}
            {thread.outdated && <span className="hx-rvapp__thread-tag">{t('next.reviewApp.outdated')}</span>}
          </span>
          <span className="hx-rvapp__thread-text">{head?.body ?? ''}</span>
          {thread.notes.length > 1 && (
            <span className="hx-rvapp__thread-more">
              {t('next.reviewApp.replies', { n: thread.notes.length - 1 })}
            </span>
          )}
        </>
      );
      return (
        <li key={thread.id} data-resolved={thread.resolved || undefined}>
          {file ? (
            <button type="button" className="hx-rvapp__thread" onClick={() => onOpen(file, thread.line)}>
              {body}
            </button>
          ) : (
            <div className="hx-rvapp__thread">{body}</div>
          )}
        </li>
      );
    })}
  </ol>
);

/** Where the change is, for the agent asked to go through it: the merge request's link, or the worktree. */
const whereOf = (source: IReviewSource): string => {
  if (source.meta.kind === 'local') return source.meta.root;
  return source.meta.url ?? source.meta.project;
};

const ReviewApp = ({
  source,
  threads,
  pick,
  footer,
  actions,
  notes,
  mr,
  worktree,
  edit,
  agent,
  expand,
}: IReviewAppProps) => {
  const prefs = useReviewPrefs();
  const me = useMe();
  const person = me.k === 'ready' ? personKey(me.me) : LOCAL_PERSON;
  const { viewed, toggle } = useViewed(person, scopeOf(source));

  const [filter, setFilter] = useState('');
  const [whole, setWhole] = useState(false);
  const [tab, setTab] = useState<TPanelTab>('overview');
  // The panel is open on a wide screen and called on a narrow one; the choice is per visit.
  const [panel, setPanel] = useState(() => window.innerWidth >= SHEET_BELOW);

  const [since, setSince] = useState(false);
  const fresh = useMemo(() => source.files.filter((f) => !viewed(f)), [source.files, viewed]);
  const sinceOn = since && fresh.length > 0 && fresh.length < source.files.length;
  const allTree = useMemo(() => buildTree(source.files), [source.files]);
  const tree = useMemo(
    () => buildTree(sinceOn ? fresh : source.files, filter),
    [source.files, fresh, sinceOn, filter],
  );
  const order = useMemo(() => reviewOrder(treeOrder(allTree)), [allTree]);
  const first = order[0]?.path ?? null;
  const initialOpen = useCallback(
    (path: string | null): ReadonlySet<string> =>
      autoExpandTree(source.files.length)
        ? new Set(dirPaths(allTree))
        : new Set(path ? ancestorsOf(allTree, path) : []),
    [allTree, source.files.length],
  );
  const [open, setOpen] = useState<ReadonlySet<string>>(() => initialOpen(first));
  const openAt = useCallback(
    (target: IReviewFile) => setOpen((set) => new Set([...set, ...ancestorsOf(allTree, target.path)])),
    [allTree],
  );
  const { file, selected, entry, shown, heavy, force, handle, onHandle, select, step } = useReviewFile(
    source,
    first,
    openAt,
  );

  const defs = useDefinitions(source, file);
  const [editing, setEditing] = useState<string | null>(null);
  const [commitOpen, setCommitOpen] = useState(false);

  const [sourceKey, setSourceKey] = useState(source.key);
  if (sourceKey !== source.key) {
    setSourceKey(source.key);
    setOpen(initialOpen(first));
    setFilter('');
  }

  const narrow = () => window.innerWidth < SHEET_BELOW;
  const go = (target: IReviewFile | null, line: number | null = null) => {
    if (!target) return;
    select(target);
    if (line) requestAnimationFrame(() => handle.current?.goTo(line));
    if (narrow()) setPanel(false);
  };

  const prev = neighbour(order, selected, -1);
  const next = neighbour(order, selected, 1);
  const isViewed = file ? viewed(file) : false;
  const upNext = nextUnviewed(order, selected, viewed);
  const viewedCount = source.files.filter(viewed).length;
  const size = useMemo(
    () => ({
      additions: source.files.reduce((s, f) => s + f.additions, 0),
      deletions: source.files.reduce((s, f) => s + f.deletions, 0),
      files: source.files.length,
      truncated: source.truncated,
    }),
    [source.files, source.truncated],
  );

  const markAndNext = () => {
    if (file && !isViewed) toggle(file, true);
    if (upNext) go(upNext);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'F7') {
      event.preventDefault();
      step(event.shiftKey ? NAV_DIR.prev : NAV_DIR.next, order);
    } else if (event.key === 'Escape' && panel && narrow()) {
      event.preventDefault();
      setPanel(false);
    }
  };

  const onLine = useCallback(
    (line: number, side: TLineSide) => {
      if (file && source.lineLink) void copy(source.lineLink(file, line, side));
    },
    [file, source],
  );

  const [layer] = useState(() => new NotesLayer());
  useEffect(() => () => layer.dispose(), [layer]);
  const noted = notes?.state.k === 'ready' ? notes.state.notes : null;
  const withLayer = Boolean(notes || mr || agent);
  const defExtension = defs.extension;
  const layerOf = useCallback(
    (side: TLineSide): Extension[] => [...(withLayer ? [layer.extension(side)] : []), defExtension()],
    [layer, withLayer, defExtension],
  );

  const savedToast = () =>
    toast(t('next.reviewApp.edit.saved'), {
      tone: 'success',
      action: {
        label: t('next.reviewApp.edit.commit'),
        onClick: () => {
          setPanel(true);
          setCommitOpen(true);
        },
      },
    });

  const takeToDraft = async (s: IAgentSuggestion): Promise<boolean> => {
    const target = source.files.find((f) => f.path === s.path);
    if (!target) return false;
    if (notes) {
      const lines = shown && file?.path === s.path ? linesOf(shown[s.side].split('\n'), s.from, s.to) : '';
      const fence = '```';
      return notes.save({
        id: newNoteId(),
        path: s.path,
        side: s.side,
        from: s.from,
        to: s.to,
        lines,
        text: s.replacement === undefined ? s.text : `${s.text}\n\n${fence}\n${s.replacement}\n${fence}`,
        state: NOTE_STATE.draft,
        createdAt: 0,
        updatedAt: 0,
        ...(worktree ? { worktree } : {}),
      });
    }
    if (!mr) return false;
    const body = s.side === LINE_SIDE.after ? gitlabSuggestion(s) : s.text;
    return (await mr.mr.act(draftAt(target, s.side, s.to, mr.review.refs, body))) === null;
  };
  const applyEdit = async (s: IAgentSuggestion): Promise<boolean> => {
    const target = source.files.find((f) => f.path === s.path);
    if (!edit || !target || s.replacement === undefined || s.side !== LINE_SIDE.after || !source.loadContent)
      return false;
    const content = await source.loadContent(target);
    if (!content?.complete) return false;
    try {
      const next = applyReplacement(content.after, s.from, s.to, s.replacement);
      await edit.save(s.path, next, await edit.hash(content.after), false);
    } catch {
      return false;
    }
    edit.reload();
    savedToast();
    return true;
  };
  const suggestionActions: ISuggestionActions = {
    ...(notes || mr?.writable ? { take: takeToDraft } : {}),
    ...(edit ? { apply: applyEdit } : {}),
    takeLabel: (s) =>
      mr && s.replacement !== undefined && s.side === LINE_SIDE.after
        ? t('next.reviewApp.agent.takeWithEdit')
        : t('next.reviewApp.agent.take'),
  };
  const askAgent = () =>
    agent?.ask(
      reviewRequest(source.files, whereOf(source), {
        ask: t('next.reviewApp.agent.request'),
        format: t('next.reviewApp.agent.format'),
      }),
    );
  const canEdit =
    Boolean(edit) &&
    file !== null &&
    !file.binary &&
    file.status !== FILE_STATUS.deleted &&
    entry?.k === 'ready' &&
    entry.content.complete;
  const commitPaths = useMemo(
    () => [
      ...new Set(
        source.files.flatMap((f) => (f.oldPath && f.oldPath !== f.path ? [f.path, f.oldPath] : [f.path])),
      ),
    ],
    [source.files],
  );
  const commitable = Boolean(edit) && source.meta.kind === 'local' && source.meta.uncommitted;
  const placeOf = useCallback(
    (note: { path: string; side: TLineSide; from: number; to: number; lines: string }) =>
      file && shown && note.path === file.path ? placeNote(note, shown[note.side].split('\n')) : undefined,
    [file, shown],
  );

  const sheet = usePresentFlag(panel, PANEL_EXIT_MS);
  const threadCount = mr ? mr.review.threads.length : (threads?.length ?? 0);
  const openByFile = useMemo(() => (mr ? openThreadsByFile(mr.review) : null), [mr]);
  const badge = openByFile
    ? (f: IReviewFile) => {
        const n = openByFile.get(f.path);
        return n ? (
          <span className="hx-rvmr__badge" title={t('next.reviewApp.mr.openThreads', { n })}>
            <Icon name="message-square" size="sm" />
            {n}
          </span>
        ) : null;
      }
    : undefined;
  const inDiff = (thread: IMrThread) =>
    Boolean(thread.path && thread.line && !thread.outdated) &&
    (thread.side === LINE_SIDE.after || prefs.layout === DIFF_LAYOUT.split);
  const tabs = [
    { value: 'overview' as const, label: t('next.reviewApp.overview') },
    {
      value: 'threads' as const,
      label: threadCount ? `${t('next.reviewApp.threads')} ${threadCount}` : t('next.reviewApp.threads'),
    },
  ];

  const body = (() => {
    if (!file) return <p className="hx-rvapp__empty">{t('next.review.noFiles')}</p>;
    if (file.binary) return <p className="hx-rvapp__empty">{t('next.review.binary')}</p>;
    if (heavy)
      return (
        <div className="hx-rvapp__empty">
          <Icon name="file-code" />
          <p>{t('next.review.heavy', { n: file.additions + file.deletions })}</p>
          <Button size="sm" onClick={force}>
            {t('next.review.showAnyway')}
          </Button>
        </div>
      );
    if (!shown) return <p className="hx-rvapp__empty">{t('next.review.loading')}</p>;
    if (edit && editing === file.path)
      return (
        <FileEdit
          key={file.path}
          path={file.path}
          text={shown.after}
          edit={edit}
          onSaved={savedToast}
          onClose={() => setEditing(null)}
        />
      );
    const layout = oneSide(file) || window.innerWidth < SPLIT_FROM ? DIFF_LAYOUT.inline : prefs.layout;
    return (
      <>
        <ReviewDiff
          path={file.path}
          content={shown}
          layout={layout}
          ignoreWhitespace={prefs.ignoreWhitespace}
          full={whole}
          onHandle={onHandle}
          onLine={source.lineLink ? onLine : undefined}
          extensions={layerOf}
        />
        {mr && (
          <MrDiffThreads
            key={file.path}
            layer={layer}
            ctx={mr}
            file={file}
            content={shown}
            sides={layout === DIFF_LAYOUT.inline ? NEW_SIDE : BOTH_SIDES}
          />
        )}
        {notes && noted && (
          <DiffNotes
            key={file.path}
            layer={layer}
            api={notes}
            notes={noted}
            file={file}
            content={shown}
            sides={layout === DIFF_LAYOUT.inline ? NEW_SIDE : BOTH_SIDES}
            worktree={worktree}
          />
        )}
        {agent && (
          <DiffSuggestions
            key={`agent:${file.path}`}
            layer={layer}
            api={agent}
            actions={suggestionActions}
            file={file}
            content={shown}
            sides={layout === DIFF_LAYOUT.inline ? NEW_SIDE : BOTH_SIDES}
          />
        )}
      </>
    );
  })();

  return (
    <div
      className="hx-rvapp"
      // Focus stays in the screen when the read-only diff is clicked, so F7 reaches it.
      tabIndex={-1}
      data-panel={panel ? 'open' : 'closed'}
      onKeyDown={onKeyDown}
    >
      <section className="hx-rvapp__main" aria-label={t('next.review.diff')}>
        <div className="hx-rvapp__bar">
          <RoundButton
            icon="chevron-left"
            label={t('next.reviewApp.prevFile')}
            size="sm"
            variant="ghost"
            disabled={!prev}
            onClick={() => go(prev)}
          />
          <RoundButton
            icon="chevron-right"
            label={t('next.reviewApp.nextFile')}
            size="sm"
            variant="ghost"
            disabled={!next}
            onClick={() => go(next)}
          />
          {file && (
            <span className="hx-rvapp__path" title={file.path}>
              <span className="hx-rvapp__dir">{dirOf(file.path)}</span>
              <span className="hx-rvapp__file">{nameOf(file.path)}</span>
              {file.additions > 0 && <span className="hx-review__add">+{file.additions}</span>}
              {file.deletions > 0 && <span className="hx-review__del">−{file.deletions}</span>}
            </span>
          )}
          {entry?.k === 'loading' && <span className="hx-rvapp__muted">{t('next.review.loading')}</span>}
          {entry?.k === 'ready' && !entry.content.complete && (
            <span className="hx-rvapp__muted" title={t('next.review.patchOnlyHint')}>
              {t('next.review.patchOnly')}
            </span>
          )}
          <span className="hx-rvapp__spacer" />
          {canEdit && file && (
            <RoundButton
              icon="file-pen"
              label={editing === file.path ? t('next.reviewApp.edit.close') : t('next.reviewApp.edit.open')}
              size="sm"
              variant="ghost"
              aria-pressed={editing === file.path}
              onClick={() => setEditing(editing === file.path ? null : file.path)}
            />
          )}
          {file && (
            <button
              type="button"
              className="hx-rvapp__barbtn"
              aria-pressed={isViewed}
              onClick={() => toggle(file, !isViewed)}
            >
              <Icon name={isViewed ? 'square-check-big' : 'square'} size="sm" />
              <span className="hx-rvapp__barlabel">{t('next.review.viewed')}</span>
            </button>
          )}
          <ViewMenu whole={whole} onWhole={setWhole} />
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
          {expand && (
            <RoundButton
              icon="maximize-2"
              label={t('next.reviewApp.openFull')}
              size="sm"
              variant="ghost"
              onClick={expand}
            />
          )}
          <RoundButton
            icon="panel-right"
            label={panel ? t('next.reviewApp.hidePanel') : t('next.reviewApp.showPanel')}
            size="sm"
            variant="ghost"
            aria-pressed={panel}
            onClick={() => setPanel(!panel)}
          />
        </div>
        <div className="hx-rvapp__diff">
          {body}
          <DefinitionPeek
            peek={defs.peek}
            files={source.files}
            onOpen={(f, line) => go(f, line)}
            onClose={defs.close}
          />
        </div>
        {file && (
          <div className="hx-rvapp__foot">
            <button
              type="button"
              className="hx-rvapp__next"
              disabled={!upNext && isViewed}
              onClick={markAndNext}
            >
              <span>{upNext ? t('next.reviewApp.markNext') : t('next.reviewApp.markLast')}</span>
              {upNext && <code className="hx-rvapp__next-name">{nameOf(upNext.path)}</code>}
              <Icon name={upNext ? 'arrow-right' : 'check'} size="sm" />
            </button>
          </div>
        )}
      </section>
      {sheet.mounted && (
        <aside className="hx-rvapp__panel" data-state={sheet.state} aria-label={t('next.reviewApp.panel')}>
          <div className="hx-rvapp__panel-head">
            <span className="hx-rvapp__panel-tab">
              <Icon name="git-pull-request" size="sm" />
              {t('next.reviewApp.panel')}
            </span>
            <span className="hx-rvapp__spacer" />
            <RoundButton
              icon="x"
              label={t('next.reviewApp.hidePanel')}
              size="sm"
              variant="ghost"
              onClick={() => setPanel(false)}
            />
          </div>
          <div className="hx-rvapp__scroll">
            {pick}
            <ReviewHeader meta={source.meta} size={size} actions={actions} />
            {threadCount > 0 && (
              <Segmented<TPanelTab>
                label={t('next.reviewApp.panel')}
                value={tab}
                options={tabs}
                onChange={setTab}
              />
            )}
            {tab === 'threads' && mr && threadCount > 0 ? (
              <MrThreadsPanel
                ctx={mr}
                files={source.files}
                inDiff={inDiff}
                onOpen={(f, line) => go(f, line)}
              />
            ) : tab === 'threads' && threads && threadCount > 0 ? (
              <Threads threads={threads} files={source.files} onOpen={(f, line) => go(f, line)} />
            ) : (
              <>
                {agent && (
                  <AgentPanel
                    api={agent}
                    files={source.files}
                    onAsk={askAgent}
                    onOpen={(f, line) => go(f, line)}
                  />
                )}
                {noted && (
                  <NotesList
                    notes={noted}
                    files={source.files}
                    placeOf={placeOf}
                    onOpen={(f, line) => go(f, line)}
                  />
                )}
                <Progress n={viewedCount} total={source.files.length} />
                {viewedCount > 0 && fresh.length > 0 && fresh.length < source.files.length && (
                  <button
                    type="button"
                    className="hx-rvapp__since"
                    aria-pressed={sinceOn}
                    title={t('next.reviewApp.since.hint')}
                    onClick={() => setSince(!sinceOn)}
                  >
                    <Icon name={sinceOn ? 'list-x' : 'list-filter'} size="sm" />
                    {sinceOn
                      ? t('next.reviewApp.since.all')
                      : t('next.reviewApp.since.only', { n: fresh.length })}
                  </button>
                )}
                <ReviewTree
                  root={tree}
                  filter={filter}
                  onFilter={setFilter}
                  total={source.files.length}
                  viewedCount={viewedCount}
                  open={open}
                  selected={selected}
                  onToggleDir={(path) =>
                    setOpen((set) => {
                      const nextOpen = new Set(set);
                      if (!nextOpen.delete(path)) nextOpen.add(path);
                      return nextOpen;
                    })
                  }
                  onSelect={(target) => go(target)}
                  isViewed={viewed}
                  badge={badge}
                  onViewed={toggle}
                />
              </>
            )}
          </div>
          {(footer || notes || mr || commitable) && (
            <div className="hx-rvapp__panel-foot">
              {edit && commitable && (
                <CommitBox edit={edit} paths={commitPaths} open={commitOpen} onOpen={setCommitOpen} />
              )}
              {mr && <MrFooter ctx={mr} />}
              {notes && <NotesFooter api={notes} />}
              {footer}
            </div>
          )}
        </aside>
      )}
    </div>
  );
};

export default ReviewApp;
