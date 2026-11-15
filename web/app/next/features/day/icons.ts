// The feed may name a glyph for a material, a file or a letter. The interface draws only the glyphs it has
// (styles/icons.css), so a name outside this list gives way to the place's own glyph.
const KNOWN: ReadonlySet<string> = new Set([
  'chart-column',
  'clock',
  'download',
  'file-code',
  'file-text',
  'folder',
  'gauge',
  'git-pull-request',
  'globe',
  'image',
  'layout-dashboard',
  'layout-grid',
  'link',
  'mail',
  'message-square',
  'messages-square',
  'palette',
  'presentation',
  'sparkles',
  'user',
  'users',
]);

export const feedIcon = (name: string | undefined, own: string): string =>
  name !== undefined && KNOWN.has(name) ? name : own;
