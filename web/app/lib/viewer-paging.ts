const SWIPE_MIN_PX = 56;
const SWIPE_AXIS_RATIO = 1.5;

export type TPageStep = -1 | 0 | 1;

/** The neighbour of `items[index]` in the given direction, wrapping at the ends; null when there is nowhere to go. */
export const pageTo = <T>(items: readonly T[], index: number, step: -1 | 1): T | null => {
  if (items.length < 2 || index < 0 || index >= items.length) return null;
  return items[(index + step + items.length) % items.length];
};

const KEY_STEPS: Readonly<Record<string, TPageStep>> = { ArrowLeft: -1, ArrowRight: 1 };

export const keyStep = (key: string): TPageStep => KEY_STEPS[key] ?? 0;

const MIME_EXTENSIONS: Readonly<Record<string, string>> = { 'image/jpeg': 'jpg', 'image/svg+xml': 'svg' };

/** A file name for «save as»: the shown name as is when it has an extension, otherwise one taken from the picture's type. */
export const downloadNameOf = (name: string, url: string): string => {
  if (/\.[A-Za-z0-9]{2,5}$/.test(name)) return name;
  const mime = url.match(/^data:(image\/[\w+.-]+)[;,]/)?.[1];
  const extension = mime
    ? (MIME_EXTENSIONS[mime] ?? mime.split('/')[1])
    : (url.split('?')[0].match(/\.([A-Za-z0-9]{2,5})$/)?.[1] ?? 'png');
  return `${name || 'image'}.${extension}`;
};

/** A drag that is long and mostly sideways pages the viewer, unless the picture is zoomed and the drag moves it. */
export const swipeStep = (dx: number, dy: number, scale: number): TPageStep => {
  if (scale > 1 || Math.abs(dx) < SWIPE_MIN_PX || Math.abs(dx) < Math.abs(dy) * SWIPE_AXIS_RATIO) return 0;
  return dx < 0 ? 1 : -1;
};
