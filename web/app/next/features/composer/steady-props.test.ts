import { createElement, createRef } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { SteadyProps } from '@/next/features/composer/steady-props';

describe('the props the window’s field draws', () => {
  it('do not change when a screen only makes its handlers and small objects anew', () => {
    const steady = new SteadyProps({
      value: 'hi',
      onChange: () => 1,
      picker: { model: 'k3', onChange: () => 2 },
    });
    const shown = steady.value;
    expect(
      steady.update({ value: 'hi', onChange: () => 1, picker: { model: 'k3', onChange: () => 2 } }),
    ).toBe(false);
    expect(steady.value).toBe(shown);
  });

  it('call the screen’s newest handler, nested ones too', () => {
    const first = vi.fn();
    const newest = vi.fn();
    const nestedNewest = vi.fn();
    const steady = new SteadyProps({ onSubmit: first, picker: { onChange: vi.fn() } });
    steady.update({ onSubmit: newest, picker: { onChange: nestedNewest } });
    steady.value.onSubmit();
    steady.value.picker.onChange('x');
    expect(first).not.toHaveBeenCalled();
    expect(newest).toHaveBeenCalled();
    expect(nestedNewest).toHaveBeenCalledWith('x');
  });

  it('change when what the field shows changes', () => {
    const steady = new SteadyProps<{ value: string; quotes: { id: string }[]; onMic?: () => void }>({
      value: 'a',
      quotes: [{ id: '1' }],
    });
    expect(steady.update({ value: 'a', quotes: [{ id: '1' }, { id: '2' }] })).toBe(true);
    expect(steady.update({ value: 'b', quotes: [{ id: '1' }, { id: '2' }] })).toBe(true);
    expect(steady.update({ value: 'b', quotes: [{ id: '1' }, { id: '2' }], onMic: () => undefined })).toBe(
      true,
    );
  });

  it('keep a ref the screen’s own', () => {
    const ref = createRef<HTMLTextAreaElement>();
    const steady = new SteadyProps({ inputRef: ref });
    expect(steady.value.inputRef).toBe(ref);
  });

  it('take a button the screen draws anew as the same, and call its newest handler', () => {
    const first = vi.fn();
    const newest = vi.fn();
    const button = (label: string, onClick: () => void) => createElement('button', { onClick }, label);
    const steady = new SteadyProps({ tools: button('Settings', first) });
    const shown = steady.value.tools;
    expect(steady.update({ tools: button('Settings', newest) })).toBe(false);
    expect(steady.value.tools).toBe(shown);
    (steady.value.tools.props as { onClick: () => void }).onClick();
    expect(newest).toHaveBeenCalled();
    expect(first).not.toHaveBeenCalled();
    expect(steady.update({ tools: button('Deck', newest) })).toBe(true);
  });
});

describe('a change of one prop', () => {
  it('keeps the other parts the same objects', () => {
    const steady = new SteadyProps({
      value: 'a',
      picker: { model: 'k3', list: [{ id: 1 }] },
      quotes: [{ id: 'q' }],
    });
    const was = steady.value;
    expect(
      steady.update({ value: 'ab', picker: { model: 'k3', list: [{ id: 1 }] }, quotes: [{ id: 'q' }] }),
    ).toBe(true);
    expect(steady.value.value).toBe('ab');
    expect(steady.value.picker).toBe(was.picker);
    expect(steady.value.quotes).toBe(was.quotes);
  });
});
