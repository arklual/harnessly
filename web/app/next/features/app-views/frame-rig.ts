// A test rig of a view in its frame: a real DOM (jsdom) for the document, the real bridge for the host, and the real
// bridge shim (internal/apps/sdk/bridge-shim.js) between them. The document sees the frame the way a browser shows it:
// window.parent is the host's window, a message from it carries the host's origin and the ports it handed over.
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { AppBridge } from '@/next/features/app-views/bridge';

export const SLOT = 'https://slot6.example.org';
export const HOST = 'https://app.example.org';
export const NONCE = 'nonce-of-the-session';

const here = new URL('.', import.meta.url);
const sdkDir = new URL('../../../../../internal/apps/sdk/', here);
export const SHIM_SRC = readFileSync(new URL('bridge-shim.js', sdkDir), 'utf8');
const SDK_SRC = readFileSync(new URL('app-sdk.js', sdkDir), 'utf8');

/** The SDK as the view server serves it to a view: a classic script that publishes window.harnessly (views.go classicSDK). */
export const classicSdk = (): string =>
  `(function () {\n${SDK_SRC.replaceAll('export async function', 'async function').replaceAll('export function', 'function').replaceAll('export class', 'class')}\nglobalThis.harnessly = { connect: connect, HarnesslyError: HarnesslyError, state: state, action: action, screen: screen };\n})();\n`;

/** What the view server does to a page: the nonce's meta and the shim's script first in <head> (views.go injectNonce). */
export const servePage = (html: string, nonce: string | null): string => {
  const meta = nonce === null ? '' : `<meta name="hl-nonce" content="${nonce}">`;
  return html.replace(/<head[^>]*>/i, (open) => `${open}${meta}<script src="/__hl/bridge-shim.js"></script>`);
};

/** The frame: documents come and go in it (a navigation), the bridge stays the one the host made for the first. */
export class FrameRig {
  /** The window object the host holds for the frame; it stays the same while the documents in it change. */
  readonly proxy: { postMessage: (data: unknown, origin: string, transfer?: unknown[]) => void };
  readonly hostWindow: { postMessage: (data: unknown, origin?: string) => void };
  current: JSDOM | null = null;
  bridge!: AppBridge;
  /** What each document posted to the host's window (before the port: only the handshake). */
  readonly onWindow: unknown[] = [];

  constructor() {
    this.proxy = {
      postMessage: (data, _origin, transfer = []) => {
        const dom = this.current;
        if (dom) queueMicrotask(() => this.dispatch(dom, data, transfer));
      },
    };
    this.hostWindow = {
      postMessage: (data) => {
        this.onWindow.push(data);
        queueMicrotask(() => this.bridge.onMessage({ source: this.proxy, origin: SLOT, data }));
      },
    };
  }

  private dispatch(dom: JSDOM, data: unknown, ports: unknown[]) {
    const event = new dom.window.Event('message');
    Object.defineProperties(event, {
      data: { value: data },
      origin: { value: HOST },
      source: { value: this.hostWindow },
      ports: { value: ports },
    });
    dom.window.dispatchEvent(event);
  }

  /** A document loads in the frame (the shim runs first, then `scripts`); the one before it is gone. */
  load(html: string, nonce: string | null, scripts: string[] = []): JSDOM {
    this.current?.window.close();
    const dom = new JSDOM(servePage(html, nonce), {
      runScripts: 'outside-only',
      pretendToBeVisual: true,
      url: `${SLOT}/page`,
    });
    Object.defineProperty(dom.window, 'parent', {
      value: this.hostWindow,
      writable: true,
      configurable: true,
    });
    this.current = dom;
    // jsdom has no layout, so no ResizeObserver; a browser has one (the views watch their size with it).
    Object.defineProperty(dom.window, 'ResizeObserver', {
      value: class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    });
    dom.window.eval(SHIM_SRC);
    for (const src of scripts) dom.window.eval(src);
    return dom;
  }
}

export const settle = (ms = 30) => new Promise((r) => setTimeout(r, ms));
