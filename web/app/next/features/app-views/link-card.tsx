// A link an app on in the chat claims (chat.links of its manifest), alone in its paragraph: the link stays, and the
// app's card view stands under it with the link as its data ({url}, without its query and fragment). The apps are read
// only for such paragraphs.
import type { ReactNode } from 'react';
import AppFrame from '@/next/features/app-views/app-frame';
import { useInstalledApps, type IInstalledApp } from '@/next/features/app-views/api';
import { useAppViewHost } from '@/next/features/app-views/host';
import { useConversation } from '@/next/stores/conversations';

/**
 * Whether a link kind of a manifest (https://<host>/<path>: the host may start with `*.`, which takes the host itself
 * and its subdomains; the path may end with `*`; no path takes every path) names the address.
 */
export const linkMatches = (match: string, href: string): boolean => {
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return false;
  }
  if (url.protocol !== 'https:' || !match.startsWith('https://')) return false;
  const rest = match.slice('https://'.length);
  const slash = rest.indexOf('/');
  const host = slash === -1 ? rest : rest.slice(0, slash);
  const path = slash === -1 ? null : rest.slice(slash);
  const sub = host.startsWith('*.');
  const base = sub ? host.slice(2) : host;
  if (url.port !== '') return false;
  if (!(url.hostname === base || (sub && url.hostname.endsWith(`.${base}`)))) return false;
  if (path === null) return true;
  return path.endsWith('*') ? url.pathname.startsWith(path.slice(0, -1)) : url.pathname === path;
};

/** What the card is given: the address without its query and fragment, where tokens and sessions ride. */
export const withoutQuery = (href: string): string => {
  try {
    const url = new URL(href);
    return `${url.origin}${url.pathname}`;
  } catch {
    return '';
  }
};

/** The first app on in the chat (in the chat's order) whose link kinds name the address, with the card view. */
export const appForLink = (
  installed: readonly IInstalledApp[],
  on: readonly string[],
  href: string,
): { app: IInstalledApp; view: IInstalledApp['views'][number] } | null => {
  for (const id of on) {
    const app = installed.find((a) => a.id === id);
    const link = app?.links?.find((l) => linkMatches(l.match, href));
    const view = link && app?.views.find((v) => v.id === link.card && v.targets.includes('chat'));
    if (app && view) return { app, view };
  }
  return null;
};

export const AppLinkParagraph = ({ href, children }: { href: string; children: ReactNode }) => {
  const host = useAppViewHost();
  const on = useConversation(host?.conversation ?? null)?.apps ?? [];
  const { list } = useInstalledApps(host?.machine ?? 'local', 0, host !== null && on.length > 0);
  const found = host && list ? appForLink(list.apps, on, href) : null;
  return (
    <>
      <p>{children}</p>
      {host && list && found && (
        <div className="hx-appcard">
          <AppFrame
            machine={host.machine}
            app={found.app}
            view={found.view}
            target="chat"
            viewPort={list.viewPort}
            conversation={host.conversation}
            data={{ url: withoutQuery(href) }}
            fit
          />
        </div>
      )}
    </>
  );
};
