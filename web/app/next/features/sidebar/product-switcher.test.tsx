// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import ProductSwitcher from '@/next/features/sidebar/product-switcher';
import { createRef } from 'react';
import { readFileSync } from 'node:fs';
import { PortalContext } from '@/next/ui/portal';

afterEach(cleanup);

beforeAll(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});

describe('ProductSwitcher', () => {
  it('opens a menu with the hub as a link and the workspace as the current row', async () => {
    const host = createRef<HTMLDivElement>();
    render(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <ProductSwitcher brand="Harnessly" hubHref="https://hub.example.com/" />
      </PortalContext.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: /Переключить продукт|Switch product/ }));
    const hub = await screen.findByRole('menuitem', { name: /Хаб|Hub/ });
    expect(hub.getAttribute('href')).toBe('https://hub.example.com/');
    const here = screen.getAllByRole('menuitem').find((el) => el.getAttribute('aria-current') === 'page');
    expect(here).toBeTruthy();
  });
});

describe('the old hub entries are gone', () => {
  const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8');
  it('«Ещё» and «Управление» do not link to the hub any more', () => {
    expect(read('../../bindings/shell.tsx')).not.toMatch(/id: 'hub'|hubHref\(\)/);
    expect(read('../manage/tabs.tsx')).not.toMatch(/hub/i);
    expect(read('../../styles/manage.css')).not.toMatch(/mg-tab--hub/);
    expect(read('./sidebar.tsx')).not.toMatch(/backToHub/);
  });
});
