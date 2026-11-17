// «Сделано за ход»: what a turn made, as one list at the end of its answer (the answer's text keeps its links as
// links). What is read and looked at — a document, a board, a Sinopia design, a stand — is a row with its picture, its
// whole name and «Открыть» / «Поделиться»; code, tasks and files are a row of one line with the state at its end. Many
// tasks the turn created are one group «Создано N задач»; tasks of one epic are its tree under a line of progress. A
// right click, two fingers or a held finger open the menu of the row's kind (link-menu.tsx).
import { Fragment, lazy, Suspense, useMemo, useState, type MouseEvent, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';
import { plural, t } from '@/next/i18n';
import { useArtifactMetas } from '@/next/features/artifacts/meta-store';
import { LOOK, type IArtifactItem, type TLook, type TMeta } from '@/next/features/artifacts/model';
import type { FileOp, MachineId, TurnOutput } from '@/next/protocol/v2';
import {
  LINK,
  LINK_ICON,
  partsOf,
  type ILinkOutput,
  type ILinkParts,
} from '@/next/features/chat/link-output';
import { usePortUp } from '@/next/features/chat/port-up';
import { copyLink, useLinkMenu, type ILinkMenuItem } from '@/next/features/chat/link-menu';
import { cardSlugOf } from '@/next/features/chat/link-card';
import { previewSrc, useLinkCardMeta } from '@/next/features/chat/link-card-meta';
import { designThumb } from '@/next/features/composer/attach-sources';
import { openOutside } from '@/next/features/chat/pane-sites';
import { isOutsideClick } from '@/next/features/chat/ref-chip';
import type { TTracker } from '@/next/features/chat/tracker-mark';
import type { IOutputsContext } from '@/next/features/chat/outputs';

const TrackerMark = lazy(() => import('@/next/features/chat/tracker-mark'));
// The share window (Sinopia's access dialog) is loaded on «Поделиться» only, as link-card.tsx does.
const ObjectShare = lazy(() => import('@/next/features/app-main/object-share'));

export interface ILinkMade {
  output: TurnOutput;
  link: ILinkOutput;
}

type TMenu = ReturnType<typeof useLinkMenu>;

const KIND_ICON = {
  artifact: 'book-open',
  design: 'palette',
  doc: 'file-text',
  mr: 'git-pull-request',
  stand: 'rocket',
  delivery: 'file-text',
  task: 'square-check-big',
  commit: 'git-commit-horizontal',
} as const satisfies Record<TurnOutput['kind'], string>;

const FILE_ICON = { created: 'file-plus', edited: 'file-pen', read: 'file-search' } as const satisfies Record<
  FileOp,
  string
>;

const SHAREABLE = /\.(md|markdown|html?)$/i;

/** A batch is this many tasks created at once; fewer are rows of their own. */
const BATCH_MIN = 4;
/** The rows of a batch in sight before «Показать ещё». */
const BATCH_SHOWN = 3;

const stop = (event: MouseEvent) => event.stopPropagation();

// ── the parts of a row ──

const Tile = ({ icon }: { icon: string }) => (
  <span className="hx-made__tile" aria-hidden="true">
    <Icon name={icon} />
  </span>
);

const TRACKER_NAME: Record<TTracker, string> = {
  youtrack: 'YouTrack',
  jira: 'Jira',
  linear: 'Linear',
  github: 'GitHub',
  gitlab: 'GitLab',
};

/** Which tracker a task's link is of, by its address; null when it does not say. */
export const trackerOf = (url: string): TTracker | null => {
  try {
    const { hostname, pathname } = new URL(url);
    if (/(^|\.)atlassian\.net$/i.test(hostname) || pathname.startsWith('/browse/')) return 'jira';
    if (hostname === 'linear.app') return 'linear';
    if (/\/issue\/[A-Z][A-Z0-9]*-\d+/.test(pathname)) return 'youtrack';
  } catch {
    // Not an address: the app's own icon.
  }
  return null;
};

const TaskTile = ({ url }: { url: string }) => {
  const tracker = trackerOf(url);
  const plain = <Tile icon={LINK_ICON.task} />;
  if (!tracker) return plain;
  return (
    <Suspense fallback={plain}>
      <TrackerMark tracker={tracker} />
    </Suspense>
  );
};

const State = ({ text, tone }: { text: string | null; tone: string | null }) =>
  text ? (
    <span className="hx-made__state" data-tone={tone ?? undefined}>
      {text}
    </span>
  ) : null;

/** The line under the name: the code-like name in monospace, then the rest, with quiet dots between. */
const Line = ({
  mono,
  sub,
  pending,
}: {
  mono?: string | null;
  sub: readonly ReactNode[];
  pending?: boolean;
}) => {
  const parts = [mono ? <span className="hx-made__mono">{mono}</span> : null, ...sub].filter(
    (part) => part !== null && part !== '',
  );
  if (parts.length === 0) return null;
  return (
    <span className="hx-made__sub" data-pending={pending || undefined}>
      {parts.map((part, index) => (
        <Fragment key={index}>
          {index > 0 && (
            <span className="hx-made__sep" aria-hidden="true">
              ·
            </span>
          )}
          {part}
        </Fragment>
      ))}
    </span>
  );
};

interface IRowProps {
  tile: ReactNode;
  title: ReactNode;
  line: ReactNode;
  end?: ReactNode;
  href?: string | null;
  newTab?: boolean;
  onOpen?: (() => void) | null;
  menu?: TMenu;
  /** A row inside a group: indented, a lighter name. */
  depth?: 0 | 1 | 2;
  below?: ReactNode;
}

/** A row of one line: a tap opens the thing in the pane, ⌘/Ctrl-click a link in a new tab of the browser. */
const Row = ({ tile, title, line, end, href, newTab = true, onOpen, menu, depth = 0, below }: IRowProps) => {
  const click = (event: MouseEvent<HTMLElement>) => {
    if (href && newTab && isOutsideClick(event)) return;
    event.preventDefault();
    if (menu?.held()) return;
    onOpen?.();
  };
  const body = (
    <>
      {tile}
      <span className="hx-made__text">
        <span className="hx-made__title">{title}</span>
        {line}
        {below}
      </span>
      {end}
    </>
  );
  const props = { className: 'hx-made__row', 'data-depth': depth || undefined, ...menu?.handlers };
  return (
    <>
      {href ? (
        <a {...props} href={href} target="_blank" rel="noopener noreferrer" onClick={click}>
          {body}
        </a>
      ) : (
        <button type="button" {...props} onClick={click} disabled={!onOpen}>
          {body}
        </button>
      )}
      {menu?.menu}
    </>
  );
};

/** A picture in place of the tile; the tile again when it does not load. */
const Preview = ({ src, page, icon }: { src: string | null; page?: boolean; icon: string }) => {
  const [broken, setBroken] = useState(false);
  if (!src || broken)
    return (
      <span className="hx-made__preview" data-empty="true" aria-hidden="true">
        <Icon name={icon} />
      </span>
    );
  return (
    <span className="hx-made__preview" aria-hidden="true">
      {page ? (
        <iframe src={src} title="" sandbox="allow-scripts" loading="lazy" tabIndex={-1} />
      ) : (
        <img src={src} alt="" loading="lazy" decoding="async" onError={() => setBroken(true)} />
      )}
    </span>
  );
};

interface IRichProps {
  preview: ReactNode;
  title: string;
  kind: ReactNode;
  extra?: ReactNode;
  openLabel?: string;
  openIcon?: string;
  href?: string | null;
  onOpen: (() => void) | null;
  onShare: (() => void) | null;
  menu: TMenu;
}

/** What is read and looked at: its picture, its whole name (two lines at most), what it is, and two buttons. */
const Rich = ({
  preview,
  title,
  kind,
  extra,
  openLabel = t('next.turnMade.open'),
  openIcon = 'panel-right',
  href,
  onOpen,
  onShare,
  menu,
}: IRichProps) => {
  const click = (event: MouseEvent<HTMLElement>) => {
    if (menu.held()) return;
    if (href && isOutsideClick(event)) {
      openOutside(href);
      return;
    }
    onOpen?.();
  };
  return (
    <>
      {/* The buttons are the keyboard's way in; a click anywhere on the row is the mouse's. */}
      <div
        className="hx-made__row hx-made__row--rich"
        data-open={onOpen ? 'true' : undefined}
        onClick={click}
        {...menu.handlers}
      >
        {preview}
        <span className="hx-made__text">
          <span className="hx-made__title hx-made__title--full">{title}</span>
          <span className="hx-made__sub">{kind}</span>
          {extra}
          {(onOpen || onShare) && (
            <span className="hx-made__acts" onClick={stop}>
              {onOpen && (
                <button type="button" className="hx-made__btn" onClick={onOpen}>
                  <Icon name={openIcon} size="sm" />
                  {openLabel}
                </button>
              )}
              {onShare && (
                <button type="button" className="hx-made__btn" onClick={onShare}>
                  <Icon name="share" size="sm" />
                  {t('next.turnMade.share')}
                </button>
              )}
            </span>
          )}
        </span>
      </div>
      {menu.menu}
    </>
  );
};

// ── the rows of the things ──

interface IMadeLinkProps {
  made: ILinkMade;
  context: IOutputsContext;
  meta: TMeta | null;
  look: TLook;
}

const copyText = (text: string, done: string) =>
  void navigator.clipboard.writeText(text).then(
    () => toast(done),
    () => toast(t('next.shellData.noClipboard'), { tone: 'error' }),
  );

/** A document or a board of the installation: the hub's picture of it and its name. */
const HubRow = ({ made, context, meta: live }: IMadeLinkProps) => {
  const { output, link } = made;
  const slug = cardSlugOf(link.url);
  const meta = useLinkCardMeta(slug);
  const [sharing, setSharing] = useState(false);
  const onOpen = context.onOpen ? () => context.onOpen?.(output) : null;
  const onShare = meta && slug ? () => setSharing(true) : () => copyLink(link.url);
  const kind = t(`next.outputs.link.${link.kind}`);
  const menu = useLinkMenu({ href: link.url, onPane: onOpen, outside: true, onShare, label: kind });
  const previews = context.shown.preview;
  const src = previews && meta && slug && (meta.preview || meta.page) ? previewSrc(slug) : null;
  const title = meta?.title ?? (live?.kind === 'artifact' ? live.title : null) ?? output.title;
  return (
    <>
      <Rich
        preview={<Preview src={src} page={!meta?.preview && meta?.page} icon={LINK_ICON[link.kind]} />}
        title={title}
        kind={kind}
        href={link.url}
        onOpen={onOpen}
        onShare={onShare}
        menu={menu}
      />
      {sharing && slug && (
        <Suspense fallback={null}>
          <ObjectShare
            kind={link.kind === LINK.board ? 'board' : 'artifact'}
            slug={slug}
            open
            onClose={() => setSharing(false)}
            fallbackLink={link.url}
          />
        </Suspense>
      )}
    </>
  );
};

/** A stand: its page's title, whether it answers, its address to copy. */
const StandRow = ({ made, context, meta, look }: IMadeLinkProps) => {
  const { output, link } = made;
  const parts = partsOf(link, meta, look, null);
  const onOpen = context.onOpen ? () => context.onOpen?.(output) : null;
  const onShare = () => copyLink(link.url);
  const kind = t('next.outputs.link.stand');
  const menu = useLinkMenu({ href: link.url, onPane: onOpen, outside: true, label: kind });
  const address = link.url.replace(/^https?:\/\//, '').replace(/\/$/, '');
  return (
    <Rich
      preview={<Preview src={null} icon={LINK_ICON.stand} />}
      title={parts.title ?? output.title}
      kind={
        <Line sub={[kind, <State key="s" text={parts.state} tone={parts.tone} />]} pending={parts.pending} />
      }
      extra={
        <span className="hx-made__url" onClick={stop}>
          <span className="hx-made__url-text">{address}</span>
          <button
            type="button"
            className="hx-made__url-copy"
            aria-label={t('next.turnMade.copyUrl')}
            title={t('next.turnMade.copyUrl')}
            onClick={() => copyText(link.url, t('next.turnMade.copied'))}
          >
            <Icon name="copy" size="sm" />
          </button>
        </span>
      }
      href={link.url}
      onOpen={onOpen}
      onShare={onShare}
      menu={menu}
    />
  );
};

/** A page of the catalogue that is no document or board: its name, «Открыть», «Поделиться» (its link). */
const PageRow = ({ made, context, meta, look }: IMadeLinkProps) => {
  const { output, link } = made;
  const parts = partsOf(link, meta, look, null);
  const onOpen = context.onOpen ? () => context.onOpen?.(output) : null;
  const menu = useLinkMenu({ href: link.url, onPane: onOpen, outside: true });
  return (
    <Rich
      preview={<Preview src={null} icon={LINK_ICON.page} />}
      title={parts.title ?? output.title}
      kind={t('next.outputs.link.page')}
      href={link.url}
      onOpen={onOpen}
      onShare={() => copyLink(link.url)}
      menu={menu}
    />
  );
};

/** Code, a task, a server: one line, the state at its end. */
const LineRow = ({
  made,
  context,
  meta,
  look,
  depth = 0,
  note,
}: IMadeLinkProps & { depth?: 0 | 1 | 2; note?: string | null }) => {
  const { output, link } = made;
  const up = usePortUp(context.machine, link.port);
  const parts: ILinkParts = partsOf(link, meta, look, up);
  const onOpen = context.onOpen ? () => context.onOpen?.(output) : null;
  const task = link.kind === LINK.task;
  const extra: ILinkMenuItem[] =
    task && parts.mono
      ? [
          {
            icon: 'copy',
            label: t('next.turnMade.copyId'),
            run: () => copyText(parts.mono ?? '', t('next.turnMade.idCopied')),
          },
        ]
      : [];
  const menu = useLinkMenu({
    href: link.url,
    onPane: onOpen,
    outside: link.newTab,
    extra,
    label: t(`next.outputs.link.${link.kind}`),
  });
  const sub = note ? [note] : parts.sub;
  const title = parts.title ?? output.title;
  return (
    <Row
      tile={depth > 0 ? null : task ? <TaskTile url={link.url} /> : <Tile icon={LINK_ICON[link.kind]} />}
      title={title}
      line={<Line mono={parts.mono === title ? null : parts.mono} sub={sub} pending={parts.pending} />}
      end={<State text={parts.state} tone={parts.tone} />}
      href={link.url}
      newTab={link.newTab}
      onOpen={onOpen}
      menu={menu}
      depth={depth}
    />
  );
};

const LinkRow = (props: IMadeLinkProps) => {
  switch (props.made.link.kind) {
    case LINK.doc:
    case LINK.board:
      return <HubRow {...props} />;
    case LINK.stand:
      return <StandRow {...props} />;
    case LINK.page:
      return <PageRow {...props} />;
    default:
      return <LineRow {...props} />;
  }
};

// ── the tasks, many at once ──

type TRead = (made: ILinkMade) => { meta: TMeta | null; look: TLook };

const issueOf = (meta: TMeta | null) => (meta?.kind === 'issue' ? meta : null);

interface IEpic {
  key: string;
  title: string;
  /** The epic's own card when the turn made or touched it too. */
  head: ILinkMade | null;
  url: string;
  kids: ILinkMade[];
}

/** The tasks of the turn: the epics two or more of them share, a batch of what it created at once, and the rest. */
export const groupTasks = (
  tasks: readonly ILinkMade[],
  read: TRead,
): { epics: IEpic[]; batch: ILinkMade[]; single: ILinkMade[] } => {
  const byParent = new Map<string, IEpic>();
  for (const made of tasks) {
    const parent = issueOf(read(made).meta)?.parent;
    if (!parent) continue;
    const epic = byParent.get(parent.key) ?? {
      key: parent.key,
      title: parent.title,
      head: null,
      url: made.link.url.replace(made.link.key, parent.key),
      kids: [],
    };
    epic.kids.push(made);
    byParent.set(parent.key, epic);
  }
  const epics = [...byParent.values()].filter((epic) => epic.kids.length >= 2);
  const grouped = new Set<ILinkMade>(epics.flatMap((epic) => epic.kids));
  for (const epic of epics) {
    const head = tasks.find((made) => made.link.key === epic.key) ?? null;
    if (head) {
      epic.head = head;
      epic.url = head.link.url;
      grouped.add(head);
    }
  }
  const rest = tasks.filter((made) => !grouped.has(made));
  const created = rest.filter((made) => made.output.op === 'created');
  const batch = created.length >= BATCH_MIN ? created : [];
  return { epics, batch, single: rest.filter((made) => !batch.includes(made)) };
};

const same = (values: readonly (string | null | undefined)[]): string | null => {
  const first = values[0];
  return first && values.every((value) => value === first) ? first : null;
};

const BatchRows = ({
  tasks,
  context,
  read,
}: {
  tasks: ILinkMade[];
  context: IOutputsContext;
  read: TRead;
}) => {
  const [open, setOpen] = useState(false);
  const first = tasks[0];
  const tracker = trackerOf(first.link.url);
  const keys = tasks.map((made) => issueOf(read(made).meta)?.key || made.link.key);
  const project = same(keys.map((key) => key.split('-')[0]));
  const who = same(tasks.map((made) => issueOf(read(made).meta)?.assignee?.name));
  const menu = useLinkMenu({
    href: null,
    onPane: null,
    outside: false,
    extra: [
      {
        icon: 'copy',
        label: t('next.turnMade.copyIds'),
        run: () => copyText(keys.join('\n'), t('next.turnMade.idCopied')),
      },
    ],
    label: plural(tasks.length, 'next.turnMade.batch'),
  });
  const shown = open ? tasks : tasks.slice(0, BATCH_SHOWN);
  const more = tasks.length - shown.length;
  return (
    <div className="hx-made__group">
      <Row
        tile={<TaskTile url={first.link.url} />}
        title={plural(tasks.length, 'next.turnMade.batch')}
        line={
          <Line
            sub={[
              tracker ? TRACKER_NAME[tracker] : null,
              project,
              who ? t('next.turnMade.allOn', { who }) : null,
            ].filter((part): part is string => !!part)}
          />
        }
        end={
          <span className="hx-made__toggle">
            {t(open ? 'next.turnMade.collapse' : 'next.turnMade.showAll')}
          </span>
        }
        onOpen={() => setOpen(!open)}
        menu={menu}
      />
      {shown.map((made) => (
        <LineRow key={made.output.ref} made={made} context={context} {...read(made)} depth={1} />
      ))}
      {more > 0 && (
        <button type="button" className="hx-made__more" onClick={() => setOpen(true)}>
          {t('next.turnMade.showMore', { n: more })}
        </button>
      )}
    </div>
  );
};

const EpicRows = ({ epic, context, read }: { epic: IEpic; context: IOutputsContext; read: TRead }) => {
  const [open, setOpen] = useState(true);
  const done = epic.kids.filter((made) => issueOf(read(made).meta)?.resolved).length;
  const headMeta = epic.head ? issueOf(read(epic.head).meta) : null;
  const { head } = epic;
  const onPane = head && context.onOpen ? () => context.onOpen?.(head.output) : null;
  const menu = useLinkMenu({
    href: epic.url,
    onPane,
    outside: true,
    extra: [
      {
        icon: 'copy',
        label: t('next.turnMade.copyId'),
        run: () => copyText(epic.key, t('next.turnMade.idCopied')),
      },
    ],
    label: t('next.turnMade.epic'),
  });
  const share = epic.kids.length ? Math.round((done / epic.kids.length) * 100) : 0;
  return (
    <div className="hx-made__group">
      <Row
        tile={<TaskTile url={epic.url} />}
        title={headMeta?.title ?? epic.title}
        line={
          <Line
            mono={epic.key}
            sub={[t('next.turnMade.epic'), t('next.turnMade.epicDone', { done, n: epic.kids.length })]}
          />
        }
        below={
          <span className="hx-made__track" aria-hidden="true">
            <span style={{ width: `${share}%` }} />
          </span>
        }
        end={
          <span className="hx-made__chev" data-open={open || undefined} aria-hidden="true">
            <Icon name="chevron-down" size="sm" />
          </span>
        }
        onOpen={() => setOpen(!open)}
        menu={menu}
      />
      {open &&
        epic.kids.map((made) => (
          <LineRow
            key={made.output.ref}
            made={made}
            context={context}
            {...read(made)}
            depth={2}
            note={made.output.op === 'created' ? t('next.turnMade.new') : null}
          />
        ))}
    </div>
  );
};

// ── the things that are no links ──

const DocRow = ({ output, context }: { output: TurnOutput; context: IOutputsContext }) => {
  const onOpen = context.onOpen ? () => context.onOpen?.(output) : null;
  const shareable = !/^https?:\/\//.test(output.ref) && SHAREABLE.test(output.ref) && context.onShareFile;
  const onShare = shareable ? () => context.onShareFile?.(output.ref) : null;
  const kind = t('next.outputs.kind.doc');
  const menu = useLinkMenu({ href: null, onPane: onOpen, outside: false, onShare, label: kind });
  return (
    <Rich
      preview={<Preview src={null} icon={KIND_ICON.doc} />}
      title={output.title}
      kind={output.op === 'changed' ? `${kind} · ${t('next.outputs.changed')}` : kind}
      onOpen={onOpen}
      onShare={onShare}
      menu={menu}
    />
  );
};

const DesignRow = ({ output, context }: { output: TurnOutput; context: IOutputsContext }) => {
  const onOpen = context.onOpen ? () => context.onOpen?.(output) : null;
  const kind = t('next.turnMade.design');
  const menu = useLinkMenu({
    href: null,
    onPane: onOpen,
    outside: false,
    label: kind,
    paneLabel: t('next.turnMade.openDesign'),
  });
  const src = context.shown.preview && context.machine ? designThumb(context.machine, output.ref) : null;
  return (
    <Rich
      preview={<Preview src={src} icon={KIND_ICON.design} />}
      title={output.title}
      kind={output.op === 'changed' ? `${kind} · ${t('next.outputs.changed')}` : kind}
      openLabel={t('next.turnMade.openDesign')}
      openIcon="palette"
      onOpen={onOpen}
      onShare={null}
      menu={menu}
    />
  );
};

const OtherRow = ({ output, context }: { output: TurnOutput; context: IOutputsContext }) => (
  <Row
    tile={<Tile icon={KIND_ICON[output.kind]} />}
    title={output.title}
    line={<Line sub={[t(`next.outputs.kind.${output.kind}`)]} />}
    onOpen={context.onOpen ? () => context.onOpen?.(output) : null}
  />
);

const FileRow = ({ file, context }: { file: { path: string; op: FileOp }; context: IOutputsContext }) => {
  const slash = file.path.lastIndexOf('/');
  return (
    <Row
      tile={<Tile icon={FILE_ICON[file.op]} />}
      title={file.path.slice(slash + 1)}
      line={<Line sub={[t(`next.files.${file.op}`), slash > 0 ? file.path.slice(0, slash) : null]} />}
      onOpen={context.onOpenFile ? () => context.onOpenFile?.(file.path) : null}
    />
  );
};

// ── the block ──

const NO_MACHINE = '' as MachineId;

interface IMadeProps {
  context: IOutputsContext;
  links: readonly ILinkMade[];
  /** The things that are no links: a document of the folder, a design. */
  outputs: readonly TurnOutput[];
  files: readonly { path: string; op: FileOp }[];
  /** The files the agent handed over, as their cards (delivery-card.tsx). */
  deliveries: ReactNode;
  count: number;
  /** The block's head «Сделано за ход · N»; a list under another card (a delegated chat's) has none. */
  head?: boolean;
}

const Made = ({ context, links, outputs, files, deliveries, count, head = true }: IMadeProps) => {
  const items = useMemo(
    () =>
      context.machine ? links.flatMap(({ link }) => (link.item ? [link.item] : [])) : ([] as IArtifactItem[]),
    [context.machine, links],
  );
  const metas = useArtifactMetas(context.machine ?? NO_MACHINE, items);
  const read: TRead = (made) => (made.link.item ? metas.of(made.link.item) : { meta: null, look: LOOK.none });
  const tasks = links.filter((made) => made.link.kind === LINK.task);
  const { epics, batch, single } = groupTasks(tasks, read);
  const rest = links.filter((made) => made.link.kind !== LINK.task);
  if (count === 0) return null;
  return (
    <section className="hx-made" aria-label={t('next.turnMade.head')}>
      {head && (
        <div className="hx-made__head">
          <span>{t('next.turnMade.head')}</span>
          <span>{count}</span>
        </div>
      )}
      {outputs.map((output) =>
        output.kind === 'doc' ? (
          <DocRow key={`doc:${output.ref}`} output={output} context={context} />
        ) : output.kind === 'design' ? (
          <DesignRow key={`design:${output.ref}`} output={output} context={context} />
        ) : (
          <OtherRow key={`${output.kind}:${output.ref}`} output={output} context={context} />
        ),
      )}
      {rest.map((made) => (
        <LinkRow
          key={`${made.output.kind}:${made.output.ref}`}
          made={made}
          context={context}
          {...read(made)}
        />
      ))}
      {epics.map((epic) => (
        <EpicRows key={epic.key} epic={epic} context={context} read={read} />
      ))}
      {batch.length > 0 && <BatchRows tasks={batch} context={context} read={read} />}
      {single.map((made) => (
        <LineRow key={made.output.ref} made={made} context={context} {...read(made)} />
      ))}
      {deliveries}
      {files.map((file) => (
        <FileRow key={file.path} file={file} context={context} />
      ))}
    </section>
  );
};

export default Made;
