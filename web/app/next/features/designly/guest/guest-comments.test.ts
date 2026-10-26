// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  anchorFor,
  cleanState,
  fetchComments,
  GuestError,
  keepWho,
  postComment,
  readWho,
} from '@/next/features/designly/guest/guest-comments';
import type { IPicked } from '@/next/features/designly/page-bridge';

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

const picked: IPicked = {
  id: 7,
  look: null,
  selector: 'main > h1',
  tag: 'h1',
  text: 'Title',
  rect: { x: 1, y: 2, width: 30, height: 4 },
  component: null,
  html: '<h1>Title</h1>',
};

const answer = (status: number, body: unknown) =>
  vi.fn().mockResolvedValue({ ok: status < 400, status, json: () => Promise.resolve(body) });

describe('the guest', () => {
  it('has an id of their own, kept with the name', () => {
    const first = readWho();
    expect(first.guestId).toMatch(/^g-/);
    keepWho({ ...first, name: 'Ann' });
    expect(readWho()).toEqual({ guestId: first.guestId, name: 'Ann' });
  });
});

describe('the anchor of a comment', () => {
  it('is the picked block with the settings of the board', () => {
    expect(anchorFor(picked, { Mode: 'dark' })).toEqual({
      selector: 'main > h1',
      dataId: 7,
      tag: 'h1',
      label: 'Title',
      rect: picked.rect,
      state: { Mode: 'dark' },
    });
  });
});

describe('what goes into a comment', () => {
  it('keeps few short scalars of the settings', () => {
    const controls = Array.from({ length: 50 }, (_, i) => ({ name: `k${i}`, value: i }));
    expect(Object.keys(cleanState(controls))).toHaveLength(20);
    expect(
      cleanState([
        { name: 'a', value: 'x'.repeat(200) },
        { name: 'b', value: { deep: 1 } },
        { name: 'c', value: Number.NaN },
        { name: 'n'.repeat(41), value: 1 },
      ]),
    ).toEqual({ a: 'x'.repeat(80) });
  });

  it('has no anchor for a block the gateway would refuse', () => {
    expect(anchorFor({ ...picked, selector: 'a'.repeat(401) }, {})).toBeNull();
    expect(anchorFor({ ...picked, tag: 'BAD TAG' }, {})).toBeNull();
    expect(anchorFor({ ...picked, rect: { x: 0, y: 0, width: -1, height: 1 } }, {})).toBeNull();
  });
});

describe('the comments over the gateway', () => {
  it('are read with their anchors, and a malformed one is left out', async () => {
    vi.stubGlobal(
      'fetch',
      answer(200, [
        {
          id: 3,
          board: 'a.html',
          text: 'hi',
          name: 'Ann',
          at: '2026-10-25T10:00:00Z',
          anchor: {
            selector: 'p',
            dataId: null,
            tag: 'p',
            label: '',
            rect: { x: 0, y: 0, width: 1, height: 1 },
          },
        },
        'junk',
      ]),
    );
    const list = await fetchComments('/s/t/comments');
    expect(list).toHaveLength(1);
    expect(list?.[0]).toMatchObject({ id: '3', name: 'Ann', anchor: { selector: 'p', state: {} } });
  });

  it('are null where the link does not take them', async () => {
    vi.stubGlobal('fetch', answer(403, {}));
    expect(await fetchComments('/s/t/comments')).toBeNull();
  });

  it('are posted with the guest, the board and the anchor', async () => {
    const fetcher = answer(200, { id: 9, board: 'a.html', text: 'ok', name: 'Ann', at: '' });
    vi.stubGlobal('fetch', fetcher);
    const taken = await postComment('/s/t/comments', {
      who: { guestId: 'g-1', name: 'Ann' },
      text: 'ok',
      board: 'a.html',
      anchor: anchorFor(picked, {}),
    });
    expect(taken.id).toBe('9');
    const [, init] = fetcher.mock.calls[0] as [string, { method: string; body: string }];
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toMatchObject({
      guestId: 'g-1',
      name: 'Ann',
      text: 'ok',
      board: 'a.html',
      hp: '',
      anchor: { selector: 'main > h1', dataId: 7 },
    });
  });

  it('refuse with the gateway word', async () => {
    vi.stubGlobal('fetch', answer(429, { error: 'slow down' }));
    await expect(
      postComment('/s/t/comments', { who: { guestId: 'g', name: 'A' }, text: 'x', board: 'b', anchor: null }),
    ).rejects.toThrow(GuestError);
  });
});
