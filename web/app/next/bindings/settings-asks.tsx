// Settings → Notifications → «Questions to people» of the machine on screen: read when the page opens, every change
// sent whole and shown only once the machine took it (a refused one leaves the settings as they were).
import { useCallback, useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import {
  isAskRefusal,
  loadAskSettings,
  saveAskSettings,
  type IAskSettingsView,
  type TAskOutcome,
} from '@/next/features/asks/api';
import { askReasonText } from '@/next/features/asks/ask-step';
import AsksSection from '@/next/features/settings/asks-section';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

type TLoad = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; view: IAskSettingsView };

const outcome = (res: TAskOutcome<IAskSettingsView>): { value: IAskSettingsView } | { error: string } => {
  if (!('ok' in res)) return { error: pageText(res) };
  if (isAskRefusal(res.value)) {
    if (res.value.refused === 'bad-settings') return { error: t('next.asks.reason.bad-settings') };
    return { error: askReasonText(res.value.refused) };
  }
  return { value: res.value };
};

export const AsksSettingsPage = ({ machine }: { machine: MachineId }) => {
  const [state, setState] = useState<TLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [saving, setSaving] = useState('');

  useEffect(() => {
    let live = true;
    void loadAskSettings(machine).then((res) => {
      if (!live) return;
      const got = outcome(res);
      setState('value' in got ? { k: 'ready', view: got.value } : { k: 'failed', text: got.error });
    });
    return () => {
      live = false;
    };
  }, [machine, attempt]);

  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  return (
    <AsksSection
      settings={state.k === 'ready' ? state.view.settings : null}
      bots={state.k === 'ready' ? state.view.bots : []}
      failed={state.k === 'failed' ? { reason: state.text, onRetry: retry } : null}
      saving={saving}
      onSave={async (next, field) => {
        setSaving(field);
        const got = outcome(await saveAskSettings(machine, next));
        setSaving('');
        if ('error' in got) return got.error;
        setState({ k: 'ready', view: got.value });
        return null;
      }}
    />
  );
};
