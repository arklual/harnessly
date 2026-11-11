// @vitest-environment jsdom
import { cleanup, fireEvent, render, renderHook, screen, within } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import AppearanceSection, { type IAppearanceSectionProps } from '@/next/features/settings/appearance-section';
import { useNoticeColor } from '@/next/bindings/notice-color';
import { pinLocale } from '@/next/i18n';
import { DEFAULT_SETTINGS, type TNoticeColor } from '@/next/stores/me';

beforeAll(() => pinLocale('ru'));
afterEach(cleanup);

const props = (patch: Partial<IAppearanceSectionProps> = {}): IAppearanceSectionProps => ({
  theme: 'dark',
  mode: 'dark',
  palette: 'graphite',
  textSize: 'm',
  textBusy: false,
  noticeColor: 'contrast',
  noticeBusy: false,
  onTheme: vi.fn(),
  onPalette: vi.fn(),
  onTextSize: vi.fn(),
  onNoticeColor: vi.fn(),
  ...patch,
});

describe('«Цвет уведомлений»', () => {
  it('is contrasting by default', () => {
    expect(DEFAULT_SETTINGS.noticeColor).toBe('contrast');
  });

  it('is a choice of two in «Оформление», not a native select, and saves the pick', () => {
    const p = props();
    render(<AppearanceSection {...p} />);
    const group = screen.getByRole('radiogroup', { name: 'Цвет уведомлений' });
    expect(group.querySelector('select')).toBeNull();
    expect(within(group).getByRole('radio', { name: 'Контрастные' }).getAttribute('aria-checked')).toBe(
      'true',
    );
    fireEvent.click(within(group).getByRole('radio', { name: 'Как тема' }));
    expect(p.onNoticeColor).toHaveBeenCalledWith('theme');
  });

  it('goes onto the page root, where the tokens are chosen by it', () => {
    const { rerender, unmount } = renderHook(({ color }) => useNoticeColor(color), {
      initialProps: { color: 'contrast' as TNoticeColor },
    });
    expect(document.documentElement.dataset.notices).toBe('contrast');
    rerender({ color: 'theme' });
    expect(document.documentElement.dataset.notices).toBe('theme');
    unmount();
    expect(document.documentElement.dataset.notices).toBeUndefined();
  });
});
