// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ChatAppsRow from '@/next/features/app-main/chat-apps-row';

afterEach(cleanup);

const APPS = [
  { id: 'a.one', name: 'Кешбэки' },
  { id: 'b.two', name: 'Ревью' },
];

describe('ChatAppsRow', () => {
  it('draws nothing when no app is on', () => {
    const { container } = render(<ChatAppsRow apps={[]} busy={null} onOpen={vi.fn()} onOff={vi.fn()} />);
    expect(container.firstChild).toBeNull();
  });

  it('opens the app by its name and turns it off by the small button, naming the app', () => {
    const onOpen = vi.fn();
    const onOff = vi.fn();
    render(<ChatAppsRow apps={APPS} busy={null} onOpen={onOpen} onOff={onOff} />);
    fireEvent.click(screen.getByRole('button', { name: 'Кешбэки' }));
    expect(onOpen).toHaveBeenCalledWith('a.one');
    fireEvent.click(screen.getByRole('button', { name: /Ревью.*(off|выключ)/i }));
    expect(onOff).toHaveBeenCalledWith('b.two');
  });

  it('keeps the chip until the machine takes the app away: a press changes nothing by itself', () => {
    const onOff = vi.fn();
    render(<ChatAppsRow apps={APPS} busy={null} onOpen={vi.fn()} onOff={onOff} />);
    const off = screen.getAllByRole('button').filter((b) => b.className.includes('__off'));
    fireEvent.click(off[0] as HTMLElement);
    expect(onOff).toHaveBeenCalledWith('a.one');
    expect(screen.getByText('Кешбэки')).toBeTruthy();
    expect(screen.getByText('Ревью')).toBeTruthy();
  });

  it('shuts every «off» while one change is on its way', () => {
    render(<ChatAppsRow apps={APPS} busy="a.one" onOpen={vi.fn()} onOff={vi.fn()} />);
    const off = screen.getAllByRole('button').filter((b) => b.className.includes('__off'));
    expect(off).toHaveLength(2);
    expect(off.every((b) => (b as HTMLButtonElement).disabled)).toBe(true);
  });

  it('lets a gone app leave through its closing phase, then removes it', async () => {
    vi.useFakeTimers();
    const { rerender } = render(<ChatAppsRow apps={APPS} busy={null} onOpen={vi.fn()} onOff={vi.fn()} />);
    rerender(
      <ChatAppsRow apps={[APPS[1] as (typeof APPS)[number]]} busy={null} onOpen={vi.fn()} onOff={vi.fn()} />,
    );
    const leaving = screen.getByText('Кешбэки').closest('.hx-chatapps__chip');
    expect(leaving?.getAttribute('data-leaving')).toBe('true');
    await act(async () => {
      await vi.advanceTimersByTimeAsync(400);
    });
    expect(screen.queryByText('Кешбэки')).toBeNull();
    vi.useRealTimers();
  });
});
