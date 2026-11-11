// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { pinLocale } from '@/next/i18n';

vi.mock('@/next/features/designly/sdk/ui', () => ({
  Modal: ({ open, title, children }: { open: boolean; title: string; children: ReactNode }) =>
    open ? (
      <div role="dialog" aria-label={title}>
        {children}
      </div>
    ) : null,
}));
const uploadFile = vi.fn();
vi.mock('@/next/commands/uploads', () => ({
  uploadFile: (...a: unknown[]) => uploadFile(...a),
  UPLOAD_FAILURE: { network: 'network', refused: 'refused', aborted: 'aborted' },
}));

import { usePackExport, usePackImport } from '@/next/features/designly/pack-dialogs';

pinLocale('ru');

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', ...headers } });

const Export = ({ ids }: { ids: string[] }) => {
  const pack = usePackExport('m1' as never);
  return (
    <>
      <button type="button" onClick={() => pack.start(ids)}>
        go
      </button>
      {pack.dialog}
    </>
  );
};

const Import = ({ onImported, onOpen }: { onImported: () => void; onOpen: (id: string) => void }) => {
  const pack = usePackImport('m1' as never, onImported, onOpen);
  return (
    <>
      <button type="button" onClick={pack.pick}>
        pick
      </button>
      {pack.dialog}
    </>
  );
};

const fetchMock = vi.fn();
beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  URL.createObjectURL = vi.fn(() => 'blob:x');
  URL.revokeObjectURL = vi.fn();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const urlOf = (call: number) => String(fetchMock.mock.calls[call]?.[0]);

describe('pack export', () => {
  it('saves the file once the machine made it, asking for every chosen project', async () => {
    fetchMock.mockResolvedValueOnce(
      new Response('pack', {
        status: 200,
        headers: { 'content-disposition': 'attachment; filename="a.sinopia-pack.tar.gz"' },
      }),
    );
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    render(<Export ids={['p1', 'p2']} />);
    fireEvent.click(screen.getByText('go'));
    await waitFor(() => expect(click).toHaveBeenCalled());
    expect(urlOf(0)).toContain('project=p1');
    expect(urlOf(0)).toContain('project=p2');
    expect(urlOf(0)).not.toContain('acceptRestricted');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('asks before a restricted kit goes into the file, and downloads nothing without the word', async () => {
    fetchMock.mockResolvedValueOnce(
      json(409, { error: 'x', restricted: [{ path: 'fonts/a.woff2', font: 'Acme Sans', reason: 'r' }] }),
    );
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    render(<Export ids={['p1']} />);
    fireEvent.click(screen.getByText('go'));
    await screen.findByText('Acme Sans');
    expect(screen.getByText(/нельзя отдавать наружу/)).toBeTruthy();
    expect(click).not.toHaveBeenCalled();

    fetchMock.mockResolvedValueOnce(new Response('pack', { status: 200 }));
    await act(async () => fireEvent.click(screen.getByText('Понимаю, скачать')));
    await waitFor(() => expect(click).toHaveBeenCalled());
    expect(urlOf(1)).toContain('acceptRestricted=1');
  });

  it("says a refusal in the machine's words", async () => {
    fetchMock.mockResolvedValueOnce(json(422, { error: 'Проект pinned to a kit that is not installed' }));
    render(<Export ids={['p1']} />);
    fireEvent.click(screen.getByText('go'));
    await screen.findByText('Проект pinned to a kit that is not installed');
  });
});

const PLAN = {
  manifest: {
    projects: [
      { id: 'one', title: 'Лендинг' },
      { id: 'two', title: 'Дека' },
    ],
  },
  projects: [
    { id: 'one', as: 'one-2', status: 'renamed' },
    { id: 'two', as: 'two', status: 'unchanged' },
  ],
  kits: [{ id: 'acme', version: '1.0.0', status: 'added' }],
};

const choose = () => {
  const input = document.querySelector<HTMLInputElement>('input[type=file]')!;
  fireEvent.change(input, { target: { files: [new File(['x'], 'p.tar.gz')] } });
};

describe('pack import', () => {
  it('shows the dry run first, writes only after the confirmation, then links the projects', async () => {
    uploadFile.mockResolvedValue({ ok: true, ref: { id: 'u1' } });
    fetchMock.mockResolvedValueOnce(json(200, { ...PLAN, dryRun: true }));
    const onImported = vi.fn();
    const onOpen = vi.fn();
    render(<Import onImported={onImported} onOpen={onOpen} />);
    choose();
    await screen.findByText('Загрузить');
    expect(urlOf(0)).toContain('dryRun=1');
    expect(screen.getByText('Лендинг')).toBeTruthy();
    expect(screen.getByText('acme 1.0.0')).toBeTruthy();
    expect(screen.getByText(/уже есть здесь/)).toBeTruthy();
    expect(onImported).not.toHaveBeenCalled();

    fetchMock.mockResolvedValueOnce(json(200, { ...PLAN, dryRun: false }));
    await act(async () => fireEvent.click(screen.getByText('Загрузить')));
    const open = await screen.findByText('Открыть «Лендинг»');
    expect(urlOf(1)).not.toContain('dryRun');
    expect(onImported).toHaveBeenCalledTimes(1);
    fireEvent.click(open);
    expect(onOpen).toHaveBeenCalledWith('one-2');
  });

  it('says there is nothing to upload when everything is here', async () => {
    uploadFile.mockResolvedValue({ ok: true, ref: { id: 'u1' } });
    fetchMock.mockResolvedValueOnce(
      json(200, {
        ...PLAN,
        projects: [{ id: 'two', as: 'two', status: 'unchanged' }],
        kits: [{ id: 'acme', version: '1', status: 'unchanged' }],
      }),
    );
    render(<Import onImported={vi.fn()} onOpen={vi.fn()} />);
    choose();
    await screen.findByText(/Загружать нечего/);
    expect(screen.queryByText('Загрузить')).toBeNull();
  });

  it('refuses a conflict with the machine words and the kits in conflict, with no import button', async () => {
    uploadFile.mockResolvedValue({ ok: true, ref: { id: 'u1' } });
    fetchMock.mockResolvedValueOnce(
      json(409, {
        error: 'Кит acme 1.0.0 уже стоит с другим содержимым',
        conflict: [{ id: 'acme', version: '1.0.0', status: 'conflict' }],
      }),
    );
    render(<Import onImported={vi.fn()} onOpen={vi.fn()} />);
    choose();
    await screen.findByText('Кит acme 1.0.0 уже стоит с другим содержимым');
    expect(screen.getByText('acme 1.0.0')).toBeTruthy();
    expect(screen.queryByText('Загрузить')).toBeNull();
  });

  it('shows the progress of a big file while it is sent', async () => {
    let report: (done: number, total: number) => void = () => undefined;
    uploadFile.mockImplementation((_m: unknown, _f: unknown, cb: typeof report) => {
      report = cb;
      return new Promise(() => undefined);
    });
    render(<Import onImported={vi.fn()} onOpen={vi.fn()} />);
    choose();
    act(() => report(40 * 1024 * 1024, 80 * 1024 * 1024));
    await screen.findByText(/50%/);
  });
});
