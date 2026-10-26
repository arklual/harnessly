// The «Модели» page on a machine: what the runtime has of the vendors' keys, and the three things a person does with
// one (save, delete, check). The list changes only from the runtime's answers, so a failure leaves it where it was.
import { useCallback, useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import {
  deleteProviderKey,
  loadProviders,
  putProviderKey,
  testProvider,
} from '@/next/features/providers/api';
import type { IPresetState, PresetId, TestState } from '@/next/features/providers/model';
import { t } from '@/next/i18n';
import type { MachineId, ProfileKey } from '@/next/protocol/v2';

type TState = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; presets: IPresetState[] };

/** An older runtime has no such address: said as that, not as a failure to load. */
const isMissing = (res: Exclude<Awaited<ReturnType<typeof loadProviders>>, { ok: true }>): boolean =>
  res.r === 'refused' ? res.code === 'not-found' : res.code === 'http' && res.status === 404;

/** The presets of a machine and the actions on them; the first-run step uses the same hook. */
export const useProviders = (machine: MachineId, enabled = true) => {
  const [state, setState] = useState<TState>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!enabled) return;
    let live = true;
    void loadProviders(machine).then((res) => {
      if (!live) return;
      if ('ok' in res) setState({ k: 'ready', presets: res.value.profiles });
      else {
        const text = isMissing(res) ? t('next.providers.unsupported') : pageText(res);
        setState((now) => (now.k === 'ready' ? now : { k: 'failed', text }));
      }
    });
    return () => {
      live = false;
    };
  }, [enabled, machine, attempt]);

  const change = useCallback((preset: PresetId, key: ProfileKey) => {
    setState((now) =>
      now.k !== 'ready'
        ? now
        : { k: 'ready', presets: now.presets.map((p) => (p.id === preset ? { ...p, key } : p)) },
    );
  }, []);

  const onSave = useCallback(
    async (preset: PresetId, key: string): Promise<string | null> => {
      const res = await putProviderKey(machine, preset, key);
      if (!('ok' in res)) return t('next.providers.saveFailed');
      change(preset, res.value);
      return null;
    },
    [machine, change],
  );
  const onRemove = useCallback(
    async (preset: PresetId): Promise<string | null> => {
      const res = await deleteProviderKey(machine, preset);
      if (!('ok' in res)) return t('next.providers.removeFailed');
      change(preset, res.value);
      return null;
    },
    [machine, change],
  );
  const onTest = useCallback(
    async (preset: PresetId): Promise<TestState> => {
      const res = await testProvider(machine, preset);
      if (!('ok' in res)) return { k: 'failed', reason: 'page' };
      const { ok, reason } = res.value;
      if (ok) return { k: 'ok' };
      return { k: 'failed', reason: reason ?? 'rejected' };
    },
    [machine],
  );
  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  return { state, onSave, onRemove, onTest, retry };
};
