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

describe('PeerNotes', () => {
  it('draws who, what of, and the body as Markdown with its path opening as a file', async () => {
    const onOpen = vi.fn();
    const { container } = render(
      <RefContext value={{ refs: {}, onOpen }}>
        <PeerNotes entry={entry(LONG)} />
      </RefContext>,
    );
    expect(screen.getByText('rev-t4')).toBeTruthy();
    expect(screen.getByText('finished')).toBeTruthy();
    expect(container.querySelector('.hx-pnote')?.getAttribute('data-color')).toBe('yellow');
    const chip = await screen.findByText('/home/coder/task-4-review.md', {}, { timeout: 20000 });
    fireEvent.click(chip);
    expect(onOpen).toHaveBeenCalledWith('file', '/home/coder/task-4-review.md', null);
    const more = screen.getByRole('button', { name: 'Show all' });
    act(() => more.click());
    expect(screen.getByRole('button', { name: 'Collapse' })).toBeTruthy();
  });

  it('names a shutdown in words, without its ids', () => {
    const shut: PeerEntry = {
      ...entry(''),
      peers: [
        {
          from: 'scout',
          kind: 'event',
          color: 'blue',
          event: 'shutdown_approved',
          summary: null,
          sentAt: null,
          agentId: null,
          model: null,
          md: '',
        },
      ],
    };
    const { container } = render(<PeerNotes entry={shut} />);
    expect(screen.getByText('shut down')).toBeTruthy();
    expect(container.querySelector('.hx-pnote__body')).toBeNull();
  });

  it('signs a subagent message with its task, says who writes to whom and opens the subagent on a click', () => {
    const sub: PeerEntry = {
      ...entry(''),
      peers: [
        {
          from: 'Debug stuck delivered queue items',
          kind: 'message',
          color: null,
          event: null,
          summary: null,
          sentAt: null,
          agentId: 'a68d2e2f655d5c31e',
          model: 'sonnet',
          md: 'Root cause found in **feeds**.',
        },
      ],
    };
    const opened = vi.fn();
    const off = onOpenSubagent(opened);
    const { container } = render(<PeerNotes entry={sub} />);
    expect(screen.getByText('Subagent → agent')).toBeTruthy();
    expect(screen.getByText('sonnet')).toBeTruthy();
    expect(container.textContent).not.toContain('a68d2e2f655d5c31e');
    expect(container.querySelector('.hx-agent')?.getAttribute('data-agent')).toBe('claude');
    fireEvent.click(screen.getByRole('button', { name: 'Debug stuck delivered queue items' }));
    expect(opened).toHaveBeenCalledWith('a68d2e2f655d5c31e');
    off();
  });

  it('never shows the hex id of a subagent it has no task for', () => {
    const sub: PeerEntry = {
      ...entry(''),
      peers: [
        {
          from: '',
          kind: 'message',
          color: null,
          event: null,
          summary: null,
          sentAt: null,
          agentId: 'a68d2e2f655d5c31e',
          model: null,
          md: 'hi',
        },
      ],
    };
    const { container } = render(<PeerNotes entry={sub} />);
    expect(screen.getByRole('button', { name: 'Subagent' })).toBeTruthy();
    expect(container.textContent).not.toContain('a68d2e2f655d5c31e');
  });

  it('never renders markup from the body', async () => {
    const { container } = render(<PeerNotes entry={entry('<img src=x onerror=alert(1)><b>bold</b> text')} />);
    await screen.findByText(/text/, {}, { timeout: 20000 });
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
