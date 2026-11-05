// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { modeKindOf, modeStateKey, useDesignMode } from '@/next/features/designly/design-mode';
import { chatPanes, resetChatPanes } from '@/next/stores/pane-state';

/** A reload: what was kept is written, the page's memory is gone, the next read is from localStorage. */
const reload = () => {
  chatPanes.flush();
  resetChatPanes();
};

beforeEach(() => {
  localStorage.clear();
  resetChatPanes();
});
afterEach(() => resetChatPanes());

describe('useDesignMode', () => {
  it('opens a project from a chat for editing the first time', () => {
    const onMode = vi.fn();
    const { result } = renderHook(() => useDesignMode({ conv: 'tab-1', id: 'p1', edit: false, onMode }));
    expect(result.current[0]).toBe(true);
    // The address follows the chat's mode.
    expect(onMode).toHaveBeenCalledWith(true);
  });

  it('keeps the mode of the chat over a reload, whatever the address says', () => {
    const first = renderHook(() => useDesignMode({ conv: 'tab-1', id: 'p1', edit: true, onMode: vi.fn() }));
    act(() => first.result.current[1](false));
    expect(first.result.current[0]).toBe(false);
    first.unmount();
    reload();
    const again = renderHook(() => useDesignMode({ conv: 'tab-1', id: 'p1', edit: true, onMode: vi.fn() }));
    expect(again.result.current[0]).toBe(false);
  });

  it('keeps the mode per chat and per project', () => {
    const one = renderHook(() => useDesignMode({ conv: 'tab-1', id: 'p1', edit: false, onMode: vi.fn() }));
    act(() => one.result.current[1](false));
    reload();
    const other = renderHook(() => useDesignMode({ conv: 'tab-2', id: 'p1', edit: false, onMode: vi.fn() }));
    expect(other.result.current[0]).toBe(true);
    const otherProject = renderHook(() =>
      useDesignMode({ conv: 'tab-1', id: 'p2', edit: false, onMode: vi.fn() }),
    );
    expect(otherProject.result.current[0]).toBe(true);
  });

  it('opened not from a chat, follows the address and is looked at by default', () => {
    const onMode = vi.fn();
    const looked = renderHook(() => useDesignMode({ conv: null, id: 'p1', edit: false, onMode }));
    expect(looked.result.current[0]).toBe(false);
    expect(onMode).not.toHaveBeenCalled();
    const edited = renderHook(() => useDesignMode({ conv: null, id: 'p1', edit: true, onMode }));
    expect(edited.result.current[0]).toBe(true);
    act(() => edited.result.current[1](false));
    expect(onMode).toHaveBeenCalledWith(false);
  });
});

describe('the words of the mode', () => {
  it('names the kind of project', () => {
    expect(modeStateKey(modeKindOf('slides'), true)).toBe('next.site.stateEdit.slides');
    expect(modeStateKey(modeKindOf('video'), false)).toBe('next.site.stateView.video');
    expect(modeStateKey(modeKindOf(undefined), true)).toBe('next.site.stateEdit.site');
  });
});
