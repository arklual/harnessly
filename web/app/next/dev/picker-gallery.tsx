// The model picker over a field in the middle of a short window: the screens that were cut to a stub.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/chat.css';
import '@/next/styles/composer.css';
import { useEffect, useState } from 'react';
import ModelPicker from '@/next/features/composer/model-picker';
import { type IModelChoice, type IPickerAgent } from '@/next/features/composer/agents';
import type { AgentModel, Effort } from '@/next/protocol/v2';

const levels: Effort[] = ['low', 'medium', 'high'];
const model = (id: string, label: string, hint = ''): AgentModel => ({
  id,
  label,
  hint,
  efforts: levels,
  defaultEffort: 'medium',
});

const agents: IPickerAgent[] = [
  {
    provider: 'claude',
    name: 'Claude Code',
    short: 'Claude',
    models: [model('opus', 'Opus 5.5'), model('sonnet', 'Sonnet 5.5'), model('haiku', 'Haiku 4.5')],
    canSwitch: true,
    available: true,
    status: 'ready',
  },
  {
    provider: 'claude',
    profile: 'kimi',
    vendor: 'kimi',
    name: 'Kimi Code',
    short: 'Kimi Code',
    models: [
      model('k31', 'K3.1 Pro'),
      model('k31hs', 'K3.1 Pro HS', 'Быстрая версия'),
      model('k3', 'K3'),
      model('k3-256', 'K3 · 256K', 'Меньше расход подписки'),
      model('kfc', 'Kimi for Coding', 'K2.8 Preview · для обычной разработки'),
      model('k27hs', 'K2.7 Code HS', 'Быстрая версия'),
      model('k27', 'K2.7 Code'),
      model('k26', 'K2.6 Code'),
    ],
    canSwitch: true,
    available: true,
    status: 'ready',
  },
];

const Gallery = () => {
  const [value, setValue] = useState<IModelChoice>({
    provider: 'claude',
    profile: 'kimi',
    model: 'k31',
    effort: 'high',
    ultracode: false,
  });
  useEffect(() => {
    document.documentElement.dataset.theme = 'dark';
    document.body.style.background = 'var(--hx-bg)';
  }, []);
  return (
    <div className="hx-root" style={{ position: 'absolute', inset: 0 }}>
      {/* The start screen's body clips what leaves it (ancestors with overflow), as the home page does under its header. */}
      <div
        style={{
          position: 'absolute',
          inset: '139px 0 0',
          overflow: 'hidden',
          display: 'grid',
          alignContent: 'end',
          paddingBottom: 90,
          justifyItems: 'center',
        }}
      >
        <div style={{ width: 520, padding: 20, borderRadius: 24, border: '1px solid var(--hx-border)' }}>
          <div style={{ height: 40, color: 'var(--hx-fg-3)' }}>Опишите задачу…</div>
          <ModelPicker
            value={value}
            agents={agents}
            locked={false}
            onChange={(next) => setValue(next)}
            offersUltracode={false}
            ultracodeAsked={null}
          />
        </div>
      </div>
    </div>
  );
};

export default Gallery;
