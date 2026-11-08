// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ICodeAsk } from '@/next/features/safe/api';
import CodeAsks, { CODE_SENT, type TCodeSent } from '@/next/features/safe/code-asks';
import CodeField from '@/next/ui/code-field';

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const ask: ICodeAsk = {
  id: 'sc_1',
  why: 'npm publish vizloom',
  conv: 'c-1',
  title: 'Release',
  digits: 6,
  at: 0,
  until: 8 * 60_000,
};

describe('CodeField', () => {
  it('takes a pasted code whole, keeps only digits and hands it over on the last one', () => {
    const onComplete = vi.fn();
    const Host = () => {
      const [value, setValue] = useState('');
      return <CodeField value={value} onChange={setValue} onComplete={onComplete} length={6} label="Код" />;
    };
    const { getByLabelText, container } = render(<Host />);
    const input = getByLabelText('Код') as HTMLInputElement;
    expect(input.getAttribute('autocomplete')).toBe('one-time-code');
    expect(input.getAttribute('inputmode')).toBe('numeric');
    fireEvent.change(input, { target: { value: '40' } });
    expect(onComplete).not.toHaveBeenCalled();
    expect(container.querySelectorAll('.hx-code__cell[data-filled]').length).toBe(2);
    fireEvent.change(input, { target: { value: '407 193' } });
    expect(onComplete).toHaveBeenCalledExactlyOnceWith('407193');
    expect(input.value).toBe('407193');
  });
});

describe('CodeAsks', () => {
  it('says what the code is for and how long it waits, sends it on the last digit and folds away empty', async () => {
    vi.useFakeTimers();
    let sent!: (r: TCodeSent) => void;
    const onPut = vi.fn(() => new Promise<TCodeSent>((ok) => (sent = ok)));
    const onDone = vi.fn();
    const { getByText, getByLabelText } = render(
      <CodeAsks asks={[ask]} now={0} skew={0} onPut={onPut} onCancel={async () => {}} onDone={onDone} />,
    );
    expect(getByText('npm publish vizloom')).toBeTruthy();
    expect(getByText(/8/)).toBeTruthy();
    const input = getByLabelText(/npm publish vizloom/) as HTMLInputElement;
    fireEvent.change(input, { target: { value: '407193' } });
    expect(onPut).toHaveBeenCalledExactlyOnceWith(ask, '407193');
    await act(async () => sent(CODE_SENT.ok));
    await act(async () => vi.advanceTimersByTime(600));
    expect(onDone).toHaveBeenCalledWith(ask);
    expect(input.value).toBe('');
  });

  it('cancels without sending a code', async () => {
    const onPut = vi.fn();
    const onCancel = vi.fn(async () => {});
    const onDone = vi.fn();
    const { getByRole } = render(
      <CodeAsks asks={[ask]} now={0} skew={0} onPut={onPut} onCancel={onCancel} onDone={onDone} />,
    );
    await act(async () => fireEvent.click(getByRole('button')));
    expect(onCancel).toHaveBeenCalledWith(ask);
    expect(onPut).not.toHaveBeenCalled();
    expect(onDone).toHaveBeenCalledWith(ask);
  });

  it('draws nothing while no command waits', () => {
    const { container } = render(
      <CodeAsks asks={[]} now={0} skew={0} onPut={vi.fn()} onCancel={vi.fn()} onDone={vi.fn()} />,
    );
    expect(container.innerHTML).toBe('');
  });
});
