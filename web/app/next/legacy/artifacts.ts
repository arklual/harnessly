// The links of a conversation as the Artifacts tool of the new pane takes them (features/artifacts): Legacy's classes
// of link (lib/presented-link, lib/hl-artifacts) turned into plain items, and where a click on one opens. Part of the
// bridge: it goes with it when Legacy goes.
import type { MouseEvent } from 'react';
import { isPlainClick } from '@/lib/image-link';
import { paneRequestOf } from '@/lib/hl-pane-link';
import type { IArtifactLink } from '@/lib/hl-artifacts';
import { hasLinkMeta, presentLink } from '@/lib/presented-link';
import { githubRefOf } from '@/lib/site';
import { ARTIFACT_KIND, type IArtifactItem, type TArtifactKind } from '@/next/features/artifacts/model';

const kindOf = (link: IArtifactLink, board: boolean): TArtifactKind => {
  if (link.group === 'issue') return ARTIFACT_KIND.task;
  if (link.group === 'mr') return ARTIFACT_KIND.mr;
  if (link.group === 'artifact') return board ? ARTIFACT_KIND.board : ARTIFACT_KIND.doc;
  return ARTIFACT_KIND.link;
};

export const artifactItemOf = (link: IArtifactLink): IArtifactItem => {
  const presented = presentLink(link.url);
  const kind = kindOf(link, presented.board === true);
  return {
    url: link.url,
    kind,
    key: kind === ARTIFACT_KIND.task || kind === ARTIFACT_KIND.mr ? presented.label : null,
    label: link.label,
    fallback: presented.label,
    where: presented.detail,
    github: githubRefOf(link.url) !== null,
    readable: hasLinkMeta(presented.kind),
    at: link.timestamp,
  };
};

/** Where a row opens: a tab of the pane for what has one (a document, a task, a merge request, a stand), the browser's
 *  own new tab for the rest and for a click with a modifier. */
export const artifactRequestOf = (
  item: IArtifactItem,
  title: string | null,
  event: MouseEvent<HTMLAnchorElement>,
) => (isPlainClick(event.nativeEvent) ? paneRequestOf(item.url, title ?? item.label) : null);
