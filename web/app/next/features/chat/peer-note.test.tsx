// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import PeerNotes from '@/next/features/chat/peer-note';
import { RefContext } from '@/next/features/chat/ref-chip';
import { onOpenSubagent } from '@/next/features/subagents/open';
import SystemNote from '@/next/features/chat/system-note';
import type { NoticeEntry, PeerEntry } from '@/next/features/chat/types';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const LONG =
  'Spec ❌, quality needs fixes. Full review: `/home/coder/task-4-review.md`\n\n**Findings**\n' +
  Array.from({ length: 8 }, (_, i) => `- finding ${i + 1}`).join('\n');

const entry = (md: string): PeerEntry => ({
  id: 'c:a#0',
  turnId: 'c:a#0',
  at: Date.now(),
  rev: 1,
  t: 'peer',
  peers: [
    {
      from: 'rev-t4',
      kind: 'done',
      color: 'yellow',
      event: null,
      summary: null,
      sentAt: null,
      agentId: null,
      model: null,
      md,
    },
  ],
});

const note = (over: Partial<PeerEntry['peers'][number]>): PeerEntry['peers'][number] => ({
  from: 'rev-t4',
  kind: 'done',
  color: 'yellow',
  event: null,
  summary: null,
  sentAt: null,
  agentId: null,
  model: null,
  md: '',
  ...over,
});

describe('PeerNotes', () => {
  it('says where it comes from, folds to plain lines and opens with a file chip and its actions', async () => {
    const onOpen = vi.fn();
    const opened = vi.fn();
    const off = onOpenSubagent(opened);
    const { container } = render(
      <RefContext value={{ refs: {}, onOpen }}>
        <PeerNotes
          entry={{ ...entry(''), peers: [note({ md: LONG, task: 'Review task 4', opens: 'arev4' })] }}
        />
      </RefContext>,
    );
    expect(screen.getByText('Background agent')).toBeTruthy();
    expect(screen.getByText('Review task 4')).toBeTruthy();
    expect(screen.getByText('rev-t4')).toBeTruthy();
    expect(screen.getByText('done')).toBeTruthy();
    expect(container.querySelector('.hx-pnote')?.getAttribute('data-color')).toBeNull();
    // Folded: the gist names the file, not its whole path, and there is no Markdown yet.
    expect(container.querySelector('.hx-pnote__gist')?.textContent).toContain('task-4-review.md');
    expect(container.querySelector('.hx-pnote__gist')?.textContent).not.toContain('/home/coder');
    expect(container.querySelector('.hx-pnote__body')).toBeNull();
    const head = container.querySelector('button.hx-pnote__head') as HTMLButtonElement;
    act(() => head.click());
    expect(head.getAttribute('aria-expanded')).toBe('true');
    const chip = await screen.findByText(
      'task-4-review.md',
      { selector: '.hx-ref__label' },
      { timeout: 20000 },
    );
    expect(chip.closest('.hx-ref')?.getAttribute('title')).toBe('/home/coder/task-4-review.md');
    fireEvent.click(chip);
    expect(onOpen).toHaveBeenCalledWith('file', '/home/coder/task-4-review.md', null);
    fireEvent.click(screen.getByRole('button', { name: 'Open file' }));
    expect(onOpen).toHaveBeenLastCalledWith('file', '/home/coder/task-4-review.md', null);
    fireEvent.click(screen.getByRole('button', { name: "Open the agent's conversation" }));
    expect(opened).toHaveBeenCalledWith('arev4');
    off();
  });

  it('marks a failed agent and counts several agents of one delivery', () => {
    const { container } = render(
      <PeerNotes
        entry={{
          ...entry(''),
          peers: [note({ md: 'ok', task: 'One' }), note({ from: 'b', md: 'no', task: 'Two', failed: true })],
        }}
      />,
    );
    expect(screen.getByText('2 background agents finished')).toBeTruthy();
    expect(container.querySelectorAll('.hx-pnote')[1].getAttribute('data-tone')).toBe('failed');
    expect(screen.getByText('failed')).toBeTruthy();
  });

  it('names a shutdown in words, without its ids', () => {
    const { container } = render(
      <PeerNotes
        entry={{ ...entry(''), peers: [note({ from: 'scout', kind: 'event', event: 'shutdown_approved' })] }}
      />,
    );
    expect(screen.getByText('shut down')).toBeTruthy();
    expect(container.querySelector('button.hx-pnote__head')).toBeNull();
  });

  it('signs a subagent message with its task and opens the subagent from it', () => {
    const opened = vi.fn();
    const off = onOpenSubagent(opened);
    const { container } = render(
      <PeerNotes
        entry={{
          ...entry(''),
          peers: [
            note({
              from: 'Debug stuck delivered queue items',
              kind: 'message',
              color: null,
              agentId: 'a68d2e2f655d5c31e',
              model: 'sonnet',
              md: 'Root cause found in **feeds**.',
            }),
          ],
        }}
      />,
    );
    expect(screen.getByText('Debug stuck delivered queue items')).toBeTruthy();
    expect(screen.getByText('sonnet')).toBeTruthy();
    expect(container.textContent).not.toContain('a68d2e2f655d5c31e');
    act(() => (container.querySelector('button.hx-pnote__head') as HTMLButtonElement).click());
    fireEvent.click(screen.getByRole('button', { name: "Open the agent's conversation" }));
    expect(opened).toHaveBeenCalledWith('a68d2e2f655d5c31e');
    off();
  });

  it('never shows the hex id of a subagent it has no task for', () => {
    const { container } = render(
      <PeerNotes
        entry={{
          ...entry(''),
          peers: [note({ from: '', kind: 'message', color: null, agentId: 'a68d2e2f655d5c31e', md: 'hi' })],
        }}
      />,
    );
    expect(screen.getByText('Subagent')).toBeTruthy();
    expect(container.textContent).not.toContain('a68d2e2f655d5c31e');
  });

  it('never renders markup from the body', async () => {
    const { container } = render(<PeerNotes entry={entry('<img src=x onerror=alert(1)><b>bold</b> text')} />);
    act(() => (container.querySelector('button.hx-pnote__head') as HTMLButtonElement).click());
    await screen.findByText(/text/, { selector: '.hx-pnote__text *' }, { timeout: 20000 });
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('b')).toBeNull();
  });
});

describe('SystemNote', () => {
  const note = (detail: string): NoticeEntry => ({
    id: 'c:b#0',
    turnId: 'c:b#0',
    at: 0,
    rev: 1,
    t: 'notice',
    level: 'info',
    code: 'system-note',
    detail,
  });

  it('is one line; its details unfold', () => {
    render(<SystemNote entry={note('Monitor fired\ntask-id: b9')} />);
    expect(screen.queryByText('task-id: b9')).toBeNull();
    act(() => screen.getByRole('button', { name: /Monitor fired/ }).click());
    expect(screen.getByText('task-id: b9')).toBeTruthy();
  });

  it('a note with nothing more is not a button', () => {
    render(<SystemNote entry={note('Goal check-in: background work still running')} />);
    expect(screen.getByText('Goal check-in: background work still running')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });
});
