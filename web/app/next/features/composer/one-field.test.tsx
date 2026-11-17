// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IComposerProps } from '@/next/features/composer/composer';
import ScreenField, { MainFieldArea, OneFieldRoot } from '@/next/features/composer/one-field';

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const base = (over: Partial<IComposerProps>): IComposerProps => ({
  value: '',
  onChange: vi.fn(),
  onSubmit: vi.fn(),
  mode: 'new',
  onStop: () => Promise.resolve(),
  picker: null,
  attachments: [],
  onFiles: vi.fn(),
  onRemoveAttachment: vi.fn(),
  onRetryAttachment: vi.fn(),
  recording: null,
  onCancelRecording: vi.fn(),
  onFinishRecording: vi.fn(),
  maxText: 1000,
  ...over,
});

/** A screen with a field of its own words, like a chat or Settings. */
const Screen = ({ name, onSubmit }: { name: string; onSubmit?: () => void }) => {
  const [text, setText] = useState('');
  return (
    <section data-testid={name} className="hx-dock">
      <ScreenField {...base({ value: text, onChange: setText, placeholder: name, onSubmit })} />
    </section>
  );
};

const Window = ({ at }: { at: 'chat' | 'settings' }) => (
  <>
    <MainFieldArea>
      {at === 'chat' ? <Screen key="chat" name="chat" /> : <Screen key="settings" name="settings" />}
    </MainFieldArea>
    <aside>
      <Screen name="pane" />
    </aside>
    <OneFieldRoot />
  </>
);

const fields = () => document.querySelectorAll('textarea');
const mainField = () => screen.getByPlaceholderText(/chat|settings/);

describe('the window’s one field', () => {
  it('is one element that moves from screen to screen and takes each screen’s words', async () => {
    const { rerender } = render(<Window at="chat" />);
    const field = mainField();
    expect(screen.getByTestId('chat').contains(field)).toBe(true);
    fireEvent.change(field, { target: { value: 'to the chat' } });
    expect(mainField()).toHaveProperty('value', 'to the chat');

    field.focus();
    rerender(<Window at="settings" />);
    expect(mainField()).toBe(field);
    expect(screen.getByTestId('settings').contains(field)).toBe(true);
    // The old words fade out before the screen's own fade in (composer.tsx, useHintSwap).
    await waitFor(() => expect(field).toHaveProperty('placeholder', 'settings'));
    expect(field).toHaveProperty('value', '');
    expect(document.activeElement).toBe(field);

    fireEvent.change(field, { target: { value: 'to settings' } });
    expect(field).toHaveProperty('value', 'to settings');
  });

  it('leaves the pane its own field', () => {
    render(<Window at="chat" />);
    expect(fields()).toHaveLength(2);
    const pane = screen.getByPlaceholderText('pane');
    expect(screen.getByTestId('pane').contains(pane)).toBe(true);
    expect(pane).not.toBe(mainField());
  });

  it('shows the words the screen keeps, when it keeps them still', async () => {
    const Still = () => (
      <MainFieldArea>
        <ScreenField {...base({ value: 'kept', placeholder: 'chat' })} />
      </MainFieldArea>
    );
    render(
      <>
        <Still />
        <OneFieldRoot />
      </>,
    );
    fireEvent.change(mainField(), { target: { value: 'kept!' } });
    await act(() => Promise.resolve());
    expect(mainField()).toHaveProperty('value', 'kept');
  });

  it('draws a key with the new words only, never the old ones in between', () => {
    render(<Window at="chat" />);
    const field = mainField() as HTMLTextAreaElement;
    const shown: string[] = [];
    const own = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value');
    Object.defineProperty(field, 'value', {
      configurable: true,
      get: () => own?.get?.call(field) as string,
      set: (text: string) => {
        shown.push(text);
        own?.set?.call(field, text);
      },
    });
    fireEvent.change(field, { target: { value: 'a' } });
    expect(shown.filter((text) => text === '')).toEqual([]);
    expect(field.value).toBe('a');
  });
});

describe('the field glides only from a start page into the screen its sending opens', () => {
  const Glide = ({ name, glideOut, glideIn }: { name: string; glideOut?: string; glideIn?: string }) => (
    <section data-testid={name}>
      <ScreenField {...base({ placeholder: name })} glideOut={glideOut} glideIn={glideIn} />
    </section>
  );
  const At = ({ at }: { at: 'home' | 'sinopia' | 'settings' | 'chat' }) => (
    <>
      <MainFieldArea>
        {at === 'home' && <Glide key="home" name="home" glideOut="chat" />}
        {at === 'sinopia' && <Glide key="sinopia" name="sinopia" glideOut="site" />}
        {at === 'settings' && <Glide key="settings" name="settings" />}
        {at === 'chat' && <Glide key="chat" name="chat" glideIn="chat" />}
      </MainFieldArea>
      <OneFieldRoot />
    </>
  );

  let animate: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    animate = vi.fn();
    // jsdom lays nothing out: the field stands high on the start pages and low in the chat.
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      const top = this.closest('[data-testid="chat"]') ? 800 : 300;
      return {
        left: 100,
        top,
        width: 600,
        height: 120,
        right: 700,
        bottom: top + 120,
        x: 100,
        y: top,
      } as DOMRect;
    });
    HTMLElement.prototype.animate = animate as unknown as HTMLElement['animate'];
  });
  afterEach(() => vi.restoreAllMocks());

  it('glides from the new chat’s page into the chat it opens (HRN-1362)', () => {
    const { rerender } = render(<At at="home" />);
    animate.mockClear();
    rerender(<At at="chat" />);
    expect(animate).toHaveBeenCalledTimes(1);
  });

  it.each(['sinopia', 'settings'] as const)(
    'stands in its place at once when the chat comes from %s',
    (from) => {
      const { rerender } = render(<At at={from} />);
      animate.mockClear();
      rerender(<At at="chat" />);
      expect(animate).not.toHaveBeenCalled();
    },
  );
});
