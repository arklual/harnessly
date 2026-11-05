// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import RefChip, { RefContext } from '@/next/features/chat/ref-chip';
import { changeLinkLabel, splitRefs } from '@/next/features/chat/refs';
import { addPaneSite, resetPaneSites } from '@/next/stores/pane-sites';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import { useRef, type ReactNode } from 'react';

afterEach(() => {
  cleanup();
  resetPaneSites();
  vi.restoreAllMocks();
});

describe('a merge request', () => {
  it('is a chip only from its link, which opens it', () => {
    const onOpen = vi.fn();
    const href = 'https://gitlab.example.com/team/harnessly/-/merge_requests/80';
    render(
      <RefContext.Provider value={{ refs: {}, onOpen }}>
        <RefChip kind="mr" label={href} href={href} text="harnessly !80" />
      </RefContext.Provider>,
    );
    fireEvent.click(screen.getByRole('link', { name: 'harnessly !80' }));
    expect(onOpen).toHaveBeenCalledWith('mr', href, href);
  });

  it('written bare is a word: the page does not guess its project', () => {
    expect(splitRefs('see harnessly !80 there')).toEqual([{ text: 'see harnessly !80 there' }]);
  });

  it('names its chip by the project and the number, a commit by its short hash', () => {
    expect(changeLinkLabel('https://gitlab.example.com/team/harnessly/-/merge_requests/80')).toBe(
      'harnessly !80',
    );
    expect(changeLinkLabel('https://github.com/ly-solutions/harnessly/pull/12')).toBe('harnessly #12');
    expect(changeLinkLabel('https://gitlab.example.com/team/harnessly/-/commit/1a2b3c4d5e6f')).toBe(
      'harnessly 1a2b3c4',
    );
    expect(changeLinkLabel('https://docs.example.com/a/plan')).toBeNull();
  });
});

// The menus mount into the page's own container, as in the app.
const Host = ({ children }: { children: ReactNode }) => {
  const ref = useRef<HTMLDivElement>(null);
  return (
    <PortalContext value={ref}>
      {children}
      <div ref={ref} />
    </PortalContext>
  );
};

describe('a link', () => {
  const chip = (href: string, onOpen = vi.fn()) => {
    render(
      <Host>
        <RefContext.Provider value={{ refs: {}, onOpen }}>
          <RefChip kind="link" label={href} href={href} text={href} />
        </RefContext.Provider>
      </Host>,
    );
    return { onOpen, link: screen.getByRole('link', { name: href }) };
  };

  it('to another site opens in a new tab of the browser, not the pane', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    const { onOpen, link } = chip('https://elsewhere.example.org/page');
    fireEvent.click(link);
    expect(open).toHaveBeenCalledWith('https://elsewhere.example.org/page', '_blank', 'noopener,noreferrer');
    expect(onOpen).not.toHaveBeenCalled();
  });

  it('to our site opens in the pane', () => {
    addPaneSite('posthog.example.com');
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    const href = 'https://posthog.example.com/project/1/dashboard/21';
    const { onOpen, link } = chip(href);
    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith('link', href, href);
    expect(open).not.toHaveBeenCalled();
  });

  it('with ⌘ or Ctrl is left to the browser, which opens it in a new tab', () => {
    addPaneSite('posthog.example.com');
    const href = 'https://posthog.example.com/x';
    const { onOpen, link } = chip(href);
    const event = new MouseEvent('click', { bubbles: true, cancelable: true, metaKey: true });
    link.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    expect(onOpen).not.toHaveBeenCalled();
    expect(link.getAttribute('target')).toBe('_blank');
  });

  it('has a menu: open in the pane, open in the browser, copy', async () => {
    pinLocale('en');
    window.matchMedia = ((query: string) => ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    })) as unknown as typeof window.matchMedia;
    const { link } = chip('https://elsewhere.example.org/page');
    fireEvent.contextMenu(link, { clientX: 10, clientY: 10 });
    expect(await screen.findByRole('menuitem', { name: 'Open in the pane' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Open in the browser' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Copy link' })).toBeTruthy();
  });

  it('to a server of the machine opens only in the pane', () => {
    const onOpen = vi.fn();
    render(
      <RefContext.Provider value={{ refs: {}, onOpen }}>
        <RefChip
          kind="link"
          label="http://localhost:4400/"
          href="http://localhost:4400/"
          text="localhost:4400"
        />
      </RefContext.Provider>,
    );
    expect(screen.queryByRole('link')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'localhost:4400' }));
    expect(onOpen).toHaveBeenCalledWith('link', 'http://localhost:4400/', 'http://localhost:4400/');
  });
});
