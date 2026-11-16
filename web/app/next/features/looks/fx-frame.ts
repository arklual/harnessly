// The document a live effect of a look runs in (docs/looks.md, «Живые эффекты»): a sandboxed frame with no
// allow-same-origin (an opaque origin: no cookies, no storage, no reach into the page), a policy with no network at all,
// and two scripts — the hx SDK (fx/runtime.js) and the effect. The effect's code is the person's own or a built-in one;
// whatever it does, all it sees is what fx-layer.tsx posts it and all it can change is its own transparent canvas.
import runtime from '@/next/features/looks/fx/runtime.js?raw';
import snowplay from '@/next/features/looks/fx/snowplay.js?raw';
import nightlamp from '@/next/features/looks/fx/nightlamp.js?raw';
import laser from '@/next/features/looks/fx/laser.js?raw';
import { sendBlob } from '@/next/core/net/http';
import { clusterApi } from '@/next/core/net/machine';
import type { TFxPreset } from '@/next/features/looks/types';

export const FX_SANDBOX = 'allow-scripts';

/** The code of the built-in effects. */
export const FX_CODE: Record<TFxPreset, string> = { snowplay, nightlamp, laser };

/** The first line a script kept by /api/v2/me/looks starts with (internal/wire/looks.go). */
export const FX_MAGIC = '//hx-fx v1\n';
/** The largest effect a person uploads, in bytes (the server takes the magic line on top). */
export const MAX_FX = 256 * 1024 - FX_MAGIC.length;

export const fxPolicy = (nonce: string): string =>
  [
    "default-src 'none'",
    'img-src data: blob:',
    'font-src data:',
    "style-src 'unsafe-inline'",
    `script-src 'nonce-${nonce}'`,
    "form-action 'none'",
    "base-uri 'none'",
  ].join('; ');

/** Code put inside a <script>: nothing in it can close the element or open a comment the parser would honour. */
export const inScript = (code: string): string =>
  code.replace(/<\/(script)/gi, '<\\/$1').replace(/<!--/g, '<\\!--');

export const fxDocument = (code: string, nonce: string): string =>
  '<!doctype html><html><head><meta charset="utf-8">' +
  `<meta http-equiv="Content-Security-Policy" content="${fxPolicy(nonce)}">` +
  '<style>html,body{margin:0;padding:0;background:transparent;overflow:hidden}canvas{display:block}</style>' +
  `</head><body><script nonce="${nonce}">${inScript(runtime)}</script>` +
  `<script nonce="${nonce}">${inScript(code.startsWith(FX_MAGIC) ? code.slice(FX_MAGIC.length) : code)}\n</script>` +
  '</body></html>';

/** Keeps an effect's code with the person's pictures (internal/wire/looks.go): its hash, or null. */
export const uploadFx = async (code: string): Promise<string | null> => {
  if (new TextEncoder().encode(code).length > MAX_FX) return null;
  const res = await sendBlob(
    clusterApi('/me/looks'),
    new Blob([FX_MAGIC + code], { type: 'text/plain' }),
    (v) =>
      typeof v === 'object' && v !== null && typeof (v as { hash?: unknown }).hash === 'string'
        ? (v as { hash: string }).hash
        : null,
    30000,
  );
  return 'ok' in res ? res.value : null;
};
