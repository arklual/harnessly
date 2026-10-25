// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import LoadFailed from '@/next/ui/load-failed';

afterEach(cleanup);

describe('LoadFailed', () => {
  it('says what and why, and asks again exactly once per press while the ask is on', async () => {
    let done!: () => void;
    const onRetry = vi.fn(() => new Promise<void>((ok) => (done = ok)));
    const { getByRole, getByText } = render(
      <LoadFailed title="План не загрузился" reason="Нет ответа" onRetry={onRetry} />,
    );
    expect(getByText('План не загрузился')).toBeTruthy();
    expect(getByText('Нет ответа')).toBeTruthy();
    const button = getByRole('button');
    fireEvent.click(button);
    fireEvent.click(button);
    expect(onRetry).toHaveBeenCalledTimes(1);
    await act(async () => done());
    fireEvent.click(button);
    expect(onRetry).toHaveBeenCalledTimes(2);
  });
});
