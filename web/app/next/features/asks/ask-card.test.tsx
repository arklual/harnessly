// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { IAsk } from '@/next/features/asks/api';
import { AskCard } from '@/next/features/asks/ask-card';
import { PeopleSection, peopleAsks } from '@/next/features/asks/people-section';
import { pinLocale } from '@/next/i18n';

const NOW = 1_800_000_000_000;

const ask = (over: Partial<IAsk>): IAsk => ({
  id: 'ask_000000000001',
  kind: 'approval',
  status: 'waiting',
  question: 'RFC: move sessions to the new store',
  details: '',
  link: 'https://example.com/rfc',
  options: [],
  to: { name: '', username: 'lead' },
  bot: 'Team',
  chat: 'Team chat',
  silent: false,
  conversation: 'tab-1',
  agent: 'Sessions',
  createdAt: NOW - 60_000,
  deadline: NOW + 30 * 60_000,
  closedAt: 0,
  decision: '',
  answer: null,
  replies: [],
  seenAt: 0,
  seenReplies: 0,
  error: '',
  ...over,
});

beforeAll(() => pinLocale('en'));
afterEach(() => cleanup());

describe('AskCard', () => {
  it('answers an approval from the page with its comment', async () => {
    const onAnswer = vi.fn(async () => null);
    render(<AskCard ask={ask({})} actions={{ onAnswer, onCancel: async () => null }} />);
    expect(screen.getByText('RFC: move sessions to the new store')).toBeTruthy();
    expect(screen.getByText('@lead')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Comment/ }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'rename the flag' } });
    fireEvent.click(screen.getByRole('button', { name: /Approve/ }));
    await waitFor(() =>
      expect(onAnswer).toHaveBeenCalledWith({ choice: 'approved', text: 'rename the flag' }),
    );
  });

  it('shows a refusal of the machine', async () => {
    render(
      <AskCard
        ask={ask({ kind: 'question', options: ['Today', 'Tomorrow'] })}
        actions={{ onAnswer: async () => 'The question is closed already', onCancel: async () => null }}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Tomorrow' }));
    expect(await screen.findByRole('alert')).toBeTruthy();
  });

  it('shows the outcome and which replies the agent has read, without the answer box', () => {
    const replies = [
      {
        from: { name: '', username: 'lead' },
        text: '',
        choice: 'changes',
        via: 'telegram' as const,
        at: NOW,
      },
      {
        from: { name: 'Anna', username: '' },
        text: 'and the docs',
        via: 'telegram' as const,
        choice: '',
        at: NOW,
      },
    ];
    render(
      <AskCard
        ask={ask({
          status: 'answered',
          decision: 'changes',
          answer: replies[0],
          replies,
          seenAt: NOW,
          seenReplies: 1,
        })}
        actions={{ onAnswer: async () => null, onCancel: async () => null }}
      />,
    );
    expect(screen.getByRole('status').textContent).toContain('Changes requested — @lead');
    expect(screen.getByLabelText('The agent has read it')).toBeTruthy();
    expect(screen.getByLabelText('The agent has not read it yet')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull();
  });
});

describe('PeopleSection', () => {
  it('lists the waiting asks first and the recent ones, and opens one', () => {
    const list = [
      ask({ id: 'ask_old', status: 'answered', closedAt: NOW - 10 * 24 * 3600_000 }),
      ask({
        id: 'ask_done',
        status: 'timeout',
        closedAt: NOW - 3600_000,
        to: { name: 'Anna', username: '' },
      }),
      ask({ id: 'ask_wait' }),
    ];
    const shown = peopleAsks(list, NOW);
    expect(shown.map((a) => a.id)).toEqual(['ask_wait', 'ask_done']);
    const onOpen = vi.fn();
    render(<PeopleSection asks={shown} now={NOW} onOpen={onOpen} />);
    fireEvent.click(screen.getByText('Anna'));
    expect(onOpen).toHaveBeenCalledWith('ask_done');
  });
});
