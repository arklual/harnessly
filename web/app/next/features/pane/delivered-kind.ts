// How the pane shows a file that is not text. Pure: the pane's tab placement reads it too.

export const DELIVERED_VIEW = { zip: 'zip', image: 'image', pdf: 'pdf', save: 'save' } as const;
export type DeliveredView = (typeof DELIVERED_VIEW)[keyof typeof DELIVERED_VIEW];

const IMAGES = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg']);
/** Files the pane cannot show as text and has no preview for: they are saved. */
const SAVED = new Set([
  'docx',
  'doc',
  'xlsx',
  'xls',
  'pptx',
  'ppt',
  'odt',
  'ods',
  'gz',
  'tgz',
  'tar',
  '7z',
  'rar',
  'mp4',
  'mov',
  'webm',
  'mp3',
  'wav',
  'bin',
  'exe',
  'dmg',
]);

const extOf = (path: string) => {
  const name = path.slice(path.lastIndexOf('/') + 1);
  const dot = name.lastIndexOf('.');
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : '';
};

/** How the pane shows this file when it is not text; null for a file the editor opens (md, html, code, text). */
export const deliveredViewOf = (path: string): DeliveredView | null => {
  const ext = extOf(path);
  if (ext === 'zip') return DELIVERED_VIEW.zip;
  if (ext === 'pdf') return DELIVERED_VIEW.pdf;
  if (IMAGES.has(ext)) return DELIVERED_VIEW.image;
  if (SAVED.has(ext)) return DELIVERED_VIEW.save;
  return null;
};
