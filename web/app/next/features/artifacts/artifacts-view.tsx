// The Artifacts tool (.hx-tv): what the agent made and named in the chat — documents and boards, tasks, merge requests —
// as calm rows, each with the real title and, for a task or a merge request, its state. The rows stand as soon
// as the links are known; titles and states fill in as the runtime reads them, a bar in the title's place until then,
// and what was read before is on screen at once. A skeleton gives way to the list through Swap; what did not load says so
// with «Повторить».
import { useEffect, useMemo, useState, type MouseEvent } from 'react';
import { runtimePath } from '@/next/core/net/machine';
import { cachedItems, rememberItems } from '@/next/features/artifacts/items-cache';
import { retryMetas, useArtifactMetas } from '@/next/features/artifacts/meta-store';
import {
  ARTIFACT_KIND,
  rowOf,
  SECTION,
  sectionsOf,
  type IArtifactItem,
  type IArtifactRow,
  type IMetaPerson,
  type IMrLabels,
  type TArtifactKind,
  type TSection,
} from '@/next/features/artifacts/model';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { usePresentFlag } from '@/next/ui/presence';
import { Skeleton } from '@/next/ui/skeleton';
import Swap from '@/next/ui/swap';
import { useDeadline } from '@/next/ui/use-deadline';
import '@/next/styles/artifacts.css';

/** The folded links' exit, as long as hx-art-fold-out in artifacts.css. */
const FOLD_EXIT_MS = 180;

const ICONS: Record<TArtifactKind, string> = {
  doc: 'file-text',
  board: 'shapes',
  task: 'square-check-big',
  mr: 'git-pull-request',
  link: 'link',
};

const SECTION_TITLE: Record<TSection, string> = {
  docs: 'next.artifacts.docs',
  tasks: 'next.artifacts.tasks',
  mrs: 'next.artifacts.mrs',
  links: 'next.artifacts.links',
};

export interface IArtifactsViewProps {
  machine: MachineId;
  /** The conversation: what was seen in it last time is drawn before the runtime answers. */
  convKey: string;
  items: readonly IArtifactItem[];
  /** The runtime has read the whole transcript (or the conversation has none): the list is all there is. */
  complete: boolean;
  /** The runtime did not answer. */
  failed: boolean;
  /** Asks the runtime for the links again. */
  onReload: () => void;
  /** A click on a row; the bridge opens it in a tab of the pane or leaves the link to the browser. */
  onOpen: (item: IArtifactItem, title: string | null, event: MouseEvent<HTMLAnchorElement>) => void;
}

const Initials = ({ person }: { person: IMetaPerson }) => (
  <span className="hx-art__ava" data-initials="true" title={person.name}>
    {person.name
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase())
      .join('') || '?'}
  </span>
);

const Avatar = ({ machine, person }: { machine: MachineId; person: IMetaPerson }) => {
  const [broken, setBroken] = useState(false);
  if (!person.avatar || broken || !person.avatar.startsWith('/')) return <Initials person={person} />;
  return (
    <img
      className="hx-art__ava"
      src={runtimePath(person.avatar as `/${string}`, machine)}
      alt={person.name}
      title={person.name}
      loading="lazy"
      onError={() => setBroken(true)}
    />
  );
};

const Row = ({
  row,
  machine,
  fresh,
  onOpen,
}: {
  row: IArtifactRow;
  machine: MachineId;
  fresh: boolean;
  onOpen: IArtifactsViewProps['onOpen'];
}) => {
  const { item } = row;
  return (
    <a
      className="hx-tv__row hx-art"
      href={item.url}
      target="_blank"
      rel="noopener noreferrer"
      data-kind={item.kind}
      data-rest={row.rest || undefined}
      data-fresh={fresh || undefined}
      aria-busy={row.pending || undefined}
      onClick={(event) => onOpen(item, row.title, event)}
    >
      <Icon name={ICONS[item.kind]} size="sm" />
      <span className="hx-tv__main">
        <span className="hx-tv__name">
          {row.title === null ? (
            <Skeleton className="hx-art__bar" />
          ) : (
            <span key={row.title} className="hx-art__text">
              {row.title}
            </span>
          )}
        </span>
        <span className="hx-tv__sub">
          {row.tone && <i className="hx-art__dot" data-tone={row.tone} aria-hidden="true" />}
          <span key={row.sub} className="hx-art__text">
            {row.sub}
          </span>
        </span>
      </span>
      {row.person && <Avatar machine={machine} person={row.person} />}
      {item.kind !== ARTIFACT_KIND.task && <Icon name="external-link" size="sm" className="hx-tv__go" />}
    </a>
  );
};

const ArtifactsSkeleton = () => (
  <>
    {[3, 2].map((rows) => (
      <div key={rows} className="hx-art-sk">
        <Skeleton className="hx-art-sk__label" />
        {Array.from({ length: rows }, (_, i) => (
          <div key={i} className="hx-art-sk__row">
            <Skeleton className="hx-art-sk__icon" />
            <span className="hx-art-sk__text">
              <Skeleton className="hx-art-sk__name" />
              <Skeleton className="hx-art-sk__sub" />
            </span>
          </div>
        ))}
      </div>
    ))}
  </>
);

const mergeByUrl = (first: readonly IArtifactItem[], second: readonly IArtifactItem[]): IArtifactItem[] => {
  const seen = new Set(first.map((item) => item.url));
  return [...first, ...second.filter((item) => !seen.has(item.url))];
};

const ArtifactsView = ({
  machine,
  convKey,
  items,
  complete,
  failed,
  onReload,
  onOpen,
}: IArtifactsViewProps) => {
  const cached = useMemo(() => cachedItems(convKey), [convKey]);
  // Until the runtime has read the whole transcript, what was seen last time stands beside what the timeline shows.
  const shown = useMemo(() => (complete ? items : mergeByUrl(items, cached)), [complete, items, cached]);
  const ready = shown.length > 0 || complete;
  const deadline = useDeadline(!ready && !failed);
  const metas = useArtifactMetas(machine, shown);
  const [linksOpen, setLinksOpen] = useState(false);
  const links = usePresentFlag(linksOpen, FOLD_EXIT_MS);

  useEffect(() => {
    if (complete) rememberItems(convKey, items);
  }, [complete, convKey, items]);

  // The rows that were there when the list first stood do not come in; the ones added after do.
  const [initial, setInitial] = useState<ReadonlySet<string> | null>(null);
  if (ready && initial === null) setInitial(new Set(shown.map((item) => item.url)));

  const mr: IMrLabels = {
    opened: t('next.artifacts.mrOpened'),
    merged: t('next.artifacts.mrMerged'),
    closed: t('next.artifacts.mrClosed'),
    draft: t('next.artifacts.mrDraft'),
  };

  if (!ready && (failed || deadline.expired))
    return (
      <div className="hx-tv" data-tool="artifacts">
        <LoadFailed
          title={t('next.load.artifacts')}
          onRetry={() => {
            deadline.restart();
            onReload();
          }}
        />
      </div>
    );

  const unreadable = !complete && failed;
  const retry = () => {
    if (unreadable) onReload();
    retryMetas(machine, shown);
  };
  const rowsOf = (list: readonly IArtifactItem[]) =>
    list.map((item) => {
      const { meta, look } = metas.of(item);
      return (
        <Row
          key={item.url}
          row={rowOf(item, meta, look, mr)}
          machine={machine}
          fresh={initial !== null && !initial.has(item.url)}
          onOpen={onOpen}
        />
      );
    });

  return (
    <div
      className="hx-tv"
      data-tool="artifacts"
      role={ready ? undefined : 'status'}
      aria-label={ready ? undefined : t('next.artifacts.loading')}
    >
      <Swap ready={ready} skeleton={<ArtifactsSkeleton />}>
        {ready && shown.length === 0 && <div className="hx-tv__empty">{t('next.artifacts.empty')}</div>}
        {sectionsOf(shown).map((section) => {
          if (section.id === SECTION.links)
            return (
              <div key={section.id} className="hx-art-more">
                <button
                  type="button"
                  className="hx-tv__fold"
                  aria-expanded={linksOpen}
                  onClick={() => setLinksOpen((now) => !now)}
                >
                  <Icon name="chevron-right" size="sm" />
                  {t(SECTION_TITLE[section.id])}
                </button>
                {links.mounted && (
                  <div className="hx-art-more__body" data-state={links.state}>
                    {rowsOf(section.items)}
                  </div>
                )}
              </div>
            );
          return (
            <section key={section.id} className="hx-art-section">
              <div className="hx-tv__label">{t(SECTION_TITLE[section.id])}</div>
              {rowsOf(section.items)}
            </section>
          );
        })}
        {(unreadable || metas.failed > 0) && (
          <LoadFailed
            size={LOAD_FAILED_SIZE.inline}
            title={unreadable ? t('next.load.artifacts') : t('next.artifacts.someFailed')}
            onRetry={retry}
          />
        )}
      </Swap>
    </div>
  );
};

export default ArtifactsView;
