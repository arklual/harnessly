import { describe, expect, it } from 'vitest';
import {
  boardOfPath,
  pictureOf,
  readImageAct,
  screenBoxOf,
} from '@/next/features/designly/image-gen/image-act';

const base = {
  type: 'tk:image-act',
  act: 'edit',
  tpl: 3,
  was: 'abc',
  rect: [10, 20, 100, 50],
  vw: 200,
  vh: 400,
};

describe('image-act', () => {
  it('reads a press and clips the foreign fields', () => {
    const act = readImageAct({
      ...base,
      src: 'assets/a.png',
      from: 'assets/b.jpg ../x.png /etc/a.png assets/c.webp',
      file: 'Главная.dc.html',
    });
    expect(act?.src).toBe('assets/a.png');
    expect(act?.from).toEqual(['assets/b.jpg', 'assets/c.webp']);
    expect(act?.file).toBe('Главная.dc.html');
  });

  it('refuses what is not a press', () => {
    expect(readImageAct({ ...base, type: 'tk:other' })).toBeNull();
    expect(readImageAct({ ...base, act: 'drop' })).toBeNull();
    expect(readImageAct({ ...base, was: '' })).toBeNull();
    expect(readImageAct({ ...base, rect: [1, 2, -3, 4] })).toBeNull();
  });

  it('knows only plain pictures of the project and boards at its root', () => {
    expect(pictureOf('assets/a.png')).toBe('assets/a.png');
    expect(pictureOf('https://x/a.png')).toBe('');
    expect(pictureOf('assets/../a.png')).toBe('');
    expect(boardOfPath('a/b.dc.html')).toBe('');
    expect(boardOfPath('b.dc.html')).toBe('b.dc.html');
  });

  it('puts the block on the screen by the frame scale', () => {
    const act = readImageAct(base);
    expect(act).not.toBeNull();
    if (act)
      expect(screenBoxOf({ left: 100, top: 10, width: 100 }, act)).toEqual({
        left: 105,
        top: 20,
        width: 50,
        height: 25,
      });
  });
});
