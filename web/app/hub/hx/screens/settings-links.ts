// The form of «Ссылки в чате»: the organization's integrations as the fields show them, and back.
import type { OrgLinks } from '../../../hub-api/orgs';

export type LinksForm = {
  standDomain: string;
  trackerUrl: string;
  gitlabHosts: string;
  artifactsUrl: string;
  boardsUrl: string;
};

export const formOfLinks = (links: OrgLinks): LinksForm => ({
  standDomain: links.standDomain ?? '',
  trackerUrl: links.trackerUrl ?? '',
  gitlabHosts: (links.gitlabHosts ?? []).join(', '),
  artifactsUrl: links.artifactsUrl ?? '',
  boardsUrl: links.boardsUrl ?? '',
});

const hostsOf = (text: string): string[] =>
  text
    .split(/[\s,]+/)
    .map((h) => h.trim())
    .filter((h) => h !== '');

/** What goes to the hub: an empty field is not named; the hub checks the addresses and brings them to one form. */
export const linksOfForm = (form: LinksForm): OrgLinks => {
  const out: OrgLinks = {};
  if (form.standDomain.trim() !== '') out.standDomain = form.standDomain.trim();
  if (form.trackerUrl.trim() !== '') out.trackerUrl = form.trackerUrl.trim();
  const hosts = hostsOf(form.gitlabHosts);
  if (hosts.length > 0) out.gitlabHosts = hosts;
  if (form.artifactsUrl.trim() !== '') out.artifactsUrl = form.artifactsUrl.trim();
  if (form.boardsUrl.trim() !== '') out.boardsUrl = form.boardsUrl.trim();
  return out;
};

/** True when the fields say something other than the saved form says. */
export const linksChanged = (form: LinksForm, saved: LinksForm): boolean =>
  JSON.stringify(linksOfForm(form)) !== JSON.stringify(linksOfForm(saved));
