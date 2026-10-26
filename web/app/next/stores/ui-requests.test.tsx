// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { requestContinue, useContinueRequest } from '@/next/stores/ui-requests';

describe('the request to continue a chat in another model', () => {
  it('reaches the chat that is on the page', () => {
    const handler = vi.fn();
    renderHook(() => useContinueRequest('tab-a', handler));
    requestContinue('tab-a');
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it('waits for a chat that is not mounted yet and is used once', () => {
    requestContinue('tab-b');
    const handler = vi.fn();
    const { rerender } = renderHook(() => useContinueRequest('tab-b', handler));
    expect(handler).toHaveBeenCalledTimes(1);
    rerender();
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it('is not for another chat', () => {
    const handler = vi.fn();
    renderHook(() => useContinueRequest('tab-c', handler));
    requestContinue('tab-d');
    expect(handler).not.toHaveBeenCalled();
  });
});
