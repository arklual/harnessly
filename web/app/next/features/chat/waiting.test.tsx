// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import WaitingLine from '@/next/features/chat/waiting';
import { onOpenSubagent } from '@/next/features/subagents/open';
import type { BackgroundTask } from '@/next/protocol/v2';

afterEach(cleanup);

const agent = (n: number): BackgroundTask => ({
  id: `a${n}`,
  conversationId: 'tab-x',
  kind: 'subagent',
  title: `Agent ${n}`,
  startedAt: 1,
  endedAt: null,
  status: 'running',
  devServer: false,
  stoppable: false,
  outputPath: null,
  role: n === 1 ? 'Explore' : undefined,
});

describe('the line that waits for background agents', () => {
  it('draws a face for each of the first four and counts the rest', () => {
    render(<WaitingLine agents={[1, 2, 3, 4, 5, 6].map(agent)} onOpen={vi.fn()} />);
    expect(screen.getAllByRole('button', { name: /^Agent/ })).toHaveLength(4);
    expect(screen.getByText('+2')).toBeTruthy();
    expect(screen.getByRole('button', { name: /6 background agents/ })).toBeTruthy();
  });

  it('opens that agent from its face and all of them from the words', () => {
    const onOpen = vi.fn();
    const opened = vi.fn();
    const stop = onOpenSubagent(opened);
    render(<WaitingLine agents={[agent(1), agent(2)]} onOpen={onOpen} />);
    fireEvent.click(screen.getByRole('button', { name: 'Agent 1 · Explore' }));
    expect(opened).toHaveBeenCalledWith('a1');
    fireEvent.click(screen.getByRole('button', { name: /2 background agents/ }));
    expect(onOpen).toHaveBeenCalledTimes(1);
    stop();
  });
});
