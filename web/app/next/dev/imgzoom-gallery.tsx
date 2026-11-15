// The picture viewer's zoom with the person's own marks on a made-up picture: ?dev=imgzoom. The picture is
// a wide strip of small «screenshots» drawn here, so no machine and no files are needed. ?theme=light|dark&lang=en|ru
// pick the look, ?tool=frame|arrow|text|pen|crop opens with that tool on. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/image-edit.css';
import '@/next/styles/motion.css';

import { useEffect, useMemo, useState } from 'react';
import { pinLocale } from '@/next/i18n';
import { useTheme } from '@/platform/theme';
import ImageView from '@/next/ui/image-view';
import { useMarkup } from '@/next/features/image-edit/use-markup';
import { MARKUP_TOOL, type TMarkupTool } from '@/next/features/image-edit/markup';

const params = new URLSearchParams(window.location.search);
const W = 4800;
const H = 640;

/** Six «screens» side by side, each with a bar, a few lines of small words and a button. */
const drawStrip = (): string => {
  const canvas = document.createElement('canvas');
  canvas.width = W;
  canvas.height = H;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';
  ctx.fillStyle = '#e9e7f2';
  ctx.fillRect(0, 0, W, H);
  const card = W / 6;
  for (let i = 0; i < 6; i++) {
    const x = i * card + 24;
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(x, 24, card - 48, H - 48);
    ctx.fillStyle = `hsl(${(i * 55 + 250) % 360} 60% 55%)`;
    ctx.fillRect(x, 24, card - 48, 56);
    ctx.fillStyle = '#ffffff';
    ctx.font = '600 22px system-ui, sans-serif';
    ctx.fillText(`Экран ${i + 1} — настройки`, x + 18, 60);
    ctx.fillStyle = '#2a2833';
    ctx.font = '13px system-ui, sans-serif';
    for (let line = 0; line < 22; line++)
      ctx.fillText(
        `${line + 1}. Пункт списка с мелким текстом, который не прочитать без увеличения`,
        x + 18,
        112 + line * 20,
      );
    ctx.fillStyle = '#7c5cff';
    ctx.fillRect(x + 18, H - 92, 160, 40);
    ctx.fillStyle = '#ffffff';
    ctx.font = '600 15px system-ui, sans-serif';
    ctx.fillText('Сохранить', x + 58, H - 66);
  }
  return canvas.toDataURL('image/png');
};

const TOOLS: readonly string[] = Object.values(MARKUP_TOOL);

const Viewer = ({ src }: { src: string }) => {
  const [open, setOpen] = useState(true);
  const tool = params.get('tool');
  const markup = useMarkup({
    src,
    name: 'screens.png',
    target: { replacing: false, give: () => undefined },
    natural: { w: W, h: H },
    onClose: () => setOpen(false),
    startTool: tool !== null && TOOLS.includes(tool) ? (tool as TMarkupTool) : undefined,
  });
  return (
    <ImageView
      item={open ? { src, name: 'screens.png' } : null}
      onClose={() => setOpen(false)}
      position={{ at: 1, of: 2 }}
      onStep={() => undefined}
      edit={markup}
    />
  );
};

const ImgZoomGallery = () => {
  const { setTheme } = useTheme();
  useEffect(() => {
    const theme = params.get('theme');
    if (theme === 'dark' || theme === 'light') setTheme(theme);
  }, [setTheme]);
  pinLocale(params.get('lang') === 'en' ? 'en' : 'ru');
  const src = useMemo(() => drawStrip(), []);
  return <Viewer src={src} />;
};

export default ImgZoomGallery;
