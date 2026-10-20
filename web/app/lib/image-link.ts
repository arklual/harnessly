import { t } from '@/lib/i18n';
const IMAGE_PATH = /\.(?:png|jpe?g|gif|webp|avif|bmp)$/i;

export const isImageHref = (href: string): boolean => {
  if (!href) return false;
  try {
    return IMAGE_PATH.test(new URL(href, 'http://local').pathname);
  } catch {
    return false;
  }
};

interface IClickModifiers {
  button: number;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}

/** A plain click opens the picture in the viewer; a modified or middle click still means «new tab». */
export const isPlainClick = (event: IClickModifiers): boolean =>
  event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;

export const linkImageName = (href: string, text: string): string => {
  const label = text.trim();
  if (label && label !== href) return label;
  const file = new URL(href, 'http://local').pathname.split('/').at(-1) ?? '';
  try {
    return decodeURIComponent(file) || t('lib.imageLink.image');
  } catch {
    return file || t('lib.imageLink.image');
  }
};
