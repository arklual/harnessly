// What a person types as an address in the pane: «4400» or «:4400» (a port of this machine), «localhost:3000/x»,
// «stand-2.preview.example.com/adminpanel», «https://…». A port opens in a port slot, anything else as a site.
import type { TPreviewTarget } from '@/next/core/net/preview';

export interface IAddress {
  target: TPreviewTarget;
  path: string;
  /** As the address bar shows it: without the scheme. */
  text: string;
}

const LOCAL = /^(localhost|127\.0\.0\.1)$/i;
const HOST = /^(localhost|\d{1,3}(\.\d{1,3}){3}|[a-z0-9-]+(\.[a-z0-9-]+)+)(:\d{2,5})?$/i;

export const parseAddress = (raw: string): IAddress | null => {
  const typed = raw.trim();
  const bare = typed.replace(/^:/, '');
  if (/^\d{2,5}$/.test(bare)) {
    const port = Number(bare);
    if (port < 1 || port > 65535) return null;
    return { target: { kind: 'port', port }, path: '/', text: `localhost:${port}` };
  }
  const secure = !/^http:\/\//i.test(typed);
  const rest = typed.replace(/^https?:\/\//i, '');
  const slash = rest.indexOf('/');
  const host = slash < 0 ? rest : rest.slice(0, slash);
  const path = slash < 0 ? '/' : rest.slice(slash);
  if (!HOST.test(host) || /\s/.test(path)) return null;
  const [name, portText] = host.split(':');
  const text = `${host}${path === '/' ? '' : path}`;
  if (LOCAL.test(name)) {
    if (!portText) return null;
    return { target: { kind: 'port', port: Number(portText) }, path, text };
  }
  return {
    target: { kind: 'url', origin: `${secure ? 'https' : 'http'}://${host.toLowerCase()}` },
    path,
    text,
  };
};
