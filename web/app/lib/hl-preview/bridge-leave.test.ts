import { describe, expect, it, vi } from 'vitest';
import * as core from './bridge-core';

/** A window with the Navigation API: `navigate` fires the page's own navigation as the browser would. */
const fakeWindow = () => {
  let listener: ((event: unknown) => void) | null = null;
  const went: string[] = [];
  const win = {
    URL,
    location: {
      origin: 'https://ai-alice-7.example.org',
      replace: (url: string) => {
        went.push(`replace ${url}`);
        win.navigate(url, 'replace');
      },
      assign: (url: string) => {
        went.push(`assign ${url}`);
        win.navigate(url, 'push');
      },
    },
    navigation: {
      addEventListener: (_: string, fn: (event: unknown) => void) => {
        listener = fn;
      },
    },
    setTimeout: (fn: () => void, ms: number) => setTimeout(fn, ms),
    clearTimeout: (id: number) => clearTimeout(id),
    /** The page navigates; true when the navigation went on. */
    navigate: (url: string, navigationType = 'replace', extra: Record<string, unknown> = {}) => {
      let prevented = false;
      listener?.({
        destination: { url },
        navigationType,
        cancelable: true,
        userInitiated: false,
        hashChange: false,
        formData: null,
        downloadRequest: null,
        preventDefault: () => {
          prevented = true;
        },
        ...extra,
      });
      return !prevented;
    },
  };
  return { win, went };
};

describe('a page leaving the slot for another site by script', () => {
  const auth = 'https://posthog.tl.example.ru/x-teleport-auth?state=a&subject=s#value=v';

  it('is held back and told to the parent; the tab going there itself keeps the page in place', () => {
    vi.useFakeTimers();
    const { win, went } = fakeWindow();
    const posted: unknown[] = [];
    const answer = core.installLeave(win, (m) => posted.push(m));
    expect(win.navigate(auth)).toBe(false);
    expect(posted).toEqual([{ type: 'tk:redirect', url: auth }]);
    answer(auth, true);
    vi.advanceTimersByTime(core.LEAVE_WAIT_MS * 2);
    expect(went).toEqual([]);
    vi.useRealTimers();
  });

  it('goes on as it asked when the tab does not follow, or no answer comes', () => {
    vi.useFakeTimers();
    const { win, went } = fakeWindow();
    const answer = core.installLeave(win, () => {});
    win.navigate(auth);
    answer(auth, false);
    expect(went).toEqual([`replace ${auth}`]);
    win.navigate('https://pay.example.com/form', 'push');
    vi.advanceTimersByTime(core.LEAVE_WAIT_MS);
    expect(went).toEqual([`replace ${auth}`, 'assign https://pay.example.com/form']);
    vi.useRealTimers();
  });

  it('leaves alone the slot itself, a click, a form, a traversal and plain http', () => {
    const { win } = fakeWindow();
    const posted: unknown[] = [];
    core.installLeave(win, (m) => posted.push(m));
    expect(win.navigate('https://ai-alice-7.example.org/next')).toBe(true);
    expect(win.navigate(auth, 'push', { userInitiated: true })).toBe(true);
    expect(win.navigate(auth, 'push', { formData: {} })).toBe(true);
    expect(win.navigate(auth, 'traverse')).toBe(true);
    expect(win.navigate('http://site.example.ru/')).toBe(true);
    expect(posted).toEqual([]);
  });

  it('is read from the parent only in its own shape', () => {
    expect(core.parseParentMessage({ type: 'tk:redirect-answer', url: auth, follow: true })).toEqual({
      type: 'tk:redirect-answer',
      url: auth,
      follow: true,
    });
    expect(core.parseParentMessage({ type: 'tk:redirect-answer', url: auth, follow: 'yes' })).toBeNull();
  });
});

describe('a page that asks for a security key or Touch ID', () => {
  it('tells the parent and still makes the call', async () => {
    const posted: unknown[] = [];
    const calls: unknown[] = [];
    const credentials = {
      get: (options: unknown) => {
        calls.push(options);
        return Promise.reject(new Error('NotAllowedError'));
      },
    };
    core.installKeyStep({ navigator: { credentials } }, (m) => posted.push(m));
    await credentials.get({ password: true }).catch(() => {});
    expect(posted).toEqual([]);
    await credentials.get({ publicKey: { challenge: 'x' } }).catch(() => {});
    expect(posted).toEqual([{ type: 'tk:key-step' }]);
    expect(calls).toHaveLength(2);
  });
});
