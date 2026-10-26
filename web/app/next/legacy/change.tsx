// The tab of a change (merge request, pull request, commit, issue) as Legacy draws it, for the gallery that shows it on
// its own (next/dev/change-gallery.tsx). The pane itself gets it through `usePaneContents`.
import '@/styles/legacy';
import GitLabView from '@/components/agents/inspector/gitlab-view';
import { changeTitleOf, gitlabRefOf } from '@/lib/hl-gitlab-ref';

const SHA_LENGTH = 8;

export const ChangeTab = ({ url }: { url: string }) => (
  <GitLabView url={url} sessionName="gallery" tabId="gallery" />
);

/** What the tab of a link is called, or the link itself when it is not a change. */
export const changeTabName = (url: string): string => {
  const ref = gitlabRefOf(url);
  return ref ? changeTitleOf(ref, SHA_LENGTH) : url;
};
