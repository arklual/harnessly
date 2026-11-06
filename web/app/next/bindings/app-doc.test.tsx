// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';

vi.mock('@/next/core/net/runtime-json', async (orig) => ({
  ...(await orig<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: vi.fn(),
}));
vi.mock('@/next/features/app-views/api', async (orig) => ({
  ...(await orig<typeof import('@/next/features/app-views/api')>()),
  useInstalledApps: () => ({
    list: {
      viewPort: 1,
      apps: [
        {
          id: 'acme.moodboard',
          name: 'Мудборд',
          views: [{ id: 'editor', targets: ['sinopia.acme.moodboard'] }],
        },
      ],
    },
    error: null,
  }),
}));
// The frame is the stage-3 one; here only what it is asked to show matters.
vi.mock('@/next/features/app-views/app-frame', () => ({
  default: (props: { target: string; document?: string; readOnly?: boolean; view: { id: string } }) => (
    <div
      data-testid="frame"
      data-target={props.target}
      data-document={props.document}
      data-readonly={String(props.readOnly === true)}
      data-view={props.view.id}
    />
  ),
}));
vi.mock('@/next/routes/routes', async (orig) => ({
  ...(await orig<typeof import('@/next/routes/routes')>()),
  goTo: vi.fn(),
}));
import AppDocPage from '@/next/bindings/app-doc';
import { runtimeJson } from '@/next/core/net/runtime-json';
import { goTo, ROUTE } from '@/next/routes/routes';
import { t } from '@/next/i18n';

const M = 'm1' as MachineId;
const asked = vi.mocked(runtimeJson);

const docs = {
  docs: [
    {
      document: 'idei-1',
      title: 'Идеи для лендинга',
      updatedAt: 1,
      kind: 'acme.moodboard',
      kindTitle: 'Мудборд',
      needsApp: false,
    },
  ],
};

/** The runtime answers kinds/open with `open` and the list of documents with `docs`. */
const runtime = (open: Record<string, unknown>) =>
  asked.mockImplementation(async (_m, path) => (String(path).endsWith('/kinds/docs') ? docs : open));

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  asked.mockReset();
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const page = () => render(<AppDocPage machine={M} id="idei-1" onOpenDrawer={vi.fn()} />);

describe('a document of an app type, opened', () => {
  it('mounts the app editor with the document as its target', async () => {
    runtime({
      kind: 'acme.moodboard',
      needsApp: false,
      app: 'acme.moodboard',
      editor: 'editor',
      target: 'sinopia.acme.moodboard',
      readOnly: false,
    });
    page();
    const frame = await screen.findByTestId('frame');
    expect(frame.dataset.target).toBe('sinopia.acme.moodboard');
    expect(frame.dataset.document).toBe('idei-1');
    expect(frame.dataset.view).toBe('editor');
    expect(frame.dataset.readonly).toBe('false');
    expect(screen.queryByText(t('next.appdoc.readOnly'))).toBeNull();
    expect(screen.getByText('Идеи для лендинга')).toBeTruthy();
    // It asked the runtime to open that very document.
    expect(
      asked.mock.calls.some(
        ([, path, init]) => String(path).endsWith('/kinds/open') && JSON.stringify(init).includes('idei-1'),
      ),
    ).toBe(true);
  });

  it('says the app is needed, with the way to the apps, and mounts nothing', async () => {
    runtime({ kind: 'acme.moodboard', needsApp: true });
    page();
    expect(await screen.findByText(t('next.appdoc.needsApp', { app: 'Мудборд' }))).toBeTruthy();
    expect(screen.queryByTestId('frame')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: t('next.appdoc.toApps') }));
    expect(goTo).toHaveBeenCalledWith({ kind: ROUTE.apps });
  });

  it('shows the editor in read-only mode, with a line saying why and the detail small', async () => {
    runtime({
      kind: 'acme.moodboard',
      needsApp: false,
      app: 'acme.moodboard',
      editor: 'editor',
      target: 'sinopia.acme.moodboard',
      readOnly: true,
      code: 'newer',
      reason: 'the document is of version 3, the installed app knows version 2',
    });
    page();
    expect(await screen.findByText(t('next.appdoc.readOnly'))).toBeTruthy();
    expect(screen.getByText(t('next.appdoc.code.newer'))).toBeTruthy();
    expect(screen.getByText(/version 3/)).toBeTruthy();
    const frame = await screen.findByTestId('frame');
    expect(frame.dataset.readonly).toBe('true');
    expect(frame.dataset.document).toBe('idei-1');
  });

  it('opens a document the person may only view in the editor, read-only', async () => {
    runtime({
      kind: 'acme.moodboard',
      needsApp: false,
      app: 'acme.moodboard',
      editor: 'editor',
      target: 'sinopia.acme.moodboard',
      readOnly: true,
      code: 'view-only',
    });
    page();
    expect(await screen.findByText(t('next.appdoc.code.view-only'))).toBeTruthy();
    expect((await screen.findByTestId('frame')).dataset.readonly).toBe('true');
  });

  // the switch at the head of the bar turns the document between looking and editing, both ways.
  it('goes between looking and editing by the parts of the mode switch', async () => {
    runtime({
      kind: 'acme.moodboard',
      needsApp: false,
      app: 'acme.moodboard',
      editor: 'editor',
      target: 'sinopia.acme.moodboard',
      readOnly: false,
    });
    const Routed = () => {
      const [edit, setEdit] = useState(false);
      return <AppDocPage machine={M} id="idei-1" edit={edit} onMode={setEdit} onOpenDrawer={vi.fn()} />;
    };
    render(<Routed />);
    const readOnly = () => screen.getByTestId('frame').dataset.readonly;
    await waitFor(() => expect(readOnly()).toBe('true'));
    fireEvent.click(await screen.findByRole('button', { name: t('next.site.stateEditShort') }));
    await waitFor(() => expect(readOnly()).toBe('false'));
    expect(
      screen.getByRole('button', { name: t('next.site.stateEditShort') }).getAttribute('aria-pressed'),
    ).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: t('next.site.modeView') }));
    await waitFor(() => expect(readOnly()).toBe('true'));
  });

  it('says so when the runtime refuses', async () => {
    asked.mockRejectedValue(new Error('not-found'));
    page();
    await waitFor(() => expect(screen.getByText(`${t('next.appdoc.failed')}: not-found`)).toBeTruthy());
  });
});
