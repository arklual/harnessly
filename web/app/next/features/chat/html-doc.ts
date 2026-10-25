// The document an agent's ```html answer is shown in: a sandboxed frame that cannot reach the page, the cookies or the
// runtime. The sandbox has no allow-same-origin (the frame is an opaque origin) and no popups, forms or navigation of the
// top page; the policy below leaves it only inline styles and data: pictures and fonts, no network at all, and one
// script, ours, which reports the height so the frame can fit its content. The agent's own scripts do not run.

export const FRAME_SANDBOX = 'allow-scripts';

/** The message the frame's script posts to the page: its height, tagged with the frame's key. */
export interface IFrameHeight {
  hxFrame: string;
  height: number;
}

export const isFrameHeight = (data: unknown, key: string): data is IFrameHeight =>
  typeof data === 'object' &&
  data !== null &&
  (data as IFrameHeight).hxFrame === key &&
  typeof (data as IFrameHeight).height === 'number' &&
  Number.isFinite((data as IFrameHeight).height);

export const policyOf = (nonce: string): string =>
  [
    "default-src 'none'",
    'img-src data: blob:',
    'media-src data:',
    'font-src data:',
    "style-src 'unsafe-inline'",
    `script-src 'nonce-${nonce}'`,
    "form-action 'none'",
    "base-uri 'none'",
  ].join('; ');

export const newNonce = (): string => {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
};

// The page is drawn on a light canvas in either theme: an agent's page names the colours it wants for its own boxes and
// leaves the rest to the defaults, which it expects to be dark text on white.
const BASE =
  ':where(html){color-scheme:light;background:#fff;color:#1a1a1f;font:14px/1.5 system-ui,sans-serif}:where(body){margin:12px}';

// Reads the height after layout and when anything resizes; the key is a plain token, so it needs no escaping.
const reporter = (key: string): string =>
  `const send=()=>parent.postMessage({hxFrame:${JSON.stringify(key)},height:Math.ceil(document.documentElement.getBoundingClientRect().height)},'*');` +
  'new ResizeObserver(send).observe(document.documentElement);addEventListener("load",send);send();';

const FORBIDDEN = 'meta, base, form, iframe, frame, frameset, object, embed, link, script, noscript, portal';

/**
 * What the agent wrote, parsed and stripped of what can steer the frame: its own policy or refresh, a base address,
 * forms, nested documents, linked files and scripts; links lose their target. Styles and markup stay.
 */
export const cleanHtml = (html: string): string => {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  doc.querySelectorAll(FORBIDDEN).forEach((node) => node.remove());
  doc.querySelectorAll('[target], [ping], [formaction]').forEach((node) => {
    node.removeAttribute('target');
    node.removeAttribute('ping');
    node.removeAttribute('formaction');
  });
  return doc.head.innerHTML + doc.body.innerHTML;
};

/** The whole srcdoc: our policy and base styles first, so nothing the agent wrote can come before them. */
export const frameDocument = (
  html: string,
  { key, nonce, fit }: { key: string; nonce: string; fit: boolean },
): string => {
  return (
    '<!doctype html><html><head><meta charset="utf-8">' +
    `<meta http-equiv="Content-Security-Policy" content="${policyOf(nonce)}">` +
    `<style>${BASE}</style>` +
    (fit ? `<script nonce="${nonce}">${reporter(key)}</script>` : '') +
    `</head><body>${cleanHtml(html)}</body></html>`
  );
};
