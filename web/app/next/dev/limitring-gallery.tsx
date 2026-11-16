// The limit on the model pill on made-up data, for the eye and for screenshots: ?dev=limitring. The field
// with «+», the pill (its outline the spent five hours), the live voice, the mic and send; a tap on the pill opens the
// card of both windows. ?five=<left %>&week=<left %> set the windows, ?theme=light|dark&lang=en|ru the look; ?alerts
// tells the steps reached at the top (the memory is cleared first); ?working shows the field in a turn (stop, the
// context ring), ?fc a weekly forecast that runs out before the reset; ?kind=task|talk the «Задача | Разговор» segment
// over the field, ?plan=week a plan of the week alone with Fable's own week, ?stale a five-hour reading of a window that has reset, ?full the popover with everything (ChatGPT, the machine, a long name) for its fit in a low window. Never part of a production path.
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
import KindSwitch, { CHAT_KIND, type ChatKind } from '@/next/features/composer/kind-switch';
import '@/next/styles/composer.css';

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
  const working = query.working !== undefined;
  const full = query.full !== undefined;
  const [kind, setKind] = useState<ChatKind | null>(() => {
    if (query.kind === CHAT_KIND.talk) return CHAT_KIND.talk;
    return query.kind === CHAT_KIND.task ? CHAT_KIND.task : null;
  });
  const talk = kind === CHAT_KIND.talk;
  const runsOut = query.fc !== undefined;
  const stale = query.stale !== undefined;
  const plan = query.plan === 'week';
  const limits = useMemo<LimitsView>(
    () => ({
      state: 'ready',
      provider: 'claude',
      limits: {
        fiveHour: plan
          ? null
          : {
              usedPct: 100 - five,
              resetsAt: stale ? now - 60 * 3600 : now + 2 * 3600 + 25 * 60,
              forecast: null,
            },
        sevenDay: {
          usedPct: 100 - week,
          resetsAt: now + 3 * 86400 + 5 * 3600,
          forecast: runsOut
            ? {
                status: 'exhausts_before_reset',
                exhaustAt: now + 86400 + 4 * 3600,
                exhaustRange: null,
                usedAtReset: null,
                confidence: 'medium',
                reason: null,
                lastAt: null,
                resetsAt: now + 3 * 86400 + 5 * 3600,
                profile: true,
              }
            : null,
        },
        month: null,
        balance: null,
        estimate: false,
        unofficial: false,
        updatedAt: now * 1000,
        plan: 'Max 20×',
        error: null,
        rejected: false,
        extra: plan
          ? [
              {
                id: 'seven_day_model_fable',
                title: 'Fable',
                window: { usedPct: 20, resetsAt: now + 3 * 86400, forecast: null },
              },
            ]
          : [],
      },
      spend: null,
      others: [
        {
          key: 'codex',
          name: null,
          provider: 'codex',
          limits: {
            fiveHour: null,
            sevenDay: { usedPct: 25, resetsAt: now + 4 * 86400, forecast: null },
            month: null,
            balance: null,
            estimate: false,
            unofficial: false,
            updatedAt: null,
            plan: null,
            error: null,
            rejected: false,
          },
        },
      ],
    }),
    [five, week, now, runsOut, stale, plan],
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
      <div className="hx-root hx-smallfeats hx-dock" data-kind={talk ? 'talk' : undefined}>
        {kind && <KindSwitch value={kind} onChange={setKind} />}
        <Composer
          value={draft}
          onChange={setDraft}
          onSubmit={() => setDraft('')}
          mode={working ? 'working' : 'idle'}
          onStop={() => Promise.resolve()}
          placeholder={talk ? 'Спросите о чём угодно — просто разговор с моделью' : undefined}
          picker={
            talk
              ? null
              : {
                  value: choice,
                  agents: [AGENT],
                  locked: !full,
                  onChange: setChoice,
                  machine: full
                    ? {
                        current: 'vm',
                        choices: [
                          {
                            id: 'vm',
                            name: 'dev-department-very-long-machine',
                            health: 'ok',
                            note: '',
                            off: false,
                          },
                          { id: 'mac', name: 'MacBook', health: 'ok', note: '', off: false },
                        ],
                        onPick: () => undefined,
                      }
                    : null,
                  extra: full
                    ? {
                        label: 'ChatGPT',
                        sub: 'Обычный чат на вашем плане',
                        models: [{ id: 'gpt', label: 'GPT-5.5' }],
                        signIn: null,
                        onPick: () => undefined,
                      }
                    : null,
                }
          }
          limits={talk ? null : limits}
          contextRing={working ? { used: 82_000, limit: 200_000, compacting: false } : null}
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
