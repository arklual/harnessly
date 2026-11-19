// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import SettingsShell from '@/next/features/settings/settings-shell';
import { pinLocale } from '@/next/i18n';

beforeAll(() => {
  pinLocale('ru');
  Element.prototype.scrollTo = () => undefined;
});
afterEach(cleanup);

describe('the head of Settings', () => {
  it('is the title alone without a way out: no menu button before it, on a wide window or a phone', () => {
    for (const pageOpen of [true, false]) {
      render(
        <SettingsShell section="general" pageOpen={pageOpen} lead="" onPick={vi.fn()} onBack={vi.fn()}>
          <p>page</p>
        </SettingsShell>,
      );
      const head = document.querySelector('.hx-set__nav-head');
      expect(head?.textContent).toBe('Настройки');
      expect(head?.querySelector('button, svg')).toBeNull();
      expect(screen.queryByRole('button', { name: /меню|menu/i })).toBeNull();
      cleanup();
    }
  });

  it('has a way out to the chats before the title when the screen gives one', () => {
    const onLeave = vi.fn();
    render(
      <SettingsShell
        section="general"
        pageOpen={false}
        lead=""
        onPick={vi.fn()}
        onBack={vi.fn()}
        onLeave={onLeave}
      >
        <p>page</p>
      </SettingsShell>,
    );
    const head = document.querySelector('.hx-set__nav-head');
    const leave = within(head as HTMLElement).getByRole('button', { name: 'Чаты' });
    expect(head?.firstElementChild).toBe(leave);
    expect(head?.textContent).toBe('Настройки');
    fireEvent.click(leave);
    expect(onLeave).toHaveBeenCalledTimes(1);
  });

  it('keeps the way back from a section to the list apart from the way out', () => {
    const onBack = vi.fn();
    const onLeave = vi.fn();
    render(
      <SettingsShell section="machines" pageOpen lead="" onPick={vi.fn()} onBack={onBack} onLeave={onLeave}>
        <p>page</p>
      </SettingsShell>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Все настройки' }));
    expect(onBack).toHaveBeenCalledTimes(1);
    expect(onLeave).not.toHaveBeenCalled();
  });
});
