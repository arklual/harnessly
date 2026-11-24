// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Outputs, {
  finalFileOf,
  mergeStands,
  OutputsContext,
  SHOWN_BY_DEFAULT,
  type TShownKinds,
} from '@/next/features/chat/outputs';
import { linkOf } from '@/next/features/chat/link-output';
import { appConfig } from '@/platform/config';
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
  it('without the block «Сделано за ход», shows the things as cards, not the files it wrote, and opens them', () => {
    const { onOpen } = draw({ ...SHOWN_BY_DEFAULT, made: false });
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
    draw({ ...SHOWN_BY_DEFAULT, made: false }, [
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

  it('names the boards of a design the turn wrote, and each opens the design on itself (HRN-1691)', () => {
    const edited: TurnOutput = { ...design, pages: ['Главная.dc.html', 'Регулярные задачи.dc.html'] };
    const { onOpen } = draw(SHOWN_BY_DEFAULT, [edited]);
    const chips = screen.getAllByRole('button', { name: /^(Главная|Регулярные задачи)$/ });
    // The last one written comes first.
    expect(chips.map((b) => b.textContent)).toEqual(['Регулярные задачи', 'Главная']);
    fireEvent.click(chips[1]);
    expect(onOpen).toHaveBeenLastCalledWith(edited, 'Главная.dc.html');
    expect(screen.getByRole('button', { name: /Открыть последнюю|Open the last one/ })).toBeTruthy();
  });

  it('lists what it made at the end of the answer, «Открыть» and «Поделиться» on what is read', () => {
    const { onOpen } = draw(SHOWN_BY_DEFAULT);
    const block = screen.getByRole('region', { name: /Сделано за ход|Made this turn/ });
    expect(block.textContent).toContain('2');
    expect(screen.queryByText('turn.tsx')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Открыть в Sinopia|Open in Sinopia/ }));
    expect(onOpen).toHaveBeenLastCalledWith(design);
    const opens = screen.getAllByRole('button', { name: /^(Открыть|Open)$/ });
    fireEvent.click(opens[0]);
    expect(onOpen).toHaveBeenLastCalledWith(page);
    expect(screen.getAllByRole('button', { name: /Поделиться|Share/ })).toHaveLength(1);
  });

  it('a merge request, a task and a stand are rows of their services; a stand shows its address to copy', () => {
    draw(SHOWN_BY_DEFAULT, [
      {
        kind: 'task',
        op: 'changed',
        title: 'Программа прогонов',
        ref: 'https://tracker.example.com/issue/HRN-1426',
      },
      {
        kind: 'mr',
        op: 'created',
        title: 'Картинки без текста',
        ref: 'https://gitlab.example.com/g/harnessly/-/merge_requests/80',
      },
      {
        kind: 'stand',
        op: 'created',
        title: 'stand-1.preview.example.com',
        ref: 'https://stand-1.preview.example.com/',
      },
    ]);
    expect(screen.getByText('harnessly !80', { selector: '.hx-made__mono' })).toBeTruthy();
    expect(screen.getByText('HRN-1426', { selector: '.hx-made__mono' })).toBeTruthy();
    expect(screen.getByText('stand-1.preview.example.com', { selector: '.hx-made__url-text' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Скопировать адрес|Copy address/ })).toBeTruthy();
  });

  it('many tasks created at once are one group: three in sight, the rest by «Показать ещё»', () => {
    const tasks: TurnOutput[] = [1, 2, 3, 4, 5].map((n) => ({
      kind: 'task',
      op: 'created',
      title: `HRN-159${n}`,
      ref: `https://tracker.example.com/issue/HRN-159${n}`,
    }));
    draw(SHOWN_BY_DEFAULT, tasks);
    expect(screen.getByText(/Создано 5 задач|5 tasks created/)).toBeTruthy();
    expect(document.querySelectorAll('.hx-made__row[data-depth="1"]')).toHaveLength(3);
    fireEvent.click(screen.getByRole('button', { name: /Показать ещё 2|Show 2 more/ }));
    expect(document.querySelectorAll('.hx-made__row[data-depth="1"]')).toHaveLength(5);
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

  it('shows a handed-over picture as a picture, not as a line of a file (HRN-1458)', () => {
    const png: TurnOutput = {
      kind: 'delivery',
      op: 'created',
      title: 'Плакат',
      ref: '/w/out/poster.png',
      name: 'poster.png',
      mime: 'image/png',
      size: 4096,
    };
    const pdf: TurnOutput = {
      ...png,
      title: 'Плакат A3',
      ref: '/w/out/poster.pdf',
      name: 'poster.pdf',
      mime: 'application/pdf',
    };
    const { container } = render(
      <OutputsContext value={{ shown: SHOWN_BY_DEFAULT, viewUrl: (path) => `/view?path=${path}` }}>
        <Outputs item={item([png, pdf])} />
      </OutputsContext>,
    );
    const img = container.querySelector('.hx-pic img');
    expect(img?.getAttribute('src')).toBe('/view?path=/w/out/poster.png');
    expect(screen.queryByText('PNG · 4 KB')).toBeNull();
    expect(screen.getByText('PDF · 4 KB')).toBeTruthy();
  });

  it('keeps a picture a line where the file cannot be shown', () => {
    const png: TurnOutput = {
      kind: 'delivery',
      op: 'created',
      title: 'Иконка',
      ref: '/w/icon.svg',
      name: 'icon.svg',
    };
    const { container } = render(
      <OutputsContext value={{ shown: SHOWN_BY_DEFAULT }}>
        <Outputs item={item([png])} />
      </OutputsContext>,
    );
    expect(container.querySelector('.hx-pic')).toBeNull();
    expect(screen.getByText('Иконка')).toBeTruthy();
  });

  it('shows the pictures a finished turn drew under its answer, each once (HRN-1458)', () => {
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        disconnect() {}
      },
    );
    const drawnItem = item([
      {
        kind: 'delivery',
        op: 'created',
        title: 'Кот',
        ref: '/d/images/j1-1.png',
        name: 'j1-1.png',
        mime: 'image/png',
      },
    ]);
    drawnItem.turn.pictures = ['/d/images/j1-1.png', '/d/images/j1-2.png'];
    const { container } = render(
      <OutputsContext value={{ shown: SHOWN_BY_DEFAULT, viewUrl: (path) => `/view?path=${path}` }}>
        <Outputs item={drawnItem} />
      </OutputsContext>,
    );
    // Several pictures large are a gallery card: the strip under the large one holds each of them once.
    const srcs = [...container.querySelectorAll('.hx-gal__thumb img')].map((img) => img.getAttribute('src'));
    expect(srcs).toEqual(['/view?path=/d/images/j1-1.png', '/view?path=/d/images/j1-2.png']);
  });
});

describe('mergeStands (HRN-1623)', () => {
  it('pages of one stand are the addresses of its first card', () => {
    const made = (ref: string) => {
      const output: TurnOutput = { kind: 'stand', op: 'created', title: ref, ref };
      return { output, link: linkOf(output, appConfig())! };
    };
    const root = 'https://s1.preview.example.com/';
    const page = 'https://s1.preview.example.com/adminpanel/analytics';
    const other = 'https://s2.preview.example.com/';
    const merged = mergeStands([made(root), made(page), made(page), made(other)]);
    expect(merged.map((m) => m.output.ref)).toEqual([root, other]);
    expect(merged[0]!.link.more).toEqual([page]);
    expect(merged[1]!.link.more).toEqual([]);
  });
});

describe('the final file of a turn with a design (GitHub #125)', () => {
  const withFiles = (files: { path: string; op: 'created' | 'edited' | 'read' }[], outputs = [design]) => {
    const it = item(outputs);
    it.turn.files = files;
    return it;
  };
  it('offers the last created HTML or ZIP, not the intermediate ones', () => {
    const turn = withFiles([
      { path: '/w/draft.html', op: 'created' },
      { path: '/w/site.zip', op: 'created' },
      { path: '/w/notes.md', op: 'created' },
    ]);
    expect(finalFileOf(turn)?.ref).toBe('/w/site.zip');
    expect(finalFileOf(withFiles([{ path: '/w/old.html', op: 'edited' }]))).toBeNull();
  });
  it('is only for a turn that opened a design, and not twice for a file already handed over', () => {
    expect(finalFileOf(withFiles([{ path: '/w/a.html', op: 'created' }], [page]))).toBeNull();
    const handed: TurnOutput = { kind: 'delivery', op: 'created', title: 'a', ref: '/w/a.html' };
    expect(finalFileOf(withFiles([{ path: '/w/a.html', op: 'created' }], [design, handed]))).toBeNull();
  });
  it('draws a save link on the card, whatever the preview does', () => {
    const turn = withFiles([{ path: '/w/a.html', op: 'created' }]);
    render(
      <OutputsContext value={{ shown: SHOWN_BY_DEFAULT, downloadUrl: (path) => `/dl?path=${path}` }}>
        <Outputs item={turn} />
      </OutputsContext>,
    );
    const save = screen.getByRole('link', { name: /next.delivered.save|Скачать|Download/ });
    expect(save.getAttribute('href')).toBe('/dl?path=/w/a.html');
  });
});
