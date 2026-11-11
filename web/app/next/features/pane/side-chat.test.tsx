// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useSideChat } from '@/next/bindings/side-chat';
import { setScheduler } from '@/next/core/store';
import { quoteBlock, SideChatBody } from '@/next/features/pane/side-chat';
import { pinLocale } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { askBeside, useSideAsk } from '@/next/stores/side-ask';
import AnnotatePill from '@/next/ui/notes/pill';

const CONV: ConvId = 'tab-main';
const OTHER: ConvId = 'tab-other';

/** The side chat of a chat as the pane draws it: the binding and the log with its field. */
const Side = ({ conv }: { conv: ConvId }) => {
  const side = useSideChat('m1' as MachineId, conv);
  return (
    <SideChatBody
      messages={side.messages}
      busy={side.busy}
      onAsk={side.ask}
      onStop={side.stop}
      onRetry={side.retry}
      quote={side.quote}
      onQuoted={side.takeQuote}
    />
  );
};

/** What waits in the store for a chat, seen from outside. */
const Watch = ({ conv }: { conv: ConvId }) => {
  const ask = useSideAsk(conv);
  return <output className="waiting">{ask?.quote ?? ''}</output>;
};
const waiting = () => document.querySelector('.waiting')?.textContent;

const field = () => document.querySelector<HTMLTextAreaElement>('.hx-sidechat__input') as HTMLTextAreaElement;
const frame = () => act(() => void vi.advanceTimersToNextFrame());

beforeEach(() => {
  // The store tells its readers at once (the page waits for a frame).
  setScheduler('sync');
  pinLocale('en');
  vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'setTimeout', 'clearTimeout'] });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  setScheduler('frame');
});

describe('Side chat: words asked about aside', () => {
  it('puts the pressed words into the field as a quote of the main chat, the caret after them', () => {
    render(
      <>
        <Side conv={CONV} />
        <Watch conv={CONV} />
      </>,
    );
    act(() => askBeside(CONV, 'the build fails on arm64'));
    expect(field().value).toBe('From the main chat:\n> the build fails on arm64\n\n');
    expect(field().value).toBe(quoteBlock('the build fails on arm64'));
    // Taken: the store holds nothing more for this chat.
    expect(waiting()).toBe('');
    frame();
    expect(document.activeElement).toBe(field());
    expect(field().selectionStart).toBe(field().value.length);
  });

  it('takes the words of a press of «Ask aside» on the pill over them', () => {
    render(
      <>
        <AnnotatePill
          target={{ x: 300, top: 400, bottom: 420 }}
          onPress={() => undefined}
          onAside={() => askBeside(CONV, 'picked words')}
          onDismiss={() => undefined}
        />
        <Side conv={CONV} />
      </>,
    );
    fireEvent.click(document.querySelectorAll('.hx-note__pillbtn')[1]);
    expect(field().value).toBe('From the main chat:\n> picked words\n\n');
  });

  it('adds the quote after what is typed in an open side chat', () => {
    render(<Side conv={CONV} />);
    act(() => askBeside(CONV, 'first words'));
    act(() => askBeside(CONV, 'second words'));
    expect(field().value).toBe(
      'From the main chat:\n> first words\n\nFrom the main chat:\n> second words\n\n',
    );
  });

  it('leaves the words of another chat to that chat', () => {
    render(
      <>
        <Side conv={CONV} />
        <Watch conv={OTHER} />
      </>,
    );
    act(() => askBeside(OTHER, 'not mine'));
    expect(field().value).toBe('');
    expect(waiting()).toBe('not mine');
    // That chat's side chat takes them when it comes.
    cleanup();
    render(<Side conv={OTHER} />);
    expect(field().value).toBe('From the main chat:\n> not mine\n\n');
  });
});
