// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useState } from 'react';
import ModeButton from '@/next/features/designly/mode-button';
import type { ISiteMode } from '@/next/features/designly/site-view';
import { pinLocale, t } from '@/next/i18n';

pinLocale('ru');
afterEach(cleanup);

const part = (k: 'view' | 'edit') => document.querySelector<HTMLButtonElement>(`[data-k='mode-${k}']`)!;
const press = (k: 'view' | 'edit') => act(() => part(k).click());

/** The bars' way: the mode is the owner's state, the switch only asks for the other one. */
const Owner = ({ ask = false }: { ask?: boolean }) => {
  const [editing, setEditing] = useState(false);
  const mode: ISiteMode = {
    state: editing ? 'Редактирование макета' : 'Макет',
    editing,
    action: { onClick: () => setEditing(!editing), ask },
  };
  return <ModeButton mode={mode} />;
};

describe('ModeButton', () => {
  it('lights the part of the mode the screen is in and goes to the other on its click', () => {
    render(<Owner />);
    expect(part('view').getAttribute('aria-pressed')).toBe('true');
    expect(part('edit').getAttribute('aria-pressed')).toBe('false');
    press('edit');
    expect(part('edit').getAttribute('aria-pressed')).toBe('true');
    expect(part('view').getAttribute('aria-pressed')).toBe('false');
    press('view');
    expect(part('view').getAttribute('aria-pressed')).toBe('true');
  });

  it('does nothing on a click of the lit part', () => {
    const onClick = vi.fn();
    render(
      <ModeButton
        mode={{ state: 'Редактирование макета', editing: true, action: { onClick, ask: false } }}
      />,
    );
    press('edit');
    expect(onClick).not.toHaveBeenCalled();
    press('view');
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('names both parts the same way in both modes', () => {
    const { rerender } = render(<Owner />);
    const words = () => [part('view').textContent, part('edit').textContent];
    const looking = words();
    press('edit');
    expect(words()).toEqual(looking);
    rerender(<Owner />);
    expect(screen.getByRole('group').getAttribute('aria-label')).toBe(t('next.site.modeSwitch'));
  });

  it('asks for access from the editing part where editing is not allowed', () => {
    const onClick = vi.fn();
    render(<ModeButton mode={{ state: 'Макет', editing: false, action: { onClick, ask: true } }} />);
    expect(part('edit').dataset.ask).toBe('true');
    expect(part('edit').getAttribute('aria-label')).toBe(t('next.site.modeAsk'));
    press('edit');
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('is one quiet label without a way to the other mode', () => {
    render(<ModeButton mode={{ state: 'Документ', editing: false, action: null }} />);
    expect(screen.queryAllByRole('button')).toHaveLength(0);
    expect(screen.getByText('Документ')).toBeTruthy();
  });

  it('keeps the icons and hints on a phone, without the words', () => {
    render(
      <ModeButton
        mode={{ state: 'Макет', editing: false, action: { onClick: vi.fn(), ask: false } }}
        compact
      />,
    );
    expect(screen.getByRole('group').dataset.compact).toBe('true');
    expect(part('view').getAttribute('aria-label')).toBe(t('next.site.modeView'));
    expect(part('edit').getAttribute('aria-label')).toBe(t('next.site.stateEditShort'));
  });
});
