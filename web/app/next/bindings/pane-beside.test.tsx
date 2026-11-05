// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { usePaneOpenBeside } from '@/next/bindings/pane-beside';

interface IProps {
  kept: boolean;
  beside: boolean;
  conv: string | null;
}

const mount = (first: IProps, setKept = vi.fn()) =>
  renderHook((p: IProps) => usePaneOpenBeside(p.kept, setKept, p.beside, p.conv), { initialProps: first });

describe('usePaneOpenBeside', () => {
  it('a chat with its pane out shows the pane', () => {
    const { result } = mount({ kept: true, beside: false, conv: 'tab-1' });
    expect(result.current[0]).toBe(true);
  });

  it('«развернуть»: the design goes full screen and the pane that showed it stays shut', () => {
    const view = mount({ kept: true, beside: false, conv: 'tab-1' });
    view.rerender({ kept: true, beside: true, conv: 'tab-1' });
    expect(view.result.current[0]).toBe(false);
  });

  it('opened beside the design, it is out there and the chat keeps its own state', () => {
    const setKept = vi.fn();
    const view = mount({ kept: false, beside: true, conv: 'tab-1' }, setKept);
    act(() => view.result.current[1](true));
    expect(view.result.current[0]).toBe(true);
    expect(setKept).not.toHaveBeenCalled();
  });

  it('back on the chat its own state rules; the next full view starts shut again', () => {
    const view = mount({ kept: true, beside: true, conv: 'tab-1' });
    act(() => view.result.current[1](true));
    view.rerender({ kept: true, beside: false, conv: 'tab-1' });
    expect(view.result.current[0]).toBe(true);
    view.rerender({ kept: true, beside: true, conv: 'tab-1' });
    expect(view.result.current[0]).toBe(false);
  });

  it('another design’s chat beside it starts shut', () => {
    const view = mount({ kept: false, beside: true, conv: 'tab-1' });
    act(() => view.result.current[1](true));
    view.rerender({ kept: false, beside: true, conv: 'tab-2' });
    expect(view.result.current[0]).toBe(false);
  });

  it('on a chat, opening and shutting write the chat’s state', () => {
    const setKept = vi.fn();
    const view = mount({ kept: false, beside: false, conv: 'tab-1' }, setKept);
    act(() => view.result.current[1](true));
    expect(setKept).toHaveBeenCalledWith(true);
  });
});
