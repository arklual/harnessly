import { describe, expect, it } from 'vitest';
import {
  FRAME_VIEW,
  frameView,
  goTake,
  hasTake,
  pageOfLink,
  readPageGo,
} from '@/next/features/designly/page-link';
import { readBridgeMessage } from '@/next/features/designly/page-bridge';

const PAGES = [
  { path: 'Главная.dc.html', name: 'Главная' },
  { path: 'Оплата.dc.html', name: 'Оплата заказа' },
  { path: 'mobile/Оплата.dc.html', name: 'Оплата · Мобильная' },
];

describe('readPageGo', () => {
  it('reads a link the board asked to follow and clips what it said', () => {
    const go = readPageGo({
      type: 'tk:go',
      ref: '/a/x/p/Оплата.dc.html',
      file: true,
      from: '/a/x/p/Главная.dc.html',
      props: { variant: 'Mobile', loading: 'true', bad: 1, ['x'.repeat(81)]: 'long' },
      hash: 'pay',
      tab: false,
    });
    expect(go).toEqual({
      ref: '/a/x/p/Оплата.dc.html',
      file: true,
      from: '/a/x/p/Главная.dc.html',
      props: { variant: 'Mobile', loading: 'true' },
      hash: 'pay',
      tab: false,
    });
  });
  it('drops what is not a link', () => {
    expect(readPageGo({ type: 'tk:go', ref: '  ' })).toBeNull();
    expect(readPageGo({ type: 'tk:hello' })).toBeNull();
    expect(readPageGo(null)).toBeNull();
  });
});

describe('pageOfLink', () => {
  it('finds a file link by its path from the project root', () => {
    const from = '/a/x/p/Главная.dc.html';
    expect(pageOfLink({ ref: '/a/x/p/Оплата.dc.html', file: true, from }, PAGES, 'Главная.dc.html')).toBe(
      'Оплата.dc.html',
    );
    expect(
      pageOfLink({ ref: '/a/x/p/mobile/Оплата.dc.html', file: true, from }, PAGES, 'Главная.dc.html'),
    ).toBe('mobile/Оплата.dc.html');
  });
  it('resolves a link from a board in a folder against the project root', () => {
    expect(
      pageOfLink(
        { ref: '/a/x/p/Главная.dc.html', file: true, from: '/a/x/p/mobile/Оплата.dc.html' },
        PAGES,
        'mobile/Оплата.dc.html',
      ),
    ).toBe('Главная.dc.html');
  });
  it('finds a named link by its file or its title', () => {
    expect(pageOfLink({ ref: 'Главная', file: false, from: '' }, PAGES, null)).toBe('Главная.dc.html');
    expect(pageOfLink({ ref: 'оплата заказа', file: false, from: '' }, PAGES, null)).toBe('Оплата.dc.html');
    expect(pageOfLink({ ref: 'mobile/Оплата.dc.html', file: false, from: '' }, PAGES, null)).toBe(
      'mobile/Оплата.dc.html',
    );
  });
  it('says there is no page for a page the host does not show (a guest sees only what was shared)', () => {
    expect(pageOfLink({ ref: 'Нет такой', file: false, from: '' }, PAGES, null)).toBeNull();
    expect(
      pageOfLink(
        { ref: '/a/x/p/Тайная.dc.html', file: true, from: '/a/x/p/Главная.dc.html' },
        PAGES,
        'Главная.dc.html',
      ),
    ).toBeNull();
  });
});

describe('the take and the frame', () => {
  it('hands the props and the block over', () => {
    expect(goTake({ props: { variant: 'Mobile' }, hash: 'pay' })).toEqual({
      type: 'tk:go-take',
      props: { variant: 'Mobile' },
      hash: 'pay',
    });
    expect(hasTake({ props: {}, hash: '' })).toBe(false);
    expect(hasTake({ props: {}, hash: 'pay' })).toBe(true);
  });
  it('reads the frame a board asks for as a phone or a wide screen', () => {
    expect(readBridgeMessage({ type: 'tk:frame', w: 390, h: 844 })).toEqual({ k: 'frame', w: 390, h: 844 });
    expect(readBridgeMessage({ type: 'tk:frame', w: 'x', h: 1 })).toBeNull();
    expect(frameView(390)).toBe(FRAME_VIEW.phone);
    expect(frameView(1440)).toBe(FRAME_VIEW.wide);
    expect(frameView(0)).toBe(FRAME_VIEW.any);
  });
});
