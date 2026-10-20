import { isPlainClick } from '@/lib/image-link';
import { paneDocTargetOf } from '@/lib/hl-doc-path';
import { gitlabRefOf } from '@/lib/hl-gitlab-ref';
import { taskKeyOf } from '@/lib/hl-task';
import { OPEN_PANE_TAB_EVENT, type IPaneTabRequest } from '@/lib/hl-pane-tabs';
import { encodePath } from '@/lib/hl-preview-client';
import { openLocalServerInTab, type ILocalServer } from '@/lib/hl-preview-tab';
import { standUrlOf } from '@/lib/hl-stand-url';

const SHORT_SHA = 8;
const LOCAL_HOSTS: ReadonlySet<string> = new Set(['localhost', '127.0.0.1', '0.0.0.0', '[::1]']);

/** `http://localhost:4200/admin?x=1` in a message: a dev server of the machine, with its port written out. */
export const localServerOf = (href: string): ILocalServer | null => {
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return null;
  }
  if (
    (url.protocol !== 'http:' && url.protocol !== 'https:') ||
    url.username ||
    url.password ||
    !LOCAL_HOSTS.has(url.hostname)
  )
    return null;
  const port = Number(url.port);
  if (!url.port || !Number.isInteger(port) || port < 1 || port > 65535) return null;
  try {
    return { port, path: encodePath(`${url.pathname}${url.search}`) };
  } catch {
    return null;
  }
};

/**
 * Where a link opens inside the right panel: a document or a board of the documents and boards services in the reader, a merge
 * request or a commit of GitLab as its diff, a task of the tracker as its card, a feature stand or
 * a dev server of the machine in the browser tab. Null for everything else: it stays a link to a new browser tab.
 */
export const paneRequestOf = (href: string, label: string | null = null): IPaneTabRequest | null => {
  const doc = paneDocTargetOf(href);
  if (doc) return { kind: 'artifacts', target: doc, title: label };
  const task = taskKeyOf(href);
  if (task) return { kind: 'task', target: task, title: task };
  const stand = standUrlOf(href);
  if (stand) return { kind: 'browser', target: stand };
  const local = localServerOf(href);
  if (local)
    return { kind: 'browser', target: `localhost:${local.port}${local.path === '/' ? '' : local.path}` };
  const ref = gitlabRefOf(href);
  if (!ref) return null;
  const repo = ref.project.split('/').at(-1);
  return {
    kind: 'gitlab',
    target: href,
    title: ref.kind === 'mr' ? `${repo} !${ref.iid}` : `${repo} ${ref.sha.slice(0, SHORT_SHA)}`,
  };
};

/** True when a panel on this page took the tab. A page without a panel (a shared read, a report) takes nothing. */
export const requestPaneTab = (request: IPaneTabRequest): boolean => {
  const event = new CustomEvent<IPaneTabRequest>(OPEN_PANE_TAB_EVENT, { detail: request, cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
};

interface ILinkClick {
  nativeEvent: MouseEvent;
  preventDefault: () => void;
}

const MIDDLE_BUTTON = 1;

/** «New tab» in the browser's own words: ⌘, Ctrl or Shift with the primary button, or the middle one. Alt downloads. */
const wantsNewTab = (event: MouseEvent): boolean =>
  event.button === MIDDLE_BUTTON ||
  (event.button === 0 && (event.metaKey || event.ctrlKey || event.shiftKey) && !event.altKey);

/**
 * A plain click on such a link opens it in the panel; no panel leaves the link alone. A modified or middle click is the
 * browser's own «new tab», which is right for a page of the web and a stand (the person's session is their browser's),
 * but not for a dev server: its `localhost` is the person's machine, so it opens as a slot of the preview.
 */
export const openLinkInPane = (event: ILinkClick, href: string, label: string | null = null): void => {
  if (!isPlainClick(event.nativeEvent)) {
    const local = wantsNewTab(event.nativeEvent) ? localServerOf(href) : null;
    if (!local) return;
    event.preventDefault();
    void openLocalServerInTab(local);
    return;
  }
  const request = paneRequestOf(href, label);
  if (request && requestPaneTab(request)) event.preventDefault();
};
