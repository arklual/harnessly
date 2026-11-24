// The gallery card of a turn's pictures (features/chat/picture-gallery.tsx) on made-up pictures: ?dev=shots. 2, 6 and 24
// pictures of mixed shapes, so the arrows, the strip, the counter and «Все» can be seen on a computer and a phone.
// Never part of a production path.
import '@/next/styles/order';
import { pinLocale } from '@/next/i18n';
import { PictureCards } from '@/next/features/chat/pictures';
import type { Picture } from '@/next/protocol/v2';

const SHAPES = [
  [1600, 900],
  [900, 1600],
  [1200, 1200],
  [1500, 1000],
] as const;

const made = new Map<number, string>();

/** A PNG (not an SVG: a picture of a machine is a raster) with its number, in one of four shapes. */
const pictureOf = (n: number): string => {
  const have = made.get(n);
  if (have) return have;
  const [w, h] = SHAPES[n % SHAPES.length];
  const canvas = document.createElement('canvas');
  canvas.width = w / 4;
  canvas.height = h / 4;
  const g = canvas.getContext('2d');
  if (!g) return '';
  const hue = (n * 47) % 360;
  const grad = g.createLinearGradient(0, 0, canvas.width, canvas.height);
  grad.addColorStop(0, `hsl(${hue} 60% 30%)`);
  grad.addColorStop(1, `hsl(${(hue + 60) % 360} 70% 60%)`);
  g.fillStyle = grad;
  g.fillRect(0, 0, canvas.width, canvas.height);
  g.fillStyle = '#fff';
  g.font = `${Math.min(canvas.width, canvas.height) / 3}px sans-serif`;
  g.textAlign = 'center';
  g.textBaseline = 'middle';
  g.fillText(String(n + 1), canvas.width / 2, canvas.height / 2);
  const url = canvas.toDataURL('image/png');
  made.set(n, url);
  return url;
};

const set = (count: number): Picture[] =>
  Array.from({ length: count }, (_, k) => ({ id: `s${count}-${k}`, text: `/tmp/shots/shot-${k + 1}.png` }));
const srcOf = (picture: Picture) => pictureOf(Number(picture.id.split('-')[1]));

pinLocale('ru');

const Gallery = () => {
  return (
    <div
      className="hx-root"
      style={{ padding: 16, display: 'grid', gap: 24, maxWidth: 720, margin: '0 auto' }}
    >
      {[2, 6, 24].map((n) => (
        <PictureCards key={n} pictures={set(n)} srcOf={srcOf} big title={`Скриншоты · ${n}`} />
      ))}
    </div>
  );
};

export default Gallery;
