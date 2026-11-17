// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import {
  forgetScreen,
  keepScreen,
  keptScreen,
  MAX_SCREENS,
  openAsChat,
  parseScreens,
  screenOf,
  takeAsChat,
} from '@/next/stores/chat-screen';
import { parseRoute, ROUTE } from '@/next/routes/routes';

beforeEach(() => localStorage.clear());

describe('the full screen a chat was left on', () => {
  it('is a screen of a chat only when the address names the chat', () => {
    expect(screenOf(parseRoute('/review?url=https://gitlab.x/a/b/-/merge_requests/1&chat=tab-1'))?.conv).toBe(
      'tab-1',
    );
    expect(screenOf(parseRoute('/review?url=https://gitlab.x/a/b/-/merge_requests/1'))).toBeNull();
    expect(screenOf(parseRoute('/tab-1'))).toBeNull();
  });

  it('keeps it per chat and forgets it when the chat is left as a chat', () => {
    keepScreen('tab-1', '/designly/d1?chat=tab-1');
    keepScreen('tab-2', '/apps/acme.notes?chat=tab-2');
    expect(keptScreen('tab-1')?.kind).toBe(ROUTE.site);
    expect(keptScreen('tab-2')?.kind).toBe(ROUTE.app);
    forgetScreen('tab-1');
    expect(keptScreen('tab-1')).toBeNull();
    expect(keptScreen('tab-2')).not.toBeNull();
  });

  it('drops an address that is not a screen of that chat, and the oldest beyond the limit', () => {
    expect(parseScreens(JSON.stringify({ 'tab-1': '/designly/d1?chat=tab-9', 'tab-2': 7 })).size).toBe(0);
    for (let i = 0; i <= MAX_SCREENS; i++) keepScreen(`tab-${i}`, `/designly/d?chat=tab-${i}`);
    expect(keptScreen('tab-0')).toBeNull();
    expect(keptScreen(`tab-${MAX_SCREENS}`)).not.toBeNull();
  });
});

describe('a chat asked for as a chat', () => {
  it('forgets its screen and is taken once', () => {
    keepScreen('tab-day', '/apps/example.inbox?chat=tab-day');
    openAsChat('tab-day');
    expect(keptScreen('tab-day')).toBeNull();
    expect(takeAsChat('tab-day')).toBe(true);
    expect(takeAsChat('tab-day')).toBe(false);
  });
});
