// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { IPresetState, TestState } from '@/next/features/providers/model';
import PresetPanel from '@/next/features/providers/preset-panel';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const OFF: IPresetState = {
  id: 'kimi',
  label: 'Kimi · platform',
  vendor: 'kimi',
  via: 'claude',
  host: 'api.moonshot.ai',
  hint: 'A key from platform.kimi.ai',
  key: { configured: false, tail: null },
  models: [],
};
const ON: IPresetState = { ...OFF, key: { configured: true, tail: '…3456' } };

const setup = (state: IPresetState, over: Partial<Parameters<typeof PresetPanel>[0]> = {}) => {
  const onSave = vi.fn(() => Promise.resolve(null as string | null));
  const onRemove = vi.fn(() => Promise.resolve(null as string | null));
  const onTest = vi.fn(() => Promise.resolve({ k: 'ok' } as TestState));
  const view = render(
    <PresetPanel state={state} vendor="Kimi" onSave={onSave} onRemove={onRemove} onTest={onTest} {...over} />,
  );
  return { view, onSave, onRemove, onTest };
};

const field = () => screen.getByLabelText<HTMLInputElement>('Kimi API key');

describe('the key form', () => {
  it('masks the key, shows it on the eye and hides it again', () => {
    setup(OFF);
    fireEvent.change(field(), { target: { value: 'sk-test-1234567890' } });
    expect(field().type).toBe('password');
    fireEvent.click(screen.getByRole('button', { name: 'Show key' }));
    expect(field().type).toBe('text');
    fireEvent.click(screen.getByRole('button', { name: 'Hide key' }));
    expect(field().type).toBe('password');
  });

  it('sends the trimmed key and clears the field once the machine has it', async () => {
    const onSaved = vi.fn();
    const { onSave } = setup(OFF, { onSaved });
    fireEvent.change(field(), { target: { value: '  sk-test-1234567890 ' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
    });
    expect(onSave).toHaveBeenCalledWith('kimi', 'sk-test-1234567890');
    expect(field().value).toBe('');
    expect(onSaved).toHaveBeenCalled();
  });

  it('does not send a key that cannot be one, and says so', async () => {
    const { onSave } = setup(OFF);
    fireEvent.change(field(), { target: { value: 'short' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
    });
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByRole('alert').textContent).toContain('too short');
  });

  it('keeps the typed key and says why when the machine refuses it', async () => {
    const { onSave } = setup(OFF);
    onSave.mockResolvedValueOnce('Could not save the key');
    fireEvent.change(field(), { target: { value: 'sk-test-1234567890' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
    });
    expect(screen.getByRole('alert').textContent).toContain('Could not save the key');
    expect(field().value).toBe('sk-test-1234567890');
  });
});

describe('a saved key', () => {
  it('shows the runtime’s one line about the preset', () => {
    setup(OFF);
    expect(screen.getByText('A key from platform.kimi.ai')).toBeTruthy();
  });

  it('shows only its last digits and never a field holding it', () => {
    setup(ON);
    expect(screen.getByText('Key saved · …3456')).toBeTruthy();
    expect(field().value).toBe('');
  });

  it('says plainly what the check found', async () => {
    const { onTest } = setup(ON);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Check connection' }));
    });
    expect(screen.getByRole('status').textContent).toBe('The connection works');
    onTest.mockResolvedValueOnce({ k: 'failed', reason: 'unauthorized' });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Check connection' }));
    });
    expect(screen.getByRole('status').textContent).toContain('did not accept the key');
    onTest.mockResolvedValueOnce({ k: 'failed', reason: 'rate-limited' });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Check connection' }));
    });
    expect(screen.getByRole('status').textContent).toContain('limited the requests');
  });

  it('deletes only on the second press', async () => {
    const { onRemove } = setup(ON);
    fireEvent.click(screen.getByRole('button', { name: 'Delete key' }));
    expect(onRemove).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Delete for sure?' }));
    });
    expect(onRemove).toHaveBeenCalledWith('kimi');
  });

  it('opens the form to replace the key and closes it on cancel', () => {
    setup(ON);
    fireEvent.click(screen.getByRole('button', { name: 'Replace key' }));
    fireEvent.change(field(), { target: { value: 'sk-test-abcdefgh' } });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(field().value).toBe('');
  });
});
