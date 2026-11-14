// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ISettingChangesHandle } from '@/next/bindings/settings-changes';
import type { TUIEvent } from '@/next/features/settings/ui-agent-api';
import { pinLocale } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

const asked: { ask: { query: string; map: string }; onEvent: (event: TUIEvent) => void }[] = [];
const goTo = vi.fn();
const putSetting = vi.fn();

vi.mock('@/next/features/settings/ui-agent-api', async (real) => ({
  ...(await real<typeof import('@/next/features/settings/ui-agent-api')>()),
  askUIAgent: (
    _machine: MachineId,
    ask: { query: string; map: string },
    _signal: AbortSignal,
    onEvent: (e: TUIEvent) => void,
  ) => {
    asked.push({ ask, onEvent });
    return Promise.resolve();
  },
  settingPlaces: () => [],
}));
vi.mock('@/next/features/settings/changes-api', () => ({
  putSetting: (...a: unknown[]) => putSetting(...a),
}));
vi.mock('@/next/features/settings/smart-api', () => ({
  loadSmart: () =>
    Promise.resolve({
      ok: true,
      value: {
        features: [{ id: 'ui-agent', choice: { provider: 'kimi', model: 'k3' } }],
        providers: [{ id: 'kimi', name: 'Kimi', models: [{ id: 'k3', label: 'K3' }] }],
      },
    }),
}));
vi.mock('@/next/routes/routes', async (real) => ({
  ...(await real<typeof import('@/next/routes/routes')>()),
  goTo: (...a: unknown[]) => goTo(...a),
}));
vi.mock('@/next/bindings/use-voice', () => ({
  useVoice: () => ({
    notice: null,
    recording: null,
    recognized: 0,
    caret: null,
    onMic: undefined,
    onCancelRecording: () => undefined,
    onFinishRecording: () => undefined,
  }),
}));
vi.mock('@/next/stores/machines', async (real) => ({
  ...(await real<typeof import('@/next/stores/machines')>()),
  limitsOf: () => ({ maxText: 1000 }),
}));

const { default: SettingsAgent } = await import('@/next/bindings/settings-agent');

beforeAll(() => pinLocale('ru'));
beforeEach(() => {
  asked.length = 0;
  goTo.mockReset();
  putSetting.mockReset();
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

const handle = (refresh = vi.fn()): ISettingChangesHandle => ({
  journal: { k: 'ready', changes: [] },
  marks: {},
  stampOf: () => '',
  refresh,
});

const ask = (words: string) => {
  const field = screen.getByPlaceholderText('Что поменять в настройках?');
  fireEvent.change(field, { target: { value: words } });
  fireEvent.keyDown(field, { key: 'Enter' });
  return asked.at(-1)!;
};

describe('the agent of the Settings window', () => {
  it('names the assistant’s model on the field and leads to Smart features', async () => {
    render(<SettingsAgent machine={'m1' as MachineId} changes={handle()} />);
    // Folded, the field names the model on its pill; open, on its bar.
    expect(await screen.findByText('K3', { selector: '.hx-fold__model' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Что поменять в настройках?' }));
    const pill = await screen.findByRole('button', { name: 'K3' });
    fireEvent.click(pill);
    expect(goTo).toHaveBeenCalledWith(expect.objectContaining({ section: 'smart' }));
  });

  it('asks the window’s agent with the map, goes where it says and shows its words as they come', () => {
    render(<SettingsAgent machine={'m1' as MachineId} changes={handle()} />);
    const call = ask('открой плагины');
    expect(call.ask.query).toBe('открой плагины');
    expect(call.ask.map).toContain('/settings/');

    act(() => call.onEvent({ type: 'go', place: '/settings/plugins', label: 'Плагины' }));
    expect(goTo).toHaveBeenCalledWith(expect.objectContaining({ section: 'plugins' }));

    act(() => call.onEvent({ type: 'delta', text: 'Открываю ' }));
    act(() => call.onEvent({ type: 'delta', text: 'плагины.' }));
    expect(screen.getByText('Открываю плагины.')).toBeTruthy();
    act(() => call.onEvent({ type: 'done', text: 'Открываю плагины.' }));
    expect(screen.getByRole('button', { name: 'Хорошо' })).toBeTruthy();
  });

  it('changes a setting only when the person says «Поменять», as their own change', async () => {
    putSetting.mockResolvedValue({ ok: true, value: { key: 'runtime.idleStop', old: '2h' } });
    const refresh = vi.fn();
    render(<SettingsAgent machine={'m1' as MachineId} changes={handle(refresh)} />);
    const call = ask('не останавливай чаты');
    act(() =>
      call.onEvent({
        type: 'confirm',
        key: 'runtime.idleStop',
        value: 'never',
        label: 'никогда',
        title: 'Остановка простаивающих чатов',
        was: 'через 2 часа',
      }),
    );
    expect(screen.queryByRole('button', { name: 'Поменять' })).toBeNull();
    act(() => call.onEvent({ type: 'done', text: 'Поменять?' }));
    expect(putSetting).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Поменять' }));
      await Promise.resolve();
    });
    expect(putSetting).toHaveBeenCalledWith('m1', 'runtime.idleStop', 'never');
    expect(refresh).toHaveBeenCalled();

    // The way back: the value before the change, as the journal wrote it.
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Вернуть' }));
      await Promise.resolve();
    });
    expect(putSetting).toHaveBeenLastCalledWith('m1', 'runtime.idleStop', '2h');
    expect(screen.getByText('Вернул как было.')).toBeTruthy();
  });

  it('offers to choose a model when the machine has none for it', () => {
    render(<SettingsAgent machine={'m1' as MachineId} changes={handle()} />);
    const call = ask('где язык?');
    act(() => call.onEvent({ type: 'error', code: 'no-model', message: '' }));
    fireEvent.click(screen.getByRole('button', { name: 'Выбрать модель' }));
    expect(goTo).toHaveBeenCalledWith(expect.objectContaining({ section: 'smart' }));
  });
});
