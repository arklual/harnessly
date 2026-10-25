// @vitest-environment jsdom
import { act, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import {
  batch,
  createKeyedStore,
  createStore,
  flush,
  setScheduler,
  useKey,
  useSlice,
} from '@/next/core/store';

afterEach(() => setScheduler('sync'));

describe('store', () => {
  it('notifies once per batch, and once per frame with the frame scheduler', () => {
    const queued: (() => void)[] = [];
    setScheduler((run) => queued.push(run));
    const s = createStore(0);
    let heard = 0;
    s.subscribe(() => (heard += 1));
    batch(() => {
      s.set(1);
      s.set(2);
      s.set(3);
    });
    s.set(4);
    expect(heard).toBe(0);
    expect(queued).toHaveLength(1);
    queued.shift()!();
    expect(heard).toBe(1);
    expect(s.get()).toBe(4);
  });

  it('a keyed store notifies only the keys that changed', () => {
    setScheduler('sync');
    const s = createKeyedStore<string, number>();
    const heard: string[] = [];
    s.subscribeKey('a', () => heard.push('a'));
    s.subscribeKey('b', () => heard.push('b'));
    let all: string[] = [];
    s.subscribeAll((ks) => (all = [...ks]));
    batch(() => {
      s.set('a', 1);
      s.set('a', 2);
      s.set('c', 1);
    });
    expect(heard).toEqual(['a']);
    expect(all.sort()).toEqual(['a', 'c']);
    s.set('a', 2); // the same value: nothing
    expect(heard).toEqual(['a']);
  });

  it('a row re-renders only when its key changes; a slice only when the selection does', () => {
    setScheduler('sync');
    const s = createKeyedStore<string, { n: number; other: number }>();
    batch(() => {
      for (let i = 0; i < 100; i += 1) s.set(`k${i}`, { n: i, other: 0 });
    });
    const renders = new Map<string, number>();
    const Row = ({ k }: { k: string }) => {
      useKey(s, k);
      renders.set(k, (renders.get(k) ?? 0) + 1);
      return null;
    };
    let sliceRenders = 0;
    const Slice = () => {
      useSlice(s, 'k0', (v) => v?.n);
      sliceRenders += 1;
      return null;
    };
    render(
      <>
        {Array.from({ length: 100 }, (_, i) => (
          <Row key={i} k={`k${i}`} />
        ))}
        <Slice />
      </>,
    );
    act(() => s.set('k5', { n: 5, other: 1 }));
    expect(renders.get('k5')).toBe(2);
    expect(renders.get('k6')).toBe(1);
    act(() => s.set('k0', { n: 0, other: 9 }));
    expect(sliceRenders).toBe(1);
    act(() => s.set('k0', { n: 7, other: 9 }));
    expect(sliceRenders).toBe(2);
    flush();
  });
});
