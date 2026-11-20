// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import type { FileContent } from '@/next/protocol/v2';
import type { IFileEditorHandle } from '@/next/features/pane/editor/file-editor';
import NotebookView from './notebook-view';
import type { NotebookViewProps } from './notebook-view';

const request = vi.hoisted(() => vi.fn());
vi.mock('./notebook-api', () => ({ notebookRequest: request }));
vi.mock('@/next/features/pane/editor/file-editor', () => ({ default: () => <p>JSON editor fallback</p> }));
vi.mock('./notebook-source', () => ({
  default: ({
    text,
    label,
    readOnly,
    onChange,
  }: {
    text: string;
    label: string;
    readOnly: boolean;
    onChange: (text: string) => void;
  }) => (
    <textarea
      aria-label={label}
      value={text}
      readOnly={readOnly}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}));
beforeAll(() => pinLocale('en'));
beforeEach(() =>
  request.mockImplementation(async (_r, _p, action) =>
    action === 'execute'
      ? {
          outputs: [{ output_type: 'stream', name: 'stdout', text: 'executed' }],
          execution_count: 1,
          status: 'ok',
        }
      : { state: action === 'shutdown' ? 'stopped' : 'idle' },
  ),
);
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const notebook = () => ({
  nbformat: 4,
  nbformat_minor: 5,
  metadata: { kernelspec: { name: 'python3' } },
  cells: [
    { cell_type: 'markdown', metadata: {}, source: '# Example' },
    { cell_type: 'code', metadata: { keep: true }, source: 'x = 4', outputs: [], execution_count: null },
    { cell_type: 'code', metadata: {}, source: 'print(x)', outputs: [], execution_count: null },
  ],
});

const show = (more: Partial<NotebookViewProps> = {}) => {
  let handle: IFileEditorHandle | null = null;
  const text = JSON.stringify(notebook());
  const props: NotebookViewProps = {
    path: 'example.ipynb',
    initial: { text, base: text, revision: 'rev1' },
    runtime: { machine: 'machine' as never, conv: 'chat' as never },
    onHandle: (next) => {
      handle = next;
    },
    onState: vi.fn(),
    onLeave: vi.fn(),
    onReload: vi.fn(),
    onSave: vi.fn(async () => ({ revision: 'rev2' }) as FileContent),
    ...more,
  };
  const result = render(<NotebookView {...props} />);
  return { ...result, props, save: () => handle?.save() };
};

const allow = () => fireEvent.click(screen.getByRole('button', { name: 'Allow execution' }));

describe('native notebook editor', () => {
  it('opens saved cells without starting a kernel or executing code', async () => {
    show();
    await waitFor(() => expect(request).toHaveBeenCalledTimes(1));
    expect(request.mock.calls[0].slice(2)).toEqual([]);
    expect(screen.getByRole('heading', { name: 'Example' })).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Run all' }) as HTMLButtonElement).disabled).toBe(true);
    allow();
    expect(request).toHaveBeenCalledTimes(1);
  });

  it('runs code cells in order and saves results with the original revision and metadata', async () => {
    const view = show();
    allow();
    fireEvent.click(screen.getByRole('button', { name: 'Run all' }));
    await waitFor(() => expect(screen.getAllByText('executed')).toHaveLength(2));
    const calls = request.mock.calls.filter((c) => c[2] === 'execute');
    expect(calls.map((c) => c[3])).toEqual(['x = 4', 'print(x)']);
    await waitFor(() =>
      expect((screen.getByRole('button', { name: 'Run all' }) as HTMLButtonElement).disabled).toBe(false),
    );
    await act(async () => {
      expect(await view.save()).toBe(true);
    });
    const save = vi.mocked(view.props.onSave).mock.calls[0];
    expect(save.slice(1)).toEqual(['rev1', false]);
    expect(JSON.parse(save[0]).cells[1]).toMatchObject({
      metadata: { keep: true },
      execution_count: 1,
      outputs: [{ text: 'executed' }],
    });
  });

  it('keeps edits as a draft without writing on a run or navigation', async () => {
    const view = show();
    fireEvent.change(screen.getByRole('textbox', { name: 'Cell 2 source' }), { target: { value: 'x = 7' } });
    view.unmount();
    expect(view.props.onSave).not.toHaveBeenCalled();
    const draft = vi.mocked(view.props.onLeave).mock.calls[0][0];
    expect(JSON.parse(draft!.text).cells[1].source).toBe('x = 7');
    expect(draft!.revision).toBe('rev1');
  });

  it('stops run-all on a cell error', async () => {
    request.mockImplementation(async (_r, _p, action) =>
      action === 'execute'
        ? {
            outputs: [{ output_type: 'error', ename: 'ValueError', evalue: 'bad input' }],
            execution_count: 1,
            status: 'error',
          }
        : { state: 'idle' },
    );
    show();
    allow();
    fireEvent.click(screen.getByRole('button', { name: 'Run all' }));
    await screen.findByText('ValueError: bad input');
    expect(request.mock.calls.filter((c) => c[2] === 'execute')).toHaveLength(1);
  });

  it('does not execute notebooks declaring a different kernel', () => {
    const doc = notebook();
    doc.metadata.kernelspec.name = 'julia';
    const text = JSON.stringify(doc);
    show({ initial: { text, base: text, revision: 'rev1' } });
    expect(screen.queryByRole('button', { name: 'Allow execution' })).toBeNull();
    expect((screen.getByRole('button', { name: 'Run all' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('allows an explicitly trusted custom-named Python environment without trusting its executable metadata', async () => {
    const doc = {
      ...notebook(),
      metadata: { kernelspec: { name: 'project-python', language: 'python', argv: ['never-run-metadata'] } },
    };
    const text = JSON.stringify(doc);
    show({ initial: { text, base: text, revision: 'rev1' } });
    expect(screen.queryByText(/This notebook uses another kernel/)).toBeNull();
    allow();
    fireEvent.click(screen.getAllByRole('button', { name: /^Run$/ })[0]);
    await screen.findByText('executed');
    expect(request.mock.calls.find((c) => c[2] === 'execute')?.[3]).toBe('x = 4');
  });

  it('leaves malformed notebooks available in the text editor', () => {
    show({ initial: { text: '{', base: '{', revision: 'rev1' } });
    expect(screen.getByText('JSON editor fallback')).toBeTruthy();
  });

  it('honors read-only handovers without editing, saving or requesting a kernel', async () => {
    const view = show({ readOnly: true });
    expect(screen.queryByRole('button', { name: 'Allow execution' })).toBeNull();
    expect((screen.getByRole('button', { name: 'Run all' }) as HTMLButtonElement).disabled).toBe(true);
    const source = screen.getByRole('textbox', { name: 'Cell 2 source' }) as HTMLTextAreaElement;
    expect(source.readOnly).toBe(true);
    fireEvent.change(source, { target: { value: 'must not change' } });
    await act(async () => {
      expect(await view.save()).toBe(false);
    });
    view.unmount();
    expect(view.props.onSave).not.toHaveBeenCalled();
    expect(view.props.onLeave).toHaveBeenCalledWith(null);
    expect(request).not.toHaveBeenCalled();
  });

  it('renders saved HTML only inside a scriptless, networkless sandbox', () => {
    const doc = notebook();
    const withOutput = {
      ...doc,
      cells: [
        {
          ...doc.cells[1],
          outputs: [
            {
              output_type: 'display_data',
              data: {
                'text/html': '<script>window.top.alert(1)</script><table><tr><td>safe</td></tr></table>',
              },
              metadata: {},
            },
          ],
        },
      ],
    };
    const text = JSON.stringify(withOutput);
    show({ initial: { text, base: text, revision: 'rev1' } });
    const frame = screen.getByTitle('Cell HTML output');
    expect(frame.getAttribute('sandbox')).toBe('');
    expect(frame.getAttribute('srcdoc')).toContain("default-src 'none'");
    expect(document.querySelector('script')).toBeNull();
  });

  it('interrupts on leaving and never starts another cell of the abandoned queue', async () => {
    let finish: ((value: unknown) => void) | undefined;
    request.mockImplementation((_r, _p, action) =>
      action === 'execute'
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : Promise.resolve({ state: 'idle' }),
    );
    const view = show();
    allow();
    fireEvent.click(screen.getByRole('button', { name: 'Run all' }));
    await waitFor(() => expect(finish).toBeDefined());
    view.unmount();
    await act(async () => {
      finish!({ outputs: [], execution_count: 1, status: 'ok' });
    });
    expect(request.mock.calls.some((c) => c[2] === 'interrupt')).toBe(true);
    expect(request.mock.calls.filter((c) => c[2] === 'execute')).toHaveLength(1);
  });
});
