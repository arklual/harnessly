// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { coverField, isFieldCovered, useCoverField, useFieldCovered } from './field-cover';

describe('the field cover', () => {
  it('holds while any opener holds it and lets go after the last', () => {
    const picker = coverField();
    const plus = coverField();
    expect(isFieldCovered()).toBe(true);
    picker();
    expect(isFieldCovered()).toBe(true);
    plus();
    expect(isFieldCovered()).toBe(false);
  });

  it('counts a release once', () => {
    const one = coverField();
    const two = coverField();
    one();
    one();
    expect(isFieldCovered()).toBe(true);
    two();
    expect(isFieldCovered()).toBe(false);
  });

  it('follows an open popover and tells the notice', () => {
    const notice = renderHook(() => useFieldCovered());
    const picker = renderHook(({ on }) => useCoverField(on), { initialProps: { on: false } });
    expect(notice.result.current).toBe(false);
    act(() => picker.rerender({ on: true }));
    expect(notice.result.current).toBe(true);
    act(() => picker.rerender({ on: false }));
    expect(notice.result.current).toBe(false);
    act(() => picker.rerender({ on: true }));
    act(() => picker.unmount());
    expect(notice.result.current).toBe(false);
  });
});
