// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { t } from '@/next/i18n';
import { SiteHeld } from '@/next/features/designly/site-loader';

afterEach(cleanup);

const held = () => document.querySelector('.hx-site-held') as HTMLElement;
const frameWin = () => (document.querySelector('.hx-site-held__frame') as HTMLIFrameElement).contentWindow;
const tell = (source: MessageEventSource | null, type: string) =>
  act(() => {
    window.dispatchEvent(new MessageEvent('message', { data: { type }, source }));
  });

describe('the board held while editing opens (HRN-1455)', () => {
  it('keeps the copy unseen until its runtime says it is drawn', () => {
    render(<SiteHeld src="about:blank" title="Главная" />);
    expect(held().dataset.drawn).toBe('false');
    expect(screen.queryByText(t('next.site.loadEdit'))).toBeNull();
    expect(document.querySelector('.hx-site-load')).not.toBeNull();

    tell(frameWin(), 'tk:drawn');
    expect(held().dataset.drawn).toBe('true');
    expect(screen.getByText(t('next.site.loadEdit'))).toBeTruthy();
    expect(document.querySelector('.hx-site-load')).toBeNull();
  });

  it('takes the word of its own frame only', () => {
    render(<SiteHeld src="about:blank" title="Главная" />);
    tell(window, 'tk:drawn');
    tell(frameWin(), 'tk:lib-ready');
    expect(held().dataset.drawn).toBe('false');
  });
});
