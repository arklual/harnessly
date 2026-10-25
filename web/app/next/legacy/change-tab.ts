// How the tab of a change (merge request, pull request, commit, issue) looks in the pane's strip. Part of the bridge:
// it goes with it when Legacy goes.
import { changeTitleOf, gitlabRefOf } from '@/lib/hl-gitlab-ref';

const SHA_LENGTH = 8;
const ICON = { mr: 'git-pull-request', issue: 'circle-dot', commit: 'git-pull-request' } as const;

export interface IChangeLook {
  name: string;
  icon: string;
}

/**
 * The name and icon of a tab by the link it shows: `repo #12` for an issue or a pull request, `repo !12` for a merge
 * request, the short sha for a commit; null for a tab that does not show a change. An issue is told from a merge request by
 * its icon. The address alone would name the tab by its last part («1»).
 */
export const changeLookOf = (target: string | null): IChangeLook | null => {
  const ref = target ? gitlabRefOf(target) : null;
  return ref ? { name: changeTitleOf(ref, SHA_LENGTH), icon: ICON[ref.kind] } : null;
};
