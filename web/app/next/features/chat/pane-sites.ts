// Which links of the chat open in the pane's browser and which in the person's own browser. «Our» sites (the
// installation's documents, boards, stands, tracker, GitLab and what the deployment adds) open in the pane, where the
// person is signed in and can show the agent a place; any other site opens in a new tab of the browser.
//
// A site is written `host`, `*.domain` (any subdomain, not the domain itself) or `host/path` (that path and below it).
import { appConfig, type IAppConfig } from '@/platform/config';

export const LINK_PLACE = { pane: 'pane', outside: 'outside' } as const;
export type TLinkPlace = (typeof LINK_PLACE)[keyof typeof LINK_PLACE];

const LABEL = '[a-z0-9](?:[a-z0-9-]*[a-z0-9])?';
const HOST = new RegExp(String.raw`^(?:\*\.)?${LABEL}(?:\.${LABEL})*(?::\d{1,5})?$`);
const PATH = /^(?:\/[\w.~%@!$&'()*+,;=:-]+)+$/;
const LOCAL = /^(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])$/;
// A public GitLab is a site like any other; its merge requests still open in the pane by their chips.
const PUBLIC_HOSTS = new Set(['gitlab.com', 'github.com']);

/** A site as the person typed it, in its one written form (`https://Posthog.x.ru/` is `posthog.x.ru`); null when it
 *  is not one. */
export const siteOf = (text: string): string | null => {
  const raw = text
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, '');
  if (!raw) return null;
  const slash = raw.indexOf('/');
  const host = slash < 0 ? raw : raw.slice(0, slash);
  const path =
    slash < 0
      ? ''
      : raw
          .slice(slash)
          .replace(/[?#].*$/, '')
          .replace(/\/+$/, '');
  if (!HOST.test(host)) return null;
  if (path && (host.startsWith('*.') || !PATH.test(path))) return null;
  return host + path;
};

const hostOf = (url: string | null): string | null => {
  if (!url) return null;
  try {
    return new URL(url.replace('{slug}', '')).host.toLowerCase();
  } catch {
    return null;
  }
};

// The reader shows a document at https://<platform>/agents/docs/<slug>: its path is ours, not the whole platform (that
// is this page itself).
const readerSite = (readerUrl: string | null): string | null => {
  if (!readerUrl) return null;
  try {
    const u = new URL(readerUrl.replace('{slug}', ''));
    return siteOf(`${u.host}${u.pathname}`);
  } catch {
    return null;
  }
};

/** The sites the installation itself names, in order and without repeats: they open in the pane until the person
 *  takes one off. */
export const defaultSites = (config: IAppConfig = appConfig()): string[] => {
  const out: (string | null)[] = [
    hostOf(config.artifactsUrl),
    hostOf(config.boardsUrl),
    readerSite(config.readerUrl),
    config.standDomain ? siteOf(`*.${config.standDomain.replace(/^\.+/, '')}`) : null,
    ...config.trackerHosts.map(siteOf),
    ...config.gitlabHosts.filter((h) => !PUBLIC_HOSTS.has(h.toLowerCase())).map(siteOf),
    ...config.paneHosts.map(siteOf),
    ...config.defaultPins.map((pin) => siteOf(pin.text)),
  ];
  return [...new Set(out.filter((s): s is string => s !== null))];
};

/** Whether a site covers an address: its host (any subdomain for `*.`) and, when the site has a path, that path or
 *  one below it. */
export const siteCovers = (site: string, url: URL): boolean => {
  const slash = site.indexOf('/');
  const host = slash < 0 ? site : site.slice(0, slash);
  const path = slash < 0 ? '' : site.slice(slash);
  const have = url.host.toLowerCase();
  const hostOk = host.startsWith('*.') ? have.endsWith(host.slice(1)) : have === host;
  if (!hostOk) return false;
  if (!path) return true;
  const p = url.pathname.toLowerCase().replace(/\/+$/, '');
  return p === path || p.startsWith(`${path}/`);
};

/** Where a link opens on a plain click: a site of the list or a server of the machine (localhost) in the pane; any
 *  other http(s) address outside. What is no address (a path, a key the runtime names) stays the pane's. */
export const linkPlace = (href: string, sites: readonly string[]): TLinkPlace => {
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return LINK_PLACE.pane;
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return LINK_PLACE.outside;
  if (LOCAL.test(url.hostname)) return LINK_PLACE.pane;
  return sites.some((site) => siteCovers(site, url)) ? LINK_PLACE.pane : LINK_PLACE.outside;
};

/** Whether the person's own browser can open the link: an address, not a server of the machine (only the pane
 *  reaches those). */
export const opensOutside = (href: string | null): href is string => {
  if (!href) return false;
  try {
    const url = new URL(href);
    return url.protocol !== 'javascript:' && url.protocol !== 'data:' && !LOCAL.test(url.hostname);
  } catch {
    return false;
  }
};

/** Opens a link in a new tab of the person's browser. */
export const openOutside = (href: string): void => {
  window.open(href, '_blank', 'noopener,noreferrer');
};
