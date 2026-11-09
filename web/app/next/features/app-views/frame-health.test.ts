import { describe, expect, it, vi } from 'vitest';
import {
  FAIL_REASON,
  HANDSHAKE_WAIT_MS,
  LOADING,
  classifySessionError,
  failureOfViewError,
  fileOf,
  keepTelling,
  nextFrameState,
  viewErrorOf,
  watchHandshake,
  withTimeout,
  type TFrameState,
} from '@/next/features/app-views/frame-health';

describe('the frame is up only by the handshake', () => {
  it('stays loading when the frame only loaded (a 404 page loads too)', () => {
    // `load` is no event of the machine: nothing moves the state but the handshake, a failure or the timeout.
    expect(LOADING.k).toBe('loading');
  });

  it('is ready on the handshake', () => {
    expect(nextFrameState(LOADING, { k: 'handshake' })).toEqual({ k: 'ready' });
  });

  it('fails on the timeout when the load came without the handshake', () => {
    const next = nextFrameState(LOADING, { k: 'timeout' });
    expect(next).toEqual({ k: 'failed', failure: { reason: FAIL_REASON.silent, detail: '' } });
  });

  it('says what the view reported when the timeout comes', () => {
    const seen = { reason: FAIL_REASON.file, detail: 'ui/main.js' };
    expect(nextFrameState(LOADING, { k: 'timeout' }, seen)).toEqual({ k: 'failed', failure: seen });
  });

  it('does not fail a view that is up, and a late handshake wins over a failure', () => {
    const ready: TFrameState = { k: 'ready' };
    expect(nextFrameState(ready, { k: 'timeout' })).toBe(ready);
    const failed = nextFrameState(LOADING, { k: 'timeout' });
    expect(nextFrameState(failed, { k: 'handshake' })).toEqual({ k: 'ready' });
  });

  it('fails when the session is refused, and starts over on retry', () => {
    const failure = { reason: FAIL_REASON.auth, detail: '401' };
    const failed = nextFrameState(LOADING, { k: 'session-failed', failure });
    expect(failed).toEqual({ k: 'failed', failure });
    expect(nextFrameState(failed, { k: 'retry' })).toEqual(LOADING);
  });
});

describe('the handshake watch', () => {
  it('calls the timeout once after HANDSHAKE_WAIT_MS', () => {
    vi.useFakeTimers();
    try {
      const timeout = vi.fn();
      watchHandshake(timeout);
      vi.advanceTimersByTime(HANDSHAKE_WAIT_MS - 1);
      expect(timeout).not.toHaveBeenCalled();
      vi.advanceTimersByTime(1);
      vi.advanceTimersByTime(HANDSHAKE_WAIT_MS);
      expect(timeout).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('does not call it once cancelled', () => {
    vi.useFakeTimers();
    try {
      const timeout = vi.fn();
      watchHandshake(timeout).cancel();
      vi.advanceTimersByTime(HANDSHAKE_WAIT_MS * 2);
      expect(timeout).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('a session that never comes', () => {
  it('gives up with the message', async () => {
    vi.useFakeTimers();
    try {
      const never = new Promise<string>(() => undefined);
      const out = withTimeout(never, 1000, 'no answer');
      const caught = out.catch((e: Error) => e.message);
      vi.advanceTimersByTime(1000);
      expect(await caught).toBe('no answer');
    } finally {
      vi.useRealTimers();
    }
  });

  it('passes the value through when it comes in time', async () => {
    await expect(withTimeout(Promise.resolve(7), 1000, 'x')).resolves.toBe(7);
  });
});

describe('the reasons', () => {
  it('sorts a refused session', () => {
    expect(classifySessionError('HTTP 401: unauthorized').reason).toBe(FAIL_REASON.auth);
    expect(classifySessionError('Failed to fetch').reason).toBe(FAIL_REASON.server);
  });

  it('names the file inside the app, never the session token', () => {
    expect(fileOf('https://slot.example/tok123/acme.demo/ui/main.js', 'acme.demo')).toBe('ui/main.js');
    expect(fileOf('https://slot.example/a/b/c.js', 'acme.demo')).toBe('b/c.js');
  });

  it('reads only a well-formed message of the view', () => {
    expect(
      viewErrorOf({
        hlViewError: { kind: 'resource', text: 'did not load', src: '/t/acme.demo/ui/main.js' },
      }),
    ).toEqual({
      kind: 'resource',
      text: 'did not load',
      src: '/t/acme.demo/ui/main.js',
      status: null,
    });
    expect(viewErrorOf({ hlViewError: { kind: 'other' } })).toBeNull();
    expect(viewErrorOf('x')).toBeNull();
    expect(viewErrorOf(null)).toBeNull();
  });

  it('puts the file in the failure and prefers the more telling reason', () => {
    const file = failureOfViewError(
      { kind: 'resource', text: '', src: '/t/acme.demo/ui/main.js', status: null },
      'acme.demo',
    );
    expect(file).toEqual({ reason: FAIL_REASON.file, detail: 'ui/main.js' });
    const script = failureOfViewError(
      { kind: 'error', text: 'x is not defined', src: '', status: null },
      'acme.demo',
    );
    expect(keepTelling(script, file)).toBe(file);
    expect(keepTelling(file, script)).toBe(file);
  });
});
