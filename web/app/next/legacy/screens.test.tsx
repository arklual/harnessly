// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ManageScreen } from '@/next/legacy/screens';

vi.mock('@/next/legacy/manage/manage-view', () => ({ default: () => <p>view</p> }));

afterEach(cleanup);

it('does not hide «Управление» behind the fallback when the shell draws again', async () => {
  const onOpen = () => undefined;
  const { rerender } = render(<ManageScreen onOpen={onOpen} />);
  await screen.findByText('view');
  act(() => rerender(<ManageScreen onOpen={onOpen} />));
  expect(screen.getByText('view').style.display).not.toBe('none');
});
