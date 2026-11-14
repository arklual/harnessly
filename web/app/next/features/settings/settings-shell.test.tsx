// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import SettingsShell from '@/next/features/settings/settings-shell';
import { pinLocale } from '@/next/i18n';

beforeAll(() => {
  pinLocale('ru');
  Element.prototype.scrollTo = () => undefined;
});
afterEach(cleanup);

describe('the head of Settings', () => {
  it('is the title alone: no menu button before it, on a wide window or a phone', () => {
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
});
