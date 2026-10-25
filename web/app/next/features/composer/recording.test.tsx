// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import Composer, { type IComposerProps } from '@/next/features/composer/composer';
import { DictationMirror } from '@/next/features/composer/recording';

beforeAll(() => pinLocale('en'));
beforeEach(() => {
  // jsdom has no canvas: the wave stays blank, which is all these tests need of it.
  HTMLCanvasElement.prototype.getContext = (() =>
    null) as unknown as typeof HTMLCanvasElement.prototype.getContext;
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const fresh = (container: HTMLElement) =>
  [...container.querySelectorAll('.hx-voice-word[data-fresh="true"]')].map((node) => node.textContent);

describe('DictationMirror', () => {
  it('draws the field’s text as it is, the words that were there when it began standing still', () => {
    const { container } = render(<DictationMirror text={'Look at this\nand that'} ref={null} />);
    expect(container.querySelector('.hx-voice-mirror')?.textContent).toBe('Look at this\nand that​');
    expect(fresh(container)).toEqual([]);
  });

  it('fades in the words recognized after it began, and again a word the recognizer takes back', () => {
    const { container, rerender } = render(<DictationMirror text="Look at this" ref={null} />);
    rerender(<DictationMirror text="Look at this and" ref={null} />);
    expect(fresh(container)).toEqual(['and']);
    rerender(<DictationMirror text="Look at this and that" ref={null} />);
    expect(fresh(container)).toEqual(['and', 'that']);
    const that = [...container.querySelectorAll('.hx-voice-word')].find(
      (node) => node.textContent === 'that',
    );
    rerender(<DictationMirror text="Look at this and than" ref={null} />);
    expect(fresh(container)).toEqual(['and', 'than']);
    // the word that stayed is the same node: it does not start its animation over
    const and = [...container.querySelectorAll('.hx-voice-word')].find((node) => node.textContent === 'and');
    rerender(<DictationMirror text="Look at this and than more" ref={null} />);
    expect([...container.querySelectorAll('.hx-voice-word')].find((node) => node.textContent === 'and')).toBe(
      and,
    );
    expect(that?.isConnected).toBe(false);
  });

  it('keeps a long text light: only the last words are apart', () => {
    const text = Array.from({ length: 300 }, (_, i) => `w${i}`).join(' ');
    const { container } = render(<DictationMirror text={text} ref={null} />);
    expect(container.querySelectorAll('.hx-voice-word').length).toBeLessThanOrEqual(48);
    expect(container.querySelector('.hx-voice-mirror')?.textContent).toBe(`${text}​`);
  });
});

const props = (over: Partial<IComposerProps> = {}): IComposerProps => ({
  value: 'Look at this',
  onChange: vi.fn(),
  onSubmit: vi.fn(),
  mode: 'idle',
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

const REC = { startedAt: Date.now(), busy: false, level: null };

describe('Composer that listens', () => {
  it('shows the layer over the field, asks to speak in the field and stands in the recording state', () => {
    const { container } = render(<Composer {...props({ recording: REC, value: '' })} />);
    expect(container.querySelector('.hx-voice-mirror')).not.toBeNull();
    expect((screen.getByRole('textbox') as HTMLTextAreaElement).placeholder).toBe(
      'Speak — your words will appear here',
    );
    expect(container.querySelector('.hx-composer')?.getAttribute('data-rec')).toBe('true');
  });

  it('lets the words in the field fade in as they come', () => {
    const { container, rerender } = render(<Composer {...props({ recording: REC })} />);
    rerender(<Composer {...props({ recording: REC, value: 'Look at this and that' })} />);
    expect(fresh(container)).toEqual(['and', 'that']);
  });

  it('keeps the layer and the recording state through the exit, then lets both go and brings the controls back softly', () => {
    vi.useFakeTimers();
    const { container, rerender } = render(<Composer {...props({ recording: REC })} />);
    rerender(<Composer {...props({ recording: null })} />);
    expect(container.querySelector('.hx-voice-mirror')).not.toBeNull();
    expect(container.querySelector('.hx-recbar')?.getAttribute('data-state')).toBe('closing');
    expect(container.querySelector('.hx-composer')?.getAttribute('data-rec')).toBe('true');
    act(() => void vi.advanceTimersByTime(260));
    expect(container.querySelector('.hx-voice-mirror')).toBeNull();
    expect(container.querySelector('.hx-composer')?.getAttribute('data-rec')).toBeNull();
    expect(container.querySelector('.hx-composer__bar[data-back="true"]')).not.toBeNull();
  });

  it('does not make the controls enter on the first draw', () => {
    const { container } = render(<Composer {...props()} />);
    expect(container.querySelector('.hx-composer__bar')?.getAttribute('data-back')).toBeNull();
  });
});
