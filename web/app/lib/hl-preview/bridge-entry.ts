/**
 * The page bridge as one classic script (`/__tk/bridge.js` of a preview slot), built by vite.bridge.config.ts into
 * dist/preview-bridge.js. The proxy puts its tag first in every document it shows: the parent page's origin, the target's
 * cookie tag and the DevTools conditions come as the tag's attributes.
 */
import * as functions from './bridge-core';

const script = document.currentScript;
const parentOrigin = script?.getAttribute('data-parent') ?? null;
const cookieTag = script?.getAttribute('data-tag') ?? '';
const devtools = script?.getAttribute('data-devtools') ?? null;
const plain = window.location.protocol === 'http:';
const core = {
  ...functions,
  prefixSetCookie: (value: string, tag: string) => functions.prefixSetCookie(value, tag, plain),
};

functions.installCookieShim(document, core, cookieTag);
try {
  functions.installDevtools(window, document, core, devtools);
} catch {
  // The page works without its DevTools record.
}
if (parentOrigin && window.parent !== window) functions.installBridge(window, document, parentOrigin, core);
