import { t } from '@/lib/i18n';
import { memo, useMemo, useState, type MouseEvent, type ReactNode } from 'react';
import { ArrowUpRight, ChevronRight, Sparkles } from 'lucide-react';
import SiteIcon from '@/components/agents/site-icon';
import { AssigneeAvatar, IssueState, MrState } from '@/components/agents/link-meta-parts';
import AttachmentViewer, { type IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import { isImageHref, isPlainClick, linkImageName } from '@/lib/image-link';
import useLinkMeta, { useLinkMetas } from '@/hooks/use-link-meta';
import type { IArtifactLink, TArtifactGroup } from '@/lib/hl-artifacts';
import { hasLinkMeta, presentLink } from '@/lib/presented-link';
import { paneDocTargetOf } from '@/lib/hl-doc-path';
import { cn } from '@/lib/utils';
import { openLinkInPane } from '@/lib/hl-pane-link';

interface IArtifactsTabProps {
  /** The conversation's links from its runtime, merged with the loaded timeline. */
  links: IArtifactLink[];
  /** Opens a published document in a tab of the right panel; without it links open in a new browser tab. */
  onOpenDoc?: (path: string, title: string) => void;
}

const GROUPS: { id: Exclude<TArtifactGroup, 'other'>; title: string }[] = [
  { id: 'artifact', title: t('inspector.artifactsTab.artifacts') },
  { id: 'issue', title: t('inspector.artifactsTab.issues') },
];

const ArtifactRow = ({
  link,
  nested = false,
  onOpenDoc,
}: {
  link: IArtifactLink;
  nested?: boolean;
  onOpenDoc?: (path: string, title: string) => void;
}) => {
  const presented = presentLink(link.url);
  const meta = useLinkMeta(hasLinkMeta(presented.kind) ? link.url : null);

  let title: ReactNode = link.label ?? presented.label;
  let detail: ReactNode = presented.detail;
  let aside: ReactNode = <ArrowUpRight className="tw-artifact-go" />;

  if (presented.kind === 'artifact') {
    title = meta?.kind === 'artifact' ? meta.title : (link.label ?? presented.label);
    detail = meta?.kind === 'artifact' && meta.description ? meta.description : presented.detail;
  } else if (presented.kind === 'mr') {
    title = meta?.kind === 'mr' ? meta.title : (link.label ?? presented.label);
    detail = `${presented.project} !${presented.iid}`;
    if (meta?.kind === 'mr') aside = <MrState meta={meta} />;
  } else if (presented.kind === 'issue') {
    const key = meta?.kind === 'issue' ? meta.key : presented.key;
    title = (
      <>
        <b className="tw-artifact-key">{key}</b>
        {meta?.kind === 'issue' ? meta.title : (link.label ?? '')}
      </>
    );
    detail =
      meta?.kind === 'issue' ? (
        <span className="tw-artifact-issue-line">
          <IssueState meta={meta} withLabel />
        </span>
      ) : null;
    if (meta?.kind === 'issue' && meta.assignee)
      aside = <AssigneeAvatar assignee={meta.assignee} className="tw-artifact-avatar" />;
  }

  const docPath = onOpenDoc ? paneDocTargetOf(link.url) : null;
  const [viewing, setViewing] = useState<IAttachmentView | null>(null);
  const openHere = (event: MouseEvent<HTMLAnchorElement>) => {
    if (isImageHref(link.url) && isPlainClick(event.nativeEvent)) {
      event.preventDefault();
      setViewing({ kind: 'image', url: link.url, name: linkImageName(link.url, link.label ?? '') });
      return;
    }
    if (presented.kind === 'issue') {
      openLinkInPane(event, link.url);
      return;
    }
    if (
      !docPath ||
      !onOpenDoc ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    )
      return;
    event.preventDefault();
    onOpenDoc(docPath, typeof title === 'string' ? title : (link.label ?? presented.label));
  };

  return (
    <li>
      <a
        className={cn('tw-artifact', `tw-artifact-${link.group}`)}
        href={link.url}
        target="_blank"
        rel="noopener noreferrer"
        title={presented.kind === 'artifact' ? presented.detail : link.url}
        onClick={openHere}
      >
        <span className="tw-artifact-icon">
          <SiteIcon href={link.url} />
        </span>
        <span className="tw-artifact-text">
          {!nested && meta?.kind === 'issue' && meta.parent && (
            <span className="tw-artifact-epic" title={`${meta.parent.key} · ${meta.parent.title}`}>
              <b>{meta.parent.key}</b>
              {meta.parent.title}
            </span>
          )}
          <strong>{title}</strong>
          {detail && <small>{detail}</small>}
        </span>
        {aside}
      </a>
      {viewing && <AttachmentViewer item={viewing} onClose={() => setViewing(null)} />}
    </li>
  );
};

interface IEpicGroup {
  key: string;
  epic: IArtifactLink;
  tasks: IArtifactLink[];
}

const issueKeyOf = (url: string) => url.match(/\/issue\/([A-Z][A-Z0-9]*-\d+)/)?.[1] ?? null;

/** Tasks sit under their epic; an epic mentioned on its own becomes the header instead of a separate row. */
const IssueGroups = ({ links }: { links: IArtifactLink[] }) => {
  const urls = useMemo(() => links.map((link) => link.url), [links]);
  const metas = useLinkMetas(urls);
  const { epics, loose } = useMemo(() => {
    const groups = new Map<string, IEpicGroup>();
    const rest: IArtifactLink[] = [];
    for (const link of links) {
      const meta = metas.get(link.url);
      const parent = meta?.kind === 'issue' ? meta.parent : null;
      if (!parent) {
        rest.push(link);
        continue;
      }
      const group = groups.get(parent.key) ?? {
        key: parent.key,
        epic: {
          url: `${new URL(link.url).origin}/issue/${parent.key}`,
          label: parent.title,
          timestamp: link.timestamp,
          count: 0,
          kind: link.kind,
          group: 'issue',
        },
        tasks: [],
      };
      group.tasks.push(link);
      groups.set(parent.key, group);
    }
    const loose = rest.filter((link) => {
      const own = groups.get(issueKeyOf(link.url) ?? '');
      if (own) own.epic = link;
      return !own;
    });
    return { epics: [...groups.values()], loose };
  }, [links, metas]);
  return (
    <>
      {epics.map((group) => (
        <li key={group.key} className="tw-epic">
          <ul className="tw-artifact-list">
            <ArtifactRow link={group.epic} />
          </ul>
          <ul className="tw-artifact-list tw-epic-tasks">
            {group.tasks.map((link) => (
              <ArtifactRow key={link.url} link={link} nested />
            ))}
          </ul>
        </li>
      ))}
      {loose.map((link) => (
        <ArtifactRow key={link.url} link={link} />
      ))}
    </>
  );
};

const ArtifactsTab = ({ links, onOpenDoc }: IArtifactsTabProps) => {
  const grouped = useMemo(() => {
    const byGroup: Record<TArtifactGroup, IArtifactLink[]> = { artifact: [], mr: [], issue: [], other: [] };
    for (const link of links) byGroup[link.group].push(link);
    byGroup.artifact.sort((a, b) => Number(b.kind === 'artifact') - Number(a.kind === 'artifact'));
    return byGroup;
  }, [links]);
  const products = links.length - grouped.other.length - grouped.mr.length;

  return (
    <div className="tw-artifacts">
      {products === 0 && (
        <div className="tw-inspector-empty tw-artifacts-empty">
          <Sparkles />
          <p>{t('inspector.artifactsTab.empty')}</p>
        </div>
      )}
      {GROUPS.map(
        ({ id, title }) =>
          grouped[id].length > 0 && (
            <section key={id} className={cn('tw-artifact-group', `tw-artifact-group-${id}`)}>
              <h3 className="tw-inspector-heading">
                {title} <span>{grouped[id].length}</span>
              </h3>
              <ul className="tw-artifact-list">
                {id === 'issue' ? (
                  <IssueGroups links={grouped.issue} />
                ) : (
                  grouped[id].map((link) => <ArtifactRow key={link.url} link={link} onOpenDoc={onOpenDoc} />)
                )}
              </ul>
            </section>
          ),
      )}
      {grouped.other.length > 0 && (
        <details className="tw-artifact-more">
          <summary className="tw-inspector-heading">
            <ChevronRight className="tw-artifact-more-chevron" />
            {t('inspector.artifactsTab.otherLinks')} <span>{grouped.other.length}</span>
          </summary>
          <ul className="tw-artifact-list">
            {grouped.other.map((link) => (
              <ArtifactRow key={link.url} link={link} />
            ))}
          </ul>
        </details>
      )}
    </div>
  );
};

export default memo(ArtifactsTab);
