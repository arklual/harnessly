// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IChainInput } from '@/next/features/composer/launch-chain';
import { CHAIN_LINGER_MS, useLaunchChain } from '@/next/features/composer/use-launch-chain';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const pending = { id: 'sonnet', effort: 'high' as const, since: 1, applying: false };
const labels = (steps: ReturnType<typeof useLaunchChain>) => steps?.map((s) => `${s.id}:${s.state}`) ?? null;

describe('useLaunchChain', () => {
  it('follows a start from the launch to the sent message, then stays a moment and leaves', () => {
    const view = renderHook((input: IChainInput) => useLaunchChain(input), {
      initialProps: { agent: 'stopped', pending, waiting: 1 } as IChainInput,
    });
    expect(view.result.current).toBeNull();

    view.rerender({ agent: 'starting', pending, waiting: 1 });
    expect(labels(view.result.current)).toEqual(['launch:active', 'model:todo', 'send:todo']);

    view.rerender({ agent: 'idle', pending: { ...pending, applying: true }, waiting: 1 });
    expect(labels(view.result.current)).toEqual(['launch:done', 'model:active', 'send:todo']);

    view.rerender({ agent: 'idle', pending: undefined, waiting: 1 });
    expect(labels(view.result.current)).toEqual(['launch:done', 'model:done', 'send:active']);

    view.rerender({ agent: 'working', pending: undefined, waiting: 0 });
    expect(labels(view.result.current)).toEqual(['launch:done', 'model:done', 'send:done']);

    act(() => void vi.advanceTimersByTime(CHAIN_LINGER_MS + 10));
    expect(view.result.current).toBeNull();
  });

  it('keeps the same steps while nothing about them changes', () => {
    const view = renderHook((input: IChainInput) => useLaunchChain(input), {
      initialProps: { agent: 'starting', pending: undefined, waiting: 1 } as IChainInput,
    });
    const first = view.result.current;
    view.rerender({ agent: 'starting', pending: undefined, waiting: 1 });
    expect(view.result.current).toBe(first);
  });

  it('ends at once when the agent did not start', () => {
    const view = renderHook((input: IChainInput) => useLaunchChain(input), {
      initialProps: { agent: 'starting', pending, waiting: 1 } as IChainInput,
    });
    expect(view.result.current).not.toBeNull();
    view.rerender({ agent: 'launch-failed', pending, waiting: 1 });
    expect(view.result.current).toBeNull();
  });

  it('does not begin for a change that only waits for the turn', () => {
    const view = renderHook((input: IChainInput) => useLaunchChain(input), {
      initialProps: { agent: 'working', pending, waiting: 0 } as IChainInput,
    });
    expect(view.result.current).toBeNull();
  });
});
