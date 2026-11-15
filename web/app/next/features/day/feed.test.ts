import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  ACTION,
  actOnItem,
  FeedError,
  isLogin,
  loadCard,
  loadDay,
  loginUrl,
  sameItem,
  SECRET_NEED,
  secretNeed,
  throughMachine,
  under,
  webUrl,
} from '@/next/features/day/feed';

const FEED = 'https://example.com/day/';
const MACHINE_FEED = '/api/platform/apps/feed/example-corp.day/day?machine=m1';

const answer = (status: number, body: string) =>
  vi.fn(async () => new Response(body, { status, headers: { 'Content-Type': 'application/json' } }));

afterEach(() => vi.unstubAllGlobals());

describe('the feed of the day', () => {
  it('asks for the day with the cookies of the person', async () => {
    const fetched = answer(200, '{"calendar":{"from":"a","to":"b"}}');
    vi.stubGlobal('fetch', fetched);
    expect(await loadDay(FEED)).toEqual({ calendar: { from: 'a', to: 'b' } });
    expect(fetched).toHaveBeenCalledWith(FEED, { credentials: 'include' });
  });

  it('asks for one thing under the feed, whatever its id', async () => {
    const fetched = answer(200, '{"itemId":"a/b","title":"T"}');
    vi.stubGlobal('fetch', fetched);
    await loadCard(FEED, 'a/b');
    expect(fetched).toHaveBeenCalledWith('https://example.com/day/items/a%2Fb', { credentials: 'include' });
  });

  it('writes what the person decided as a request only a page can make', async () => {
    const fetched = answer(200, '');
    vi.stubGlobal('fetch', fetched);
    expect(await actOnItem(FEED, 7, ACTION.ask, { say: 'shorter' })).toEqual({});
    expect(fetched).toHaveBeenCalledWith('https://example.com/day/items/7/ask', {
      credentials: 'include',
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'harnessly' },
      body: '{"say":"shorter"}',
    });
  });

  it('tells a missing sign-in from any other refusal', async () => {
    vi.stubGlobal('fetch', answer(401, 'sign in'));
    const refused = await loadDay(FEED).catch((e: unknown) => e);
    expect(refused).toBeInstanceOf(FeedError);
    expect(isLogin(refused, FEED)).toBe(true);
    vi.stubGlobal('fetch', answer(500, 'broken'));
    expect(isLogin(await loadDay(FEED).catch((e: unknown) => e), FEED)).toBe(false);
    expect(isLogin(new Error('offline'), FEED)).toBe(false);
  });

  it('reads a feed the machine signs in under its path, the machine named last', async () => {
    const fetched = answer(200, '{}');
    vi.stubGlobal('fetch', fetched);
    await loadCard(MACHINE_FEED, 101);
    await actOnItem(MACHINE_FEED, 101, ACTION.take);
    expect(fetched.mock.calls.map((call) => (call as unknown[])[0])).toEqual([
      '/api/platform/apps/feed/example-corp.day/day/items/101?machine=m1',
      '/api/platform/apps/feed/example-corp.day/day/items/101/take?machine=m1',
    ]);
    expect(under('https://example.com/day/?embed=1#top', '/items/7')).toBe(
      'https://example.com/day/items/7?embed=1#top',
    );
    expect(under('https://example.com/day', '/items/7')).toBe('https://example.com/day/items/7');
  });

  it('tells a token the machine lacks, or the feed refused, from a sign-in', async () => {
    vi.stubGlobal('fetch', answer(409, '{"error":"secret-missing","secret":"token"}'));
    const missing = await loadDay(MACHINE_FEED).catch((e: unknown) => e);
    expect(secretNeed(missing)).toBe(SECRET_NEED.missing);
    expect(isLogin(missing, MACHINE_FEED)).toBe(false);
    vi.stubGlobal('fetch', answer(409, '{"error":"secret-refused","secret":"token"}'));
    expect(secretNeed(await loadDay(MACHINE_FEED).catch((e: unknown) => e))).toBe(SECRET_NEED.refused);
    // The feed's own 409 (the thing is already being done) is not about the secret.
    vi.stubGlobal('fetch', answer(409, '{"error":"taken"}'));
    expect(secretNeed(await loadDay(MACHINE_FEED).catch((e: unknown) => e))).toBeNull();
    vi.stubGlobal('fetch', answer(500, '{"error":"secret-missing"}'));
    expect(secretNeed(await loadDay(MACHINE_FEED).catch((e: unknown) => e))).toBeNull();
    // Through the machine a 401 is the host's own session: the feed's sign-in window would not help.
    vi.stubGlobal('fetch', answer(401, '{"error":"Unauthorized"}'));
    expect(isLogin(await loadDay(MACHINE_FEED).catch((e: unknown) => e), MACHINE_FEED)).toBe(false);
    expect(throughMachine(MACHINE_FEED)).toBe(true);
    expect(throughMachine(FEED)).toBe(false);
  });

  it('signs in on the site of the feed, in a window', () => {
    expect(loginUrl('https://example.com:8443/day?x=1')).toBe('https://example.com:8443/auth/login?popup=1');
  });

  it('keeps only web addresses of what the feed names', () => {
    expect(webUrl('https://example.com/a')).toBe('https://example.com/a');
    expect(webUrl('http://example.com/a')).toBe('http://example.com/a');
    expect(webUrl('javascript:alert(1)')).toBeUndefined();
    expect(webUrl('data:text/html,x')).toBeUndefined();
    expect(webUrl('/relative')).toBeUndefined();
    expect(webUrl(undefined)).toBeUndefined();
  });

  it('matches a thing by its id as text', () => {
    expect(sameItem(7, '7')).toBe(true);
    expect(sameItem(7, 8)).toBe(false);
    expect(sameItem(undefined, undefined)).toBe(false);
  });
});
