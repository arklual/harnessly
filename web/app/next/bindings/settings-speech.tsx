// The keys of the cloud recognition models on a machine: what it has of them and the two things a person does with one
// (save, delete). The list changes only from the machine's answers, so a failure leaves it where it was.
import { useCallback, useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import {
  deleteSpeechKey,
  loadSpeechKeys,
  putSpeechKey,
  type SpeechKeyId,
  type SpeechKeys,
} from '@/next/features/providers/speech-keys';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

export type SpeechState = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; keys: SpeechKeys };

/** An older runtime has no such address: said as that, not as a failure to load. */
const isMissing = (res: Exclude<Awaited<ReturnType<typeof loadSpeechKeys>>, { ok: true }>): boolean =>
  res.r === 'refused' ? res.code === 'not-found' : res.code === 'http' && res.status === 404;

export const useSpeechKeys = (machine: MachineId) => {
  const [state, setState] = useState<SpeechState>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    void loadSpeechKeys(machine).then((res) => {
      if (!live) return;
      if ('ok' in res) setState({ k: 'ready', keys: res.value });
      else {
        const text = isMissing(res) ? t('next.speech.unsupported') : pageText(res);
        setState((now) => (now.k === 'ready' ? now : { k: 'failed', text }));
      }
    });
    return () => {
      live = false;
    };
  }, [machine, attempt]);

  const onSave = useCallback(
    async (id: SpeechKeyId, key: string): Promise<string | null> => {
      const res = await putSpeechKey(machine, id, key);
      if (!('ok' in res)) return t('next.providers.saveFailed');
      setState({ k: 'ready', keys: res.value });
      return null;
    },
    [machine],
  );
  const onRemove = useCallback(
    async (id: SpeechKeyId): Promise<string | null> => {
      const res = await deleteSpeechKey(machine, id);
      if (!('ok' in res)) return t('next.providers.removeFailed');
      setState({ k: 'ready', keys: res.value });
      return null;
    },
    [machine],
  );
  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);
  return { state, onSave, onRemove, retry };
};
