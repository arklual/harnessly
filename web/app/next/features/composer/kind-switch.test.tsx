// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createRef } from 'react';
import KindSwitch, {
  CHAT_KIND,
  setChatKind,
  useChatKind,
  useKindKey,
} from '@/next/features/composer/kind-switch';

afterEach(() => {
  cleanup();
  setChatKind(CHAT_KIND.task);
});

describe('«Task | Talk» over a new chat (HRN-1496)', () => {
  it('marks the kind and hands over the other on a click', () => {
    const onChange = vi.fn();
    render(<KindSwitch value={CHAT_KIND.task} onChange={onChange} />);
    expect(screen.getByRole('radio', { name: 'Task' }).getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByRole('radio', { name: 'Talk' }));
    expect(onChange).toHaveBeenCalledWith(CHAT_KIND.talk);
  });

  it('remembers the kind for the next new chat', () => {
    const { result } = renderHook(() => useChatKind());
    act(() => setChatKind(CHAT_KIND.talk));
    expect(result.current).toBe(CHAT_KIND.talk);
    expect(localStorage.getItem('hx-new-chat-kind')).toBe(CHAT_KIND.talk);
  });

  it('turns on Shift+Tab only in an empty field', () => {
    const field = createRef<HTMLTextAreaElement>();
    const onKind = vi.fn();
    render(<textarea ref={field} />);
    renderHook(() => useKindKey(field, CHAT_KIND.task, onKind));
    fireEvent.keyDown(field.current as HTMLTextAreaElement, { key: 'Tab', shiftKey: true });
    expect(onKind).toHaveBeenCalledWith(CHAT_KIND.talk);
    (field.current as HTMLTextAreaElement).value = 'words';
    fireEvent.keyDown(field.current as HTMLTextAreaElement, { key: 'Tab', shiftKey: true });
    expect(onKind).toHaveBeenCalledTimes(1);
  });
});
