// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import FieldFold, { type IFieldFoldProps } from '@/next/features/composer/field-fold';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('ru'));
afterEach(cleanup);

const fold = (over: Partial<IFieldFoldProps> = {}) => (
  <FieldFold hint="Что поменять?" line={null} model="Fable" limit={null} working={false} empty {...over}>
    <textarea aria-label="поле" />
  </FieldFold>
);

const box = () => document.querySelector<HTMLElement>('.hx-fold')!;

describe('the folded field', () => {
  it('lies as a pill and opens into the field on a press, with the focus in it', () => {
    render(fold());
    expect(box().dataset.open).toBe('false');
    fireEvent.click(screen.getByRole('button', { name: 'Что поменять?' }));
    expect(box().dataset.open).toBe('true');
    expect(document.activeElement).toBe(screen.getByLabelText('поле'));
  });

  it('folds on Escape and on a press outside while empty', () => {
    render(fold());
    fireEvent.click(screen.getByRole('button', { name: 'Что поменять?' }));
    fireEvent.keyDown(screen.getByLabelText('поле'), { key: 'Escape' });
    expect(box().dataset.open).toBe('false');

    fireEvent.click(screen.getByRole('button', { name: 'Что поменять?' }));
    fireEvent.pointerDown(document.body);
    expect(box().dataset.open).toBe('false');
  });

  it('stays open on a press outside while there are words, and opens when words come', () => {
    const { rerender } = render(fold());
    rerender(fold({ empty: false }));
    expect(box().dataset.open).toBe('true');
    fireEvent.pointerDown(document.body);
    expect(box().dataset.open).toBe('true');
  });

  it('shows what the agent does and stops it while it works', () => {
    const onStop = vi.fn();
    render(fold({ working: true, line: 'Смотрю настройки', onStop, onMic: vi.fn() }));
    expect(screen.getByText('Смотрю настройки')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /голос|микрофон/i })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Остановить агента' }));
    expect(onStop).toHaveBeenCalled();
    expect(box().dataset.open).toBe('false');
  });
});
