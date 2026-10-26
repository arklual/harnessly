// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { boardUrl, readGuestConfig, sharedView } from '@/next/features/designly/guest/guest-config';
import { FIT } from '@/next/features/designly/site-zoom';

afterEach(() => {
  document.body.innerHTML = '';
});

const page = (json: string) => {
  document.body.innerHTML = `<script id="guest-config" type="application/json">${json}</script>`;
  return readGuestConfig();
};

describe('the config of a guest page', () => {
  it('is null where the page has none', () => {
    expect(readGuestConfig()).toBeNull();
    expect(page('not json')).toBeNull();
    expect(page('{"v":2}')).toBeNull();
  });

  it('is read with what the link gives', () => {
    const config = page(
      JSON.stringify({
        v: 1,
        token: 'tok',
        title: 'Site',
        rights: 'comment',
        expiresAt: null,
        view: { v: 1, device: 'fill' },
        boards: [
          { id: 'a', title: 'A', path: 'a.html', w: 100, h: null },
          { id: 'x', path: '' },
        ],
        filesBase: '/s/tok/f/',
        commentsUrl: '/s/tok/comments',
      }),
    );
    expect(config?.rights).toBe('comment');
    expect(config?.boards).toEqual([{ id: 'a', title: 'A', path: 'a.html', w: 100, h: null }]);
    expect(config?.filesBase).toBe('/s/tok/f/');
  });

  it('gives no right to comment unless the link says it', () => {
    expect(page('{"v":1,"rights":"admin"}')?.rights).toBe('view');
  });
});

describe('the address of a board', () => {
  it('encodes each segment and keeps the query', () => {
    expect(boardUrl('/s/t/f/', 'lib/my page.html?Name=a b')).toBe('/s/t/f/lib/my%20page.html?Name=a b');
  });
});

describe('the view the owner shared', () => {
  it('is none where the link carries none', () => {
    expect(sharedView(null)).toBeUndefined();
  });

  it('is applied as the owner had it', () => {
    const view = sharedView({
      v: 1,
      device: 'fill',
      rotated: true,
      width: 800,
      height: 600,
      chrome: false,
      browser: 'chrome',
      fit: null,
      zoom: 1.5,
      panX: 10,
      panY: -4,
      locked: true,
      page: 'a.html',
    });
    expect(view).toMatchObject({
      device: 'fill',
      size: { w: 800, h: 600 },
      rot: true,
      chrome: false,
      browser: 'chrome',
      fit: null,
      zoom: { s: 1.5, x: 10, y: -4 },
      locked: true,
      page: 'a.html',
    });
  });

  it('keeps a fit mode and ignores a pan', () => {
    const view = sharedView({ v: 1, device: 'fill', fit: 'width', zoom: 3, panX: 1, panY: 1 });
    expect(view?.fit).toBe(FIT.width);
    expect(view?.zoom).toMatchObject({ s: 1 });
  });
});
