// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DockStatus, { DOCK_STATUS_EXIT_MS } from '@/next/features/composer/dock-status';
import type { ILaunchStep } from '@/next/features/composer/launch-chain';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const steps = (...states: ILaunchStep['state'][]): ILaunchStep[] =>
  states.map((state, i) => ({ id: (['launch', 'model', 'send'] as const)[i], state }));

describe('DockStatus', () => {
  it('says what waits, quietly', () => {
    render(<DockStatus pending="Model will change to Sonnet 5.5 after the turn" steps={null} />);
    expect(screen.getByText('Model will change to Sonnet 5.5 after the turn')).toBeTruthy();
    expect(document.querySelector('.hx-dockstat')?.getAttribute('data-kind')).toBe('pending');
  });

  it('shows the steps of a start in order, each as it stands', () => {
    render(<DockStatus pending={null} steps={steps('done', 'active', 'todo')} />);
    const items = [...document.querySelectorAll('.hx-dockstat__step')];
    expect(items.map((li) => li.textContent)).toEqual([
      'Starting the agent',
      'Applying the model',
      'Sending the message',
    ]);
    expect(items.map((li) => li.getAttribute('data-state'))).toEqual(['done', 'active', 'todo']);
  });

  it('puts the steps in place of the sentence and keeps one element for both', () => {
    const view = render(<DockStatus pending="Model will change to Sonnet 5.5 after the turn" steps={null} />);
    view.rerender(
      <DockStatus pending="Model will change to Sonnet 5.5 after the turn" steps={steps('active')} />,
    );
    expect(document.querySelectorAll('.hx-dockstat')).toHaveLength(1);
    expect(document.querySelector('.hx-dockstat')?.getAttribute('data-kind')).toBe('steps');
  });

  it('leaves with a closing phase, not at once', () => {
    const view = render(<DockStatus pending="Model will change to Sonnet 5.5 after the turn" steps={null} />);
    view.rerender(<DockStatus pending={null} steps={null} />);
    expect(document.querySelector('.hx-dockstat')?.getAttribute('data-state')).toBe('closing');
    expect(screen.getByText('Model will change to Sonnet 5.5 after the turn')).toBeTruthy();
    act(() => void vi.advanceTimersByTime(DOCK_STATUS_EXIT_MS + 10));
    expect(document.querySelector('.hx-dockstat')).toBeNull();
  });

  it('draws nothing when nothing waits', () => {
    render(<DockStatus pending={null} steps={null} />);
    expect(document.querySelector('.hx-dockstat')).toBeNull();
  });
});
