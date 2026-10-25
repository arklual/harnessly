// The extra model vendors on made-up data, for the eye and for screenshots: ?dev=providers. Never a real key: the keys
// typed here go nowhere and only their last digits are kept. ?scene=settings|picker|chat picks the page, ?state=empty|kimi|
// all the keys on the machine, ?check=ok|auth|network|limit what the check of a connection finds,
// ?theme=light|dark&lang=en|ru the look. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/chat.css';
import '@/next/styles/settings.css';
import '@/next/dev/providers-gallery.css';

import { useEffect, useRef, useState } from 'react';
import Composer from '@/next/features/composer/composer';
import { pickerAgents, type IModelChoice } from '@/next/features/composer/agents';
import type { IPresetState, PresetId, TestState } from '@/next/features/providers/model';
import ModelsSection from '@/next/features/providers/models-section';
import { pinLocale } from '@/next/i18n';
import type { MachineAgent } from '@/next/protocol/v2';
import { PortalContext } from '@/next/ui/portal';
import { useRouter } from '@/platform/router';

const levels = ['low', 'medium', 'high', 'xhigh', 'max'] as const;

const MACHINE_AGENTS = [
  {
    provider: 'claude',
    version: '2.1.310',
    status: 'ready',
    canSwitchModel: true,
    models: [
      { id: 'opus', label: 'Opus 5.5', hint: 'Самая сильная', efforts: [...levels], defaultEffort: 'high' },
      {
        id: 'sonnet',
        label: 'Sonnet 5.5',
        hint: 'Быстрая и точная',
        efforts: [...levels],
        defaultEffort: 'medium',
      },
    ],
    profiles: [
      {
        id: 'kimi',
        label: 'Kimi',
        vendor: 'kimi',
        configured: true,
        models: [
          { id: 'kimi-k3.1-pro', label: 'K3.1 Pro', hint: '1M контекста', efforts: [], defaultEffort: null },
          {
            id: 'kimi-k2.7-code',
            label: 'K2.7 Code',
            hint: 'Быстрее и дешевле',
            efforts: [],
            defaultEffort: null,
          },
        ],
      },
      {
        id: 'glm',
        label: 'GLM',
        vendor: 'glm',
        configured: true,
        models: [
          { id: 'glm-5.3', label: 'GLM 5.3', hint: 'Только текст', efforts: [], defaultEffort: null },
          { id: 'glm-5.3-flash', label: 'GLM 5.3 Flash', hint: '', efforts: [], defaultEffort: null },
        ],
      },
    ],
  },
  {
    provider: 'codex',
    version: '0.152.0',
    status: 'ready',
    canSwitchModel: true,
    models: [
      { id: 'gpt-6', label: 'GPT-6', hint: '', efforts: ['low', 'medium', 'high'], defaultEffort: 'medium' },
    ],
    profiles: [
      {
        id: 'xai',
        label: 'Grok',
        vendor: 'grok',
        configured: true,
        models: [
          { id: 'grok-build-0.1', label: 'Grok Build', hint: 'Окно 256k', efforts: [], defaultEffort: null },
          { id: 'grok-4.7', label: 'Grok 4.7', hint: 'Окно 500k', efforts: [], defaultEffort: null },
        ],
      },
    ],
  },
] as unknown as MachineAgent[];

const model = (id: string, label: string) => ({ id, label, hint: '', efforts: [], defaultEffort: null });
const PRESETS: readonly IPresetState[] = [
  {
    id: 'kimi',
    label: 'Kimi · платформа',
    vendor: 'kimi',
    via: 'claude',
    host: 'api.moonshot.ai',
    hint: 'Ключ с platform.kimi.ai, оплата за токены',
    key: { configured: false, tail: null },
    models: [model('kimi-k3.1-pro', 'K3.1 Pro')],
  },
  {
    id: 'kimi-code',
    label: 'Kimi · Kimi Code',
    vendor: 'kimi',
    via: 'claude',
    host: 'api.kimi.com',
    hint: 'Ключ подписки Kimi Code',
    key: { configured: false, tail: null },
    models: [model('kimi-for-coding', 'Kimi for coding')],
  },
  {
    id: 'glm',
    label: 'GLM · Z.ai',
    vendor: 'glm',
    via: 'claude',
    host: 'api.z.ai',
    hint: 'Ключ с z.ai: тарифа Coding Plan или обычный ключ API. Они не взаимозаменяемы',
    key: { configured: false, tail: null },
    models: [model('glm-5.3', 'GLM 5.3')],
  },
  {
    id: 'glm-cn',
    label: 'GLM · bigmodel.cn',
    vendor: 'glm',
    via: 'claude',
    host: 'open.bigmodel.cn',
    hint: 'Ключ с open.bigmodel.cn, для китайского сервиса',
    key: { configured: false, tail: null },
    models: [model('glm-5.3', 'GLM 5.3')],
  },
  {
    id: 'xai',
    label: 'Grok',
    vendor: 'grok',
    via: 'codex',
    host: 'api.x.ai',
    hint: 'Ключ API с console.x.ai',
    key: { configured: false, tail: null },
    models: [model('grok-build-0.1', 'Grok Build')],
  },
];

const startStates = (state: string | undefined): IPresetState[] =>
  PRESETS.map((p) => {
    const on = state === 'all' || (state === 'kimi' && p.id === 'kimi');
    return on ? { ...p, key: { configured: true, tail: '…3456' } } : { ...p };
  });

const tailOf = (key: string) => `…${key.slice(-4)}`;

const SettingsScene = ({ state, check }: { state?: string; check?: string }) => {
  const [states, setStates] = useState(() => startStates(state));
  const change = (id: PresetId, patch: Partial<IPresetState>) =>
    setStates((have) => have.map((p) => (p.id === id ? { ...p, ...patch } : p)));
  const result = (): TestState => {
    if (check === 'auth') return { k: 'failed', reason: 'unauthorized' };
    if (check === 'network') return { k: 'failed', reason: 'unreachable' };
    if (check === 'limit') return { k: 'failed', reason: 'rate-limited' };
    return { k: 'ok' };
  };
  return (
    <div className="hx-root hx-providers-set">
      <div className="hx-set__page-in">
        <ModelsSection
          states={states}
          onSave={async (id, key) => {
            await new Promise((resolve) => setTimeout(resolve, 500));
            change(id, { key: { configured: true, tail: tailOf(key) } });
            return null;
          }}
          onRemove={async (id) => {
            await new Promise((resolve) => setTimeout(resolve, 400));
            change(id, { key: { configured: false, tail: null } });
            return null;
          }}
          onTest={async () => {
            await new Promise((resolve) => setTimeout(resolve, 900));
            return result();
          }}
        />
      </div>
    </div>
  );
};

const PickerScene = ({ locked = false }: { locked?: boolean }) => {
  const portalRef = useRef<HTMLDivElement>(null);
  const all = pickerAgents(MACHINE_AGENTS);
  // A created chat shows only its own group.
  const agents = locked ? all.filter((a) => a.profile === 'kimi') : all;
  const [choice, setChoice] = useState<IModelChoice>({
    provider: 'claude',
    profile: 'kimi',
    model: 'kimi-k3.1-pro',
    effort: null,
    ultracode: false,
  });
  const [draft, setDraft] = useState('');
  return (
    <PortalContext value={portalRef}>
      <div className="hx-root hx-providers-dock hx-dock">
        <Composer
          value={draft}
          onChange={setDraft}
          onSubmit={() => setDraft('')}
          mode="new"
          onStop={() => Promise.resolve()}
          picker={{ value: choice, agents, locked, onChange: setChoice }}
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
    </PortalContext>
  );
};

const ProvidersGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);
  if (query.scene === 'picker') return <PickerScene />;
  if (query.scene === 'chat') return <PickerScene locked />;
  return <SettingsScene state={String(query.state ?? '')} check={String(query.check ?? '')} />;
};

export default ProvidersGallery;
