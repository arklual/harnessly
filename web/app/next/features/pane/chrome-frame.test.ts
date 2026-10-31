import { describe, expect, it } from 'vitest';
import { pagePoint, parseChromeFrame, parseChromeMessage, tabLabel } from '@/next/features/pane/chrome-frame';

describe('chrome-frame', () => {
  it('reads the socket text it knows and nothing else', () => {
    expect(parseChromeMessage('{"t":"status","up":false,"error":"refused"}')).toEqual({
      t: 'status',
      up: false,
      error: 'refused',
    });
    expect(parseChromeMessage('{"t":"tabs","tabs":null}')).toEqual({ t: 'tabs', tabs: [] });
    expect(
      parseChromeMessage(
        '{"t":"tabs","tabs":[{"id":"a","title":"A","url":"https://a/","active":true},{"id":1}]}',
      ),
    ).toEqual({ t: 'tabs', tabs: [{ id: 'a', title: 'A', url: 'https://a/', active: true }] });
    expect(parseChromeMessage('{"t":"other"}')).toBeNull();
    expect(parseChromeMessage('not json')).toBeNull();
  });

  it('reads a frame: the size first, then the picture', () => {
    const buf = new ArrayBuffer(12);
    const view = new DataView(buf);
    view.setUint32(0, 1440);
    view.setUint32(4, 812);
    new Uint8Array(buf, 8).set([1, 2, 3, 4]);
    const frame = parseChromeFrame(buf);
    expect(frame?.w).toBe(1440);
    expect(frame?.h).toBe(812);
    expect(frame?.jpeg.size).toBe(4);
    expect(parseChromeFrame(new ArrayBuffer(8))).toBeNull();
    expect(parseChromeFrame(new ArrayBuffer(12))).toBeNull();
  });

  it('finds the point of the page under the pointer, nothing on the bands around it', () => {
    // A 1000×500 page in a 500×500 box: drawn at half size, a band of 125 above and below.
    const box = { left: 10, top: 20, width: 500, height: 500 };
    const page = { w: 1000, h: 500 };
    expect(pagePoint(box, page, { x: 10, y: 145 })).toEqual({ x: 0, y: 0 });
    expect(pagePoint(box, page, { x: 260, y: 270 })).toEqual({ x: 500, y: 250 });
    expect(pagePoint(box, page, { x: 260, y: 100 })).toBeNull();
    expect(pagePoint({ ...box, width: 0 }, page, { x: 10, y: 145 })).toBeNull();
  });

  it('names a tab by its title, else by its address', () => {
    expect(tabLabel({ id: 'a', title: ' Docs ', url: 'https://x/', active: false })).toBe('Docs');
    expect(tabLabel({ id: 'a', title: '', url: 'https://x.org/p', active: false })).toBe('x.org/p');
  });
});
