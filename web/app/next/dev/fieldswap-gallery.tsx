// The field's bar over a change of chat on made-up data, for a recording: ?dev=fieldswap changes between two
// chats every two seconds — the model's pill (Opus · Высокое ↔ Haiku), the limits chip and the context ring — and for
// 300 ms after each change the next chat's limits and window have not come yet (null), as a chat that has just opened.
// ?theme=light|dark. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/motion.css';
import '@/next/styles/chat.css';

import { useEffect, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import Composer from '@/next/features/composer/composer';
import type { IModelChoice, IPickerAgent } from '@/next/features/composer/agents';
import type { LimitsView, ProviderLimits } from '@/next/features/limits/model';

const levels = ['low', 'medium', 'high', 'xhigh', 'max'] as const;
const AGENT: IPickerAgent = {
  provider: 'claude',
  name: 'Claude Code',
  short: 'Claude',
  canSwitch: true,
  available: true,
  status: 'ready',
  models: [
    { id: 'opus', label: 'Opus 5.5', hint: 'Самая сильная', efforts: [...levels], defaultEffort: 'high' },
    { id: 'haiku', label: 'Haiku 4.5', hint: 'Самая быстрая', efforts: [], defaultEffort: null },
  ],
};
const CHOICES: IModelChoice[] = [
  { provider: 'claude', model: 'opus', effort: 'high', ultracode: false },
  { provider: 'claude', model: 'haiku', effort: null, ultracode: false },
];
const HOUR = 3600;
const limitsOf = (five: number, week: number): LimitsView => {
  const at = Date.now() / 1000;
  const limits: ProviderLimits = {
    fiveHour: { usedPct: five, resetsAt: at + 2 * HOUR, forecast: null },
    sevenDay: { usedPct: week, resetsAt: at + 90 * HOUR, forecast: null },
    month: null,
    balance: null,
    estimate: false,
    unofficial: false,
    updatedAt: Date.now(),
    plan: null,
    error: null,
    rejected: false,
  };
  return { state: 'ready', provider: 'claude', limits, spend: null };
};
const LIMITS = [limitsOf(0, 12), limitsOf(64, 41)];
const RINGS = [
  { used: 52_000, limit: 200_000, compacting: false },
  { used: 151_000, limit: 200_000, compacting: false },
];
const SWITCH_MS = 2000;
const LATE_MS = 300;
const nothing = () => undefined;

const FieldswapGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale('ru');
  const [chat, setChat] = useState(0);
  const [late, setLate] = useState(false);
  const [text, setText] = useState('');
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);
  useEffect(() => {
    const timer = setInterval(() => {
      setChat((c) => 1 - c);
      setLate(true);
    }, SWITCH_MS);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    if (!late) return;
    const timer = setTimeout(() => setLate(false), LATE_MS);
    return () => clearTimeout(timer);
  }, [late]);
  return (
    <div className="hx-root hx-app" data-chat={chat}>
      <div className="hx-dock" data-screen>
        <Composer
          value={text}
          onChange={setText}
          onSubmit={() => setText('')}
          mode="idle"
          placeholder={`Чат ${chat + 1}`}
          onStop={() => Promise.resolve()}
          picker={{ value: CHOICES[chat], agents: [AGENT], locked: true, onChange: nothing }}
          limits={late ? null : LIMITS[chat]}
          contextRing={late ? undefined : RINGS[chat]}
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
  );
};

export default FieldswapGallery;
