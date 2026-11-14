import { describe, expect, it } from 'vitest';
import { isPublicAddress, slotBoardHref, storeBoardAtHref } from '@/next/features/designly/api';

describe('isPublicAddress', () => {
  it('is an address another device can open', () => {
    expect(isPublicAddress('https://harnessly.example.org/designly/x')).toBe(true);
    expect(isPublicAddress('http://192.168.1.20:8080/designly/x')).toBe(true);
  });

  it("is not the machine's own loopback, nor a broken address", () => {
    expect(isPublicAddress('http://localhost:5173/designly/x')).toBe(false);
    expect(isPublicAddress('http://app.localhost/designly/x')).toBe(false);
    expect(isPublicAddress('http://127.0.0.1:8080/')).toBe(false);
    expect(isPublicAddress('http://[::1]:8080/')).toBe(false);
    expect(isPublicAddress('not an address')).toBe(false);
  });
});

describe('slotBoardHref', () => {
  it('opens a board on a slot of its own origin', () => {
    expect(slotBoardHref('https://pv-1.harnessly.example.org', 'relay-video', '02 Выкат.dc.html')).toBe(
      'https://pv-1.harnessly.example.org/a/relay-video/p/02%20%D0%92%D1%8B%D0%BA%D0%B0%D1%82.dc.html',
    );
  });

  it('keeps the prefix of a slot by path on the hub', () => {
    const slot = 'https://hub.example.org/m/abc/__pv/1-dfdda5399adc61a06ab6b5ceb2a09ab1';
    expect(slotBoardHref(slot, 'relay-video', '04 b.dc.html?x=1')).toBe(
      `${slot}/a/relay-video/p/04%20b.dc.html?x=1`,
    );
  });
});

describe('storeBoardAtHref', () => {
  // the revision used to be glued after the machine («?machine=m?rev=7»), so the hub knew no such machine
  // and every board of a project read from the store answered 404.
  it('names the revision in one query and no machine (HRN-1464)', () => {
    const href = storeBoardAtHref('deck', 'Кто мы.dc.html', 7, false);
    const url = new URL(href, 'https://hub.example.org');
    expect(href.split('?')).toHaveLength(2);
    expect(url.pathname.endsWith('/design/store/deck/f/%D0%9A%D1%82%D0%BE%20%D0%BC%D1%8B.dc.html')).toBe(
      true,
    );
    expect(url.searchParams.has('machine')).toBe(false);
    expect(url.searchParams.get('rev')).toBe('7');
    expect(url.searchParams.has('scene')).toBe(false);
  });

  it("asks for the scene's runtime of a video", () => {
    const url = new URL(storeBoardAtHref('film', '01.dc.html', 3, true), 'https://hub.example.org');
    expect(url.searchParams.get('scene')).toBe('1');
    expect(url.searchParams.has('machine')).toBe(false);
  });
});
