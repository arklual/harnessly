// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { t } from '@/next/i18n';
import PublishSection from '@/next/features/designly/publish/publish-section';
import { usePublish } from '@/next/features/designly/publish/use-publish';
import type { MachineId } from '@/next/protocol/v2';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

beforeEach(() => {
  // The wide window: the pages' menu is a popover.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});

const Host = ({ onPush }: { onPush?: () => void }) => {
  const state = usePublish({
    machine: 'm1' as MachineId,
    project: 'p1',
    title: 'Shop Landing',
    pages: [
      { path: 'a.dc.html', title: 'A' },
      { path: 'b.dc.html', title: 'B' },
    ],
  });
  return <PublishSection state={state} onPush={onPush} />;
};

describe('«Опубликовать как сайт»', () => {
  it('says the site is not published, unfolds the form, and publishes with the name and who may open it', async () => {
    let answer: (v: Response) => void = () => undefined;
    const fetchMock = vi.fn((url: string) =>
      url.includes('/design/published')
        ? Promise.resolve(
            new Response(JSON.stringify({ site: null }), { headers: { 'Content-Type': 'application/json' } }),
          )
        : new Promise<Response>((resolve) => (answer = resolve)),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(<Host />);
    expect(screen.getByText(t('next.publish.none'))).toBeTruthy();
    const unfold = screen.getByRole('button', { name: t('next.publish.go') });
    expect(unfold.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(unfold);
    expect(unfold.getAttribute('aria-expanded')).toBe('true');
    const name = screen.getByLabelText(t('next.publish.name')) as HTMLInputElement;
    // The name starts from the design's title.
    expect(name.value).toBe('shop-landing');
    fireEvent.change(name, { target: { value: 'Shop' } });
    fireEvent.click(screen.getByRole('radio', { name: new RegExp(t('next.publish.access.link')) }));
    fireEvent.submit(name.closest('form')!);
    const publishing = () =>
      (fetchMock.mock.calls as unknown as [string, RequestInit][]).find(
        ([u]) => u.includes('/design/publish?') || u.endsWith('/design/publish'),
      );
    await waitFor(() => expect(publishing()).toBeTruthy());
    const [url, init] = publishing() ?? ['', {}];
    expect(url).toContain('/design/publish');
    expect(JSON.parse(String(init.body))).toEqual({
      project: 'p1',
      name: 'shop',
      access: 'link',
      pages: null,
    });
    // Nothing is shown as published before the hub answers.
    expect(screen.queryByRole('link', { name: t('next.publish.open') })).toBeNull();
    await act(async () =>
      answer(
        new Response(JSON.stringify({ url: 'https://shop.sites.x.ru/', version: 1, name: 'shop' }), {
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    expect(await screen.findByText('https://shop.sites.x.ru/')).toBeTruthy();
    expect(screen.getByRole('link', { name: t('next.publish.open') }).getAttribute('href')).toBe(
      'https://shop.sites.x.ru/',
    );
    expect(screen.getByRole('button', { name: t('next.publish.copy') })).toBeTruthy();
    expect(screen.getByRole('button', { name: t('next.publish.update') })).toBeTruthy();
  });

  it('shows the site published before, read back from the hub after a reload (HRN-675)', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            site: { url: 'https://shop.sites.x.ru/', name: 'shop', access: 'link', version: 4 },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(<Host />);
    expect(await screen.findByText('https://shop.sites.x.ru/')).toBeTruthy();
    expect((fetchMock.mock.calls[0] as unknown as [string])[0]).toContain('/design/published?project=p1');
    fireEvent.click(screen.getByRole('button', { name: t('next.publish.settings') }));
    expect(((await screen.findByLabelText(t('next.publish.name'))) as HTMLInputElement).value).toBe('shop');
    expect(screen.getByRole('button', { name: t('next.publish.update') })).toBeTruthy();
  });

  it('refuses a name the hub would not take', () => {
    vi.stubGlobal('fetch', vi.fn());
    render(<Host />);
    fireEvent.click(screen.getByRole('button', { name: t('next.publish.go') }));
    fireEvent.change(screen.getByLabelText(t('next.publish.name')), { target: { value: 'a--b' } });
    expect(screen.getByText(t('next.publish.nameBad'))).toBeTruthy();
    expect(
      (screen.getAllByRole('button', { name: t('next.publish.go') }).at(-1) as HTMLButtonElement).disabled,
    ).toBe(true);
  });

  it('on the phone asks the dialog to push the form instead of unfolding it', () => {
    const onPush = vi.fn();
    render(<Host onPush={onPush} />);
    fireEvent.click(screen.getByRole('button', { name: t('next.publish.go') }));
    expect(onPush).toHaveBeenCalled();
    expect(screen.queryByLabelText(t('next.publish.name'))).toBeNull();
  });
});
