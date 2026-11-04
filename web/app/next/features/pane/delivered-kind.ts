// How the pane shows a file that is not text. Pure: the pane's tab placement reads it too.

export const DELIVERED_VIEW = {
  zip: 'zip',
  image: 'image',
  pdf: 'pdf',
  video: 'video',
  audio: 'audio',
  sheet: 'sheet',
  docx: 'docx',
  pptx: 'pptx',
  font: 'font',
  save: 'save',
} as const;
export type DeliveredView = (typeof DELIVERED_VIEW)[keyof typeof DELIVERED_VIEW];

// An svg is text: it opens in the editor as a picture with its code a click away.
const IMAGES = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'ico']);
const FONTS = new Set(['ttf', 'otf', 'woff', 'woff2']);
/** Archives whose contents the pane lists: zip, and tar plain or gzipped. */
const ARCHIVES = new Set(['zip', 'tar', 'tgz']);
const VIDEOS = new Set(['mp4', 'm4v', 'webm', 'mov', 'mkv', 'ogv']);
const SHEETS = new Set(['xlsx', 'xlsm', 'csv', 'tsv']);
const DOCUMENTS = new Set(['docx', 'docm']);
const DECKS = new Set(['pptx', 'pptm']);
const SOUNDS = new Set(['mp3', 'wav', 'm4a', 'aac', 'ogg', 'oga', 'opus', 'flac']);
/** Files the pane cannot show as text and has no preview for: they are saved. */
const SAVED = new Set([
  'doc',
  'xls',
  'ppt',
  'odt',
  'ods',
  'gz',
  '7z',
  'rar',
  'avi',
  'wmv',
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
  if (ARCHIVES.has(ext) || /\.tar\.gz$/i.test(path)) return DELIVERED_VIEW.zip;
  if (FONTS.has(ext)) return DELIVERED_VIEW.font;
  if (ext === 'pdf') return DELIVERED_VIEW.pdf;
  if (IMAGES.has(ext)) return DELIVERED_VIEW.image;
  if (VIDEOS.has(ext)) return DELIVERED_VIEW.video;
  if (SOUNDS.has(ext)) return DELIVERED_VIEW.audio;
  if (SHEETS.has(ext)) return DELIVERED_VIEW.sheet;
  if (DOCUMENTS.has(ext)) return DELIVERED_VIEW.docx;
  if (DECKS.has(ext)) return DELIVERED_VIEW.pptx;
  if (SAVED.has(ext)) return DELIVERED_VIEW.save;
  return null;
};
