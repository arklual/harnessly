// «Задача | Разговор» over the new chat's field on made-up data (the motion of the switch): ?dev=chatkind.
// The segment's plate slides, the border flows to green and back, the hint crossfades, the pill and the limits fold away
// in a talk and unfold in a task; the wave (live voice) stays in both. &kind=talk starts in a talk, &auto=1 turns the
// kind every 1.6 s, &voice=off shows the quiet wave of a machine whose voice mode is off. ?theme=light|dark.
// Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/motion.css';
import '@/next/styles/chat.css';
import '@/next/styles/voice-live.css';

import { useEffect, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale, t } from '@/next/i18n';
import Composer from '@/next/features/composer/composer';
import KindSwitch, { CHAT_KIND, type ChatKind } from '@/next/features/composer/kind-switch';
import type { IModelChoice, IPickerAgent } from '@/next/features/composer/agents';
import type { LimitsView, ProviderLimits } from '@/next/features/limits/model';
import { RoundButton } from '@/next/ui/button';

const AGENT: IPickerAgent = {
  provider: 'claude',
  name: 'Claude Code',
  short: 'Claude',
  canSwitch: true,
  available: true,
  status: 'ready',
  models: [{ id: 'opus', label: 'Opus 5.5', hint: '', efforts: ['high'], defaultEffort: 'high' }],
};
const CHOICE: IModelChoice = { provider: 'claude', model: 'opus', effort: 'high', ultracode: false };
const at = Date.now() / 1000;
const LIMITS: LimitsView = {
  state: 'ready',
  provider: 'claude',
  spend: null,
  limits: {
    fiveHour: { usedPct: 34, resetsAt: at + 7200, forecast: null },
    sevenDay: { usedPct: 41, resetsAt: at + 90 * 3600, forecast: null },
    month: null,
    balance: null,
    estimate: false,
    unofficial: false,
    updatedAt: Date.now(),
    plan: null,
    error: null,
    rejected: false,
  } satisfies ProviderLimits,
};
const AUTO_MS = 1600;
const nothing = () => undefined;

const ChatkindGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale('ru');
  const [kind, setKind] = useState<ChatKind>(query.kind === 'talk' ? CHAT_KIND.talk : CHAT_KIND.task);
  const [text, setText] = useState('');
  const off = query.voice === 'off';
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);
  useEffect(() => {
    if (query.auto !== '1') return;
    const timer = setInterval(
      () => setKind((k) => (k === CHAT_KIND.task ? CHAT_KIND.talk : CHAT_KIND.task)),
      AUTO_MS,
    );
    return () => clearInterval(timer);
  }, [query.auto]);
  const talk = kind === CHAT_KIND.talk;
  return (
    <div
      className="hx-root hx-stage"
      style={{ width: '100vw', height: '100vh', background: 'var(--hx-bg-app)' }}
    >
      <div
        style={{
          width: '100%',
          maxWidth: 880,
          margin: '0 auto',
          padding: '120px 16px 0',
          boxSizing: 'border-box',
        }}
      >
        <div className="hx-dock" data-screen data-kind={kind} style={{ width: '100%' }}>
          <KindSwitch value={kind} onChange={setKind} top />
          <Composer
            value={text}
            onChange={setText}
            onSubmit={() => setText('')}
            mode="new"
            placeholder={talk ? t('next.kind.talkPlaceholder') : undefined}
            onStop={() => Promise.resolve()}
            tucked={talk}
            picker={{ value: CHOICE, agents: [AGENT], locked: false, onChange: nothing }}
            limits={LIMITS}
            tools={
              <span className="hx-voice-holder">
                <RoundButton
                  icon="audio-lines"
                  className="hx-voice-btn"
                  data-off={off ? '' : undefined}
                  aria-disabled={off || undefined}
                  label={t(off ? 'next.chatgpt.voice.offHint' : 'next.chatgpt.voice.newChat')}
                />
              </span>
            }
            onMic={nothing}
            attachments={[]}
            onFiles={nothing}
            onRemoveAttachment={nothing}
            onRetryAttachment={nothing}
            recording={null}
            onCancelRecording={nothing}
            onFinishRecording={nothing}
            maxText={100000}
          />
        </div>
      </div>
    </div>
  );
};

export default ChatkindGallery;
