// The limit on the model pill on made-up data, for the eye and for screenshots: ?dev=limitring. The field
// with «+», the pill (its outline the spent five hours), the live voice, the mic and send; a tap on the pill opens the
// card of both windows. ?five=<left %>&week=<left %> set the windows, ?theme=light|dark&lang=en|ru the look; ?alerts
// tells the steps reached at the top (the memory is cleared first). Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/chat.css';
import '@/next/dev/smallfeats-gallery.css';

import { useEffect, useMemo, useRef, useState } from 'react';
import { PortalContext } from '@/next/ui/portal';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import Composer from '@/next/features/composer/composer';
import type { IModelChoice, IPickerAgent } from '@/next/features/composer/agents';
import type { LimitsView } from '@/next/features/limits/model';
import { useLimitAlerts } from '@/next/features/limits/alerts';
import { ToastHost } from '@/next/ui/toast';
import { RoundButton } from '@/next/ui/button';

const levels = ['low', 'medium', 'high', 'xhigh', 'max'] as const;
const AGENT: IPickerAgent = {
  provider: 'claude',
  name: 'Claude Code',
  short: 'Claude',
  canSwitch: true,
  available: true,
  status: 'ready',
  models: [
    { id: 'fable', label: 'Fable 5.2', hint: 'Самая сильная', efforts: [...levels], defaultEffort: 'high' },
    { id: 'opus', label: 'Opus 5.5', hint: 'Сильная', efforts: [...levels], defaultEffort: 'high' },
  ],
};

const num = (raw: string | string[] | undefined, fallback: number): number => {
  const one = Array.isArray(raw) ? raw[0] : raw;
  const n = Number(one);
  return one !== undefined && Number.isFinite(n) ? n : fallback;
};

const LimitringGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const portalRef = useRef<HTMLDivElement>(null);
  const [draft, setDraft] = useState('');
  const [choice, setChoice] = useState<IModelChoice>({
    provider: 'claude',
    model: 'fable',
    effort: 'high',
    ultracode: false,
  });
  const five = num(query.five, 64);
  const week = num(query.week, 51);
  const [now] = useState(() => Math.floor(Date.now() / 1000));
  const limits = useMemo<LimitsView>(
    () => ({
      state: 'ready',
      provider: 'claude',
      limits: {
        fiveHour: { usedPct: 100 - five, resetsAt: now + 2 * 3600 + 25 * 60, forecast: null },
        sevenDay: { usedPct: 100 - week, resetsAt: now + 3 * 86400 + 5 * 3600, forecast: null },
        month: null,
        balance: null,
        estimate: false,
        unofficial: false,
        updatedAt: now * 1000,
        plan: 'Max 20×',
        error: null,
        rejected: false,
      },
      spend: null,
    }),
    [five, week, now],
  );
  const alerts = query.alerts !== undefined;
  const [cleared] = useState(() => {
    if (alerts) localStorage.removeItem('hx-limit-alerts');
    return true;
  });
  useLimitAlerts(alerts && cleared ? limits : null);

  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);

  return (
    <PortalContext value={portalRef}>
      <div className="hx-root hx-smallfeats hx-dock">
        <Composer
          value={draft}
          onChange={setDraft}
          onSubmit={() => setDraft('')}
          mode="idle"
          onStop={() => Promise.resolve()}
          picker={{ value: choice, agents: [AGENT], locked: true, onChange: setChoice }}
          limits={limits}
          onMic={() => undefined}
          tools={<RoundButton icon="audio-lines" label="Живой голос" onClick={() => undefined} />}
          attachments={[]}
          onFiles={() => undefined}
          onRemoveAttachment={() => undefined}
          onRetryAttachment={() => undefined}
          recording={null}
          onCancelRecording={() => undefined}
          onFinishRecording={() => undefined}
          maxText={20000}
        />
        <div ref={portalRef} className="hx-portal" />
      </div>
      <ToastHost />
    </PortalContext>
  );
};

export default LimitringGallery;
