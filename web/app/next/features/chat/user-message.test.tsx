// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Message } from '@/next/features/chat/types';
import UserMessage from '@/next/features/chat/user-message';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const message = (state: Message['state']): Message => ({
  clientId: 'c1',
  text: 'Hello',
  attachments: [],
  mode: 'queue',
  createdAt: Date.now(),
  stateSince: Date.now(),
  state,
});

const look = () => document.querySelector('.hx-msg-user')?.getAttribute('data-send');

describe('UserMessage of a steer', () => {
  const steer = (state: Message['state'], handed = false): Message => ({
    ...message(state),
    mode: 'steer',
    handed,
  });
  const show = (outgoing: Message) =>
    render(
      <UserMessage
        text="Hello"
        attachments={[]}
        outgoing={outgoing}
        offline={false}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
      />,
    );

  it('says it is going out while the runtime types it', () => {
    show(steer({ k: 'typing' }));
    expect(screen.getByText('Sending straight into the turn')).toBeTruthy();
    expect(screen.queryByText("In the agent's queue — read after the current step")).toBeNull();
  });

  it('says the agent reads it after the current step, and offers no retry however long that takes', () => {
    show(steer({ k: 'in-input' }, true));
    expect(screen.getByText("In the agent's queue — read after the current step")).toBeTruthy();
    expect(look()).toBe('waiting');
    act(() => void vi.advanceTimersByTime(10 * 60_000));
    expect(screen.getByText("In the agent's queue — read after the current step")).toBeTruthy();
    expect(screen.queryByText('Retry')).toBeNull();
    expect(look()).toBe('waiting');
  });

  it('says it was read once the transcript has it', () => {
    show(steer({ k: 'accepted', entryId: 'c:1#0' }));
    expect(screen.getByText('Read by the agent mid-turn')).toBeTruthy();
    expect(screen.queryByText("In the agent's queue — read after the current step")).toBeNull();
    expect(look()).toBe('sent');
  });
});

describe('UserMessage', () => {
  it('goes pale at once, says «longer than usual» at 3 s and never turns red by its own clock', () => {
    render(
      <UserMessage
        text="Hello"
        attachments={[]}
        outgoing={message({ k: 'submitting' })}
        offline={false}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
      />,
    );
    expect(look()).toBe('sending');
    act(() => void vi.advanceTimersByTime(3000));
    expect(screen.getByText('Takes longer than usual…')).toBeTruthy();
    act(() => void vi.advanceTimersByTime(10 * 60_000));
    expect(look()).toBe('sending');
    expect(document.querySelector('[data-tone="fail"]')).toBeNull();
    expect(screen.queryByText('Retry')).toBeNull();
  });

  it('waits for the machine quietly when it did not answer: no red, no retry, only Delete', () => {
    const onDelete = vi.fn(() => Promise.resolve());
    render(
      <UserMessage
        text="Hello"
        attachments={[]}
        outgoing={{ ...message({ k: 'submitting' }), waitsMachine: true }}
        offline={false}
        onRetry={vi.fn()}
        onDelete={onDelete}
      />,
    );
    act(() => void vi.advanceTimersByTime(60_000));
    expect(screen.getByText('Waiting for the machine, it will go by itself')).toBeTruthy();
    expect(look()).toBe('waiting');
    expect(document.querySelector('[data-tone="fail"]')).toBeNull();
    expect(screen.queryByText('Retry')).toBeNull();
    fireEvent.click(screen.getByText('Delete'));
    expect(onDelete).toHaveBeenCalledWith('c1');
  });

  it("says why the runtime refused a message on the merits, in the person's words, with Delete only", () => {
    render(
      <UserMessage
        text="Hello"
        attachments={[]}
        outgoing={{
          ...message({ k: 'failed', code: 'refused', retryable: false, at: Date.now() }),
          refusal: 'text-too-long',
        }}
        offline={false}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
      />,
    );
    expect(look()).toBe('failed');
    expect(screen.getByText('Not sent. The message is too long')).toBeTruthy();
    expect(screen.queryByText('Retry')).toBeNull();
    expect(screen.getByText('Delete')).toBeTruthy();
  });

  it('offers to send a refusal again when its cause may have passed, beside Delete', () => {
    const onResend = vi.fn(() => Promise.resolve());
    render(
      <UserMessage
        text="Hello"
        attachments={[]}
        outgoing={{
          ...message({ k: 'failed', code: 'refused', retryable: false, at: Date.now() }),
          refusal: 'agent-gone',
          resendable: true,
        }}
        offline={false}
        onRetry={vi.fn()}
        onResend={onResend}
        onDelete={vi.fn()}
      />,
    );
    expect(screen.getByText('Not sent. The agent is not running any more')).toBeTruthy();
    expect(screen.getByText('Delete')).toBeTruthy();
    fireEvent.click(screen.getByText('Send again'));
    expect(onResend).toHaveBeenCalledWith('c1');
  });

  it('waits for the network instead of failing while offline', () => {
    render(
      <UserMessage
        text="Hello"
        attachments={[]}
        outgoing={message({ k: 'submitting' })}
        offline
        onRetry={vi.fn()}
        onDelete={vi.fn()}
      />,
    );
    act(() => void vi.advanceTimersByTime(60_000));
    expect(screen.getByText('Goes out when the connection is back')).toBeTruthy();
  });

  it('turns the pressed action busy until the store answers', async () => {
    let resolve!: () => void;
    const onDelete = vi.fn(() => new Promise<void>((ok) => (resolve = ok)));
    render(
      <UserMessage
        text="Hello"
        attachments={[]}
        outgoing={{
          ...message({ k: 'failed', code: 'unconfirmed', retryable: true, at: Date.now() }),
        }}
        offline={false}
        onRetry={vi.fn()}
        onDelete={onDelete}
      />,
    );
    expect(screen.getByText('Not sent: the agent did not confirm it')).toBeTruthy();
    fireEvent.click(screen.getByText('Delete'));
    expect(onDelete).toHaveBeenCalledWith('c1');
    expect(screen.getByText('Delete').closest('button')?.getAttribute('data-busy')).toBe('true');
    expect(screen.getByText('Retry').closest('button')?.disabled).toBe(true);
    await act(async () => resolve());
    expect(screen.getByText('Delete').closest('button')?.getAttribute('data-busy')).toBeNull();
  });
});

describe('UserMessage with attached places', () => {
  it('shows the words without the block and the places as chips', () => {
    const text =
      'Look\n\n<attached>\nAttached to the message:\n<ref kind="design" id="d1" title="Checkout"/> Design …\n</attached>';
    render(
      <UserMessage
        text={text}
        attachments={[]}
        outgoing={null}
        offline={false}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
      />,
    );
    expect(document.body.textContent).not.toContain('<attached>');
    expect(document.body.textContent).not.toContain('Design …');
    expect(screen.getByText('Look')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Open design “Checkout”' })).toBeTruthy();
  });
});
