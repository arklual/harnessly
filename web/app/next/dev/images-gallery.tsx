// The picture card in every state, on made-up data: ?dev=images. ?theme=light|dark&lang=en|ru pick the look. The
// "pictures" are small SVG data URLs drawn from the path, so no machine and no files are needed. ?scene=<id> shows one box.
// Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/ui.css';
import '@/next/styles/chat.css';
import '@/next/styles/icons.css';
import '@/next/styles/image-card.css';
import '@/next/dev/images-gallery.css';

import { useEffect, useMemo, type ReactNode } from 'react';
import { pinLocale } from '@/next/i18n';
import { useTheme } from '@/platform/theme';
import { ImageCard, ImageHost, imageEntryOf, type IImageHost } from '@/next/features/chat/image-card';
import type { ImageGen, ToolEntry } from '@/next/features/chat/types';
import type { ConvId } from '@/next/protocol/v2';
import type { IEditProgress, IImageEdit, IImageEditHost } from '@/next/features/image-edit/host';

const params = new URLSearchParams(window.location.search);
const SCENE = params.get('scene');

const SIZES = { square: [512, 512], landscape: [768, 512], portrait: [512, 768] } as const;
const HUES = [262, 200, 28, 150, 330, 60, 180, 300] as const;

/**
 * A picture for a path like /g/landscape-2: a gradient with a few soft shapes. What the fake editor made of it is
 * named after it: /g/landscape-2~cut has no background, ~size-4-3 has that shape, each other change turns the hue.
 */
const pictureOf = (path: string): string => {
  const [, , name = 'square-0'] = path.split('/');
  const [base, ...changes] = name.split('~');
  const [shape = 'square', n = '0'] = base.split('-');
  const sized = changes.findLast((change) => change.startsWith('size-'));
  const [w0, h0] = SIZES[shape as keyof typeof SIZES] ?? SIZES.square;
  const [fw = 0, fh = 0] = sized ? sized.slice(5).split('-').map(Number) : [];
  const [w, h] = sized ? [Math.round((h0 * fw) / fh), h0] : [w0, h0];
  const clear = changes.includes('cut');
  const hue = HUES[(Number(n) + changes.length) % HUES.length];
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">` +
    `<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${hue} 80% 62%)"/>` +
    `<stop offset="1" stop-color="hsl(${hue + 50} 70% 30%)"/></linearGradient></defs>` +
    (clear ? '' : `<rect width="${w}" height="${h}" fill="url(#g)"/>`) +
    `<circle cx="${w * 0.7}" cy="${h * 0.3}" r="${Math.min(w, h) * 0.16}" fill="hsl(${hue + 20} 90% 85%)" opacity="0.9"/>` +
    `<path d="M0 ${h} L${w * 0.35} ${h * 0.55} L${w * 0.6} ${h * 0.8} L${w * 0.8} ${h * 0.62} L${w} ${h} Z" fill="hsl(${hue} 60% 18%)" opacity="0.7"/>` +
    `</svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
};

/** The editor on made-up data: a change takes 2.5 s; the words «fail» and «busy» show the failures. */
const FAKE_RUN_MS = 2500;
const runs = new Map<string, { edit: IImageEdit; at: number }>();
const editHost: IImageEditHost = {
  start: async (edit) => {
    if (edit.prompt === 'busy')
      throw new Error('Too many pictures are being drawn at once. Wait for one to finish.');
    const id = `run-${runs.size + 1}`;
    runs.set(id, { edit, at: Date.now() });
    return id;
  },
  read: async (id): Promise<IEditProgress> => {
    const run = runs.get(id);
    if (!run) throw new Error('no such run');
    if (Date.now() - run.at < FAKE_RUN_MS) return { state: 'running', versions: [] };
    const { edit } = run;
    if (edit.prompt === 'fail')
      return { state: 'failed', versions: [], error: 'The picture was refused by the model.' };
    const suffix = edit.mode === 'size' ? `size-${edit.format?.replace(':', '-')}` : edit.mode;
    const path = `${edit.from.id}~${suffix}`;
    return { state: 'done', versions: [{ id: path, src: pictureOf(path), clear: edit.mode === 'cut' }] };
  },
  versions: async (original) => [original],
};

const host: IImageHost = {
  machine: 'local',
  conv: 'tab-gallery' as ConvId,
  srcOf: pictureOf,
  edit: editHost,
};

let serial = 0;
const call = (over: Partial<ImageGen>, brief = over.prompt ?? ''): ToolEntry => {
  serial += 1;
  const images: ImageGen = {
    count: 1,
    prompt: brief,
    shape: '',
    jobId: '',
    state: 'drawing',
    done: [],
    failed: null,
    ...over,
  };
  return {
    id: `x:${serial}#0`,
    turnId: null,
    at: 0,
    rev: 1,
    t: 'tool',
    toolUseId: `call_${serial}`,
    tool: 'mcp__image_gen__imagegen',
    summary: brief,
    ok: images.state === 'drawing' ? null : true,
    output: null,
    images,
  };
};

const files = (shape: string, n: number): string[] => Array.from({ length: n }, (_, i) => `/g/${shape}-${i}`);

const BOXES: { id: string; label: string; entry: ToolEntry }[] = [
  {
    id: 'drawing-1',
    label: '1 picture, drawing',
    entry: call({
      count: 1,
      shape: 'square',
      prompt: 'An orange tabby cat in a white astronaut suit on the Moon',
    }),
  },
  {
    id: 'drawing-4',
    label: '4 pictures, drawing (2×2)',
    entry: call({ count: 4, shape: 'landscape', prompt: 'Four logo variants for a bakery called Rye & Co' }),
  },
  {
    id: 'mixed-4',
    label: '4 pictures, 2 done + 2 drawing',
    entry: call({
      count: 4,
      shape: 'landscape',
      done: files('landscape', 2),
      prompt: 'Four moody landscapes of a lighthouse at dusk, different weather in each',
    }),
  },
  {
    id: 'done-3',
    label: '3 done, landscape',
    entry: call({
      count: 3,
      shape: 'landscape',
      state: 'done',
      done: files('landscape', 3),
      prompt: 'Three hero images',
    }),
  },
  {
    id: 'portrait-1',
    label: '1 portrait, done',
    entry: call({
      count: 1,
      shape: 'portrait',
      state: 'done',
      done: files('portrait', 1),
      prompt: 'A poster for a jazz night',
    }),
  },
  {
    id: 'failed',
    label: 'failed',
    entry: call({
      count: 1,
      shape: 'square',
      state: 'failed',
      failed: 'the request was rejected by the content policy',
      prompt: 'A busy market square',
    }),
  },
  {
    id: 'partial',
    label: 'partial: 2 of 3 + failed line',
    entry: call({
      count: 3,
      shape: 'square',
      state: 'done',
      done: files('square', 2),
      failed: 'one picture could not be drawn',
      prompt: 'Three app icons: a leaf, a wave, a flame',
    }),
  },
];

const Box = ({ label, children }: { label: string; children: ReactNode }) => (
  <div className="hx-ig__box">
    <div className="hx-ig__label">{label}</div>
    {children}
  </div>
);

const Gallery = () => {
  const { setTheme } = useTheme();
  useEffect(() => {
    const theme = params.get('theme');
    if (theme === 'dark' || theme === 'light') setTheme(theme);
    const lang = params.get('lang');
    pinLocale(lang === 'en' || lang === 'ru' ? lang : null);
  }, [setTheme]);
  const boxes = useMemo(() => BOXES.filter((box) => SCENE === null || box.id === SCENE), []);
  return (
    <ImageHost.Provider value={host}>
      <div className="hx-ig">
        {boxes.map((box) => {
          const entry = imageEntryOf(box.entry);
          return (
            <Box key={box.id} label={box.label}>
              {entry && <ImageCard entry={entry} />}
            </Box>
          );
        })}
      </div>
    </ImageHost.Provider>
  );
};

export default Gallery;
