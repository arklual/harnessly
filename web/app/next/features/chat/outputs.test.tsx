// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Outputs, { OutputsContext, SHOWN_BY_DEFAULT, type TShownKinds } from '@/next/features/chat/outputs';
import type { TurnItem, TurnOutput } from '@/next/features/chat/types';

afterEach(cleanup);

const page: TurnOutput = {
  kind: 'artifact',
  op: 'created',
  title: 'Разбор выката',
  ref: 'https://docs.example.com/a/razbor',
};
const design: TurnOutput = { kind: 'design', op: 'changed', title: 'landing-v2', ref: 'landing-v2' };

const item = (outputs: TurnOutput[]): TurnItem =>
  ({
    kind: 'turn',
    key: 'c:1#0',
    turn: {
      id: 'c:1#0',
      author: { provider: 'claude', role: null },
      startedAt: 1,
      endedAt: 2,
      durationMs: 1,
      toolCount: 2,
      stepCount: 2,
      status: 'done',
      failure: null,
      answerEntryId: null,
      files: [
        { path: '/home/coder/workspace/harnessly/.worktrees/x/web/app/turn.tsx', op: 'created' },
        { path: 'README.md', op: 'read' },
      ],
      outputs,
    },
    prompt: null,
    woke: null,
    steps: null,
    answer: null,
    now: null,
    wait: null,
    endNote: null,
  }) as unknown as TurnItem;

const draw = (shown: TShownKinds, outputs: TurnOutput[] = [page, design]) => {
  const onOpen = vi.fn();
  const onOpenFile = vi.fn();
  render(
    <OutputsContext value={{ shown, onOpen, onOpenFile }}>
      <Outputs item={item(outputs)} />
    </OutputsContext>,
  );
  return { onOpen, onOpenFile };
};

describe('what a turn made', () => {
  it('shows the things it made, not the files it wrote, and opens them', () => {
    const { onOpen } = draw(SHOWN_BY_DEFAULT);
    expect(screen.queryByText('turn.tsx')).toBeNull();
    // A link is a link: a tap opens it in the pane, ⌘-click and the arrow at its end in a new tab.
    const card = screen.getByRole('link', { name: /Разбор выката/ });
    expect(card.getAttribute('href')).toBe(page.ref);
    fireEvent.click(card);
    expect(onOpen).toHaveBeenCalledWith(page);
    fireEvent.click(card, { metaKey: true });
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(
      screen.getByRole('link', { name: /Открыть в новой вкладке|Open in a new tab/ }).getAttribute('target'),
    ).toBe('_blank');
    fireEvent.click(screen.getByRole('button', { name: /landing-v2/ }));
    expect(onOpen).toHaveBeenLastCalledWith(design);
  });

  it('leaves out a kind the person turned off', () => {
    draw({ ...SHOWN_BY_DEFAULT, design: false });
    expect(screen.queryByText('landing-v2')).toBeNull();
    expect(screen.getByText('Разбор выката')).toBeTruthy();
  });

  it('shows the files when the person asked for them, and a tap opens the file by its path', () => {
    const { onOpenFile } = draw({ ...SHOWN_BY_DEFAULT, file: true }, []);
    expect(screen.queryByText('README.md')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /turn\.tsx/ }));
    expect(onOpenFile).toHaveBeenCalledWith('/home/coder/workspace/harnessly/.worktrees/x/web/app/turn.tsx');
  });

  it('a task, a merge request and a stand are cards of their services', () => {
    draw(SHOWN_BY_DEFAULT, [
      { kind: 'task', op: 'created', title: 'HRN-1426', ref: 'https://tracker.example.com/issue/HRN-1426' },
      {
        kind: 'mr',
        op: 'created',
        title: 'harnessly !80',
        ref: 'https://gitlab.example.com/g/harnessly/-/merge_requests/80',
      },
      {
        kind: 'stand',
        op: 'created',
        title: 'stand-1.preview.example.com',
        ref: 'https://stand-1.preview.example.com/',
      },
      { kind: 'stand', op: 'created', title: 'localhost:4400', ref: 'http://localhost:4400/' },
    ]);
    expect(screen.getByText(/Задача|Task/)).toBeTruthy();
    expect(screen.getByText(/MR · harnessly !80/)).toBeTruthy();
    expect(screen.getAllByRole('link', { name: /Открыть в новой вкладке|Open in a new tab/ })).toHaveLength(
      3,
    );
  });

  it('draws nothing for a turn that made nothing shown', () => {
    const { container } = render(<Outputs item={item([])} />);
    expect(container.innerHTML).toBe('');
  });

  it('shows a file the agent handed over with its kind and size, «Открыть сбоку» and «Скачать»', () => {
    const zip: TurnOutput = {
      kind: 'delivery',
      op: 'created',
      title: 'Отчёт',
      ref: '/w/out/report.zip',
      name: 'report.zip',
      mime: 'application/zip',
      size: 2048,
      sha256: 'ab',
    };
    const onOpen = vi.fn();
    render(
      <OutputsContext
        value={{ shown: SHOWN_BY_DEFAULT, onOpen, downloadUrl: (path) => `/save?path=${path}` }}
      >
        <Outputs item={item([zip])} />
      </OutputsContext>,
    );
    expect(screen.getByText('ZIP · 2 KB')).toBeTruthy();
    // One line: the card opens it beside, one icon saves it; no buttons of words.
    expect(screen.queryByText(/Открыть сбоку|Open beside/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Отчёт/ }));
    expect(onOpen).toHaveBeenCalledWith(zip);
    const save = screen.getByRole('link', { name: /next.delivered.save|Скачать|Download/ });
    expect(save.getAttribute('href')).toBe('/save?path=/w/out/report.zip');
    expect(save.hasAttribute('download')).toBe(true);
    fireEvent.click(save);
    expect(onOpen).toHaveBeenCalledTimes(1);
    // The files the turn only wrote make no card of this kind.
    expect(screen.queryByText('turn.tsx')).toBeNull();
  });
});
