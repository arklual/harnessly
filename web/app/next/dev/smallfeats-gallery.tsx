// The small features of the field on made-up data, for the eye and for screenshots: ?dev=smallfeats. The stash in «+»,
// the several models of the picker, the goal bar over the field and the line in the feed where a goal was reached.
// ?theme=light|dark&lang=en|ru pick the look; ?goal=active|paused|met|none the bar. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import '@/next/styles/chat.css';
import '@/next/styles/settings.css';
import '@/next/dev/smallfeats-gallery.css';

import { useEffect, useRef, useState } from 'react';
import { PortalContext } from '@/next/ui/portal';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import BetaSection from '@/next/features/settings/beta-section';
import Composer from '@/next/features/composer/composer';
import GoalBar from '@/next/features/composer/goal-bar';
import GoalNote from '@/next/features/chat/goal-note';
import type { IModelChoice, IPickerAgent } from '@/next/features/composer/agents';
import { toggleChoice } from '@/next/features/composer/multi-model';
import type { IStash } from '@/next/features/composer/stash-list';
import type { GoalStatus } from '@/next/protocol/v2';

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
    {
      id: 'sonnet',
      label: 'Sonnet 5.5',
      hint: 'Быстрая и точная',
      efforts: [...levels],
      defaultEffort: 'medium',
    },
    { id: 'haiku', label: 'Haiku 4.5', hint: 'Самая быстрая', efforts: [], defaultEffort: null },
  ],
};

const GOAL = 'Довести контракт v2 до конца: схемы, тесты, README';

const SmallfeatsGallery = () => {
  const { query } = useRouter();
  const theme = query.theme === 'dark' ? 'dark' : 'light';
  pinLocale(query.lang === 'en' ? 'en' : 'ru');
  const goalState = (query.goal ?? 'active') as GoalStatus | 'none';
  const portalRef = useRef<HTMLDivElement>(null);
  const [beta, setBeta] = useState({ retitle: true, multiModel: false });
  const multi = query.multi !== undefined;
  const [draft, setDraft] = useState('Сделай форму оплаты с проверкой карты');
  const [rows, setRows] = useState(() => [
    { id: 'a', preview: 'Поправь отступы в карточке заказа', files: 0, at: Date.now() - 12 * 60_000 },
    { id: 'b', preview: '', files: 2, at: Date.now() - 3 * 3_600_000 },
  ]);
  const [chosen, setChosen] = useState<IModelChoice[]>([
    { provider: 'claude', model: 'opus', effort: 'high', ultracode: false },
    ...(multi
      ? [{ provider: 'claude' as const, model: 'sonnet', effort: 'medium' as const, ultracode: false }]
      : []),
  ]);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.toggle('dark', theme === 'dark');
    root.style.colorScheme = theme;
  }, [theme]);

  const stash: IStash = {
    rows,
    canStash: draft.trim() !== '',
    onStash: () => {
      setRows((have) => [{ id: String(Date.now()), preview: draft, files: 0, at: Date.now() }, ...have]);
      setDraft('');
    },
    onRestore: (id) => {
      setDraft((have) => [have, rows.find((r) => r.id === id)?.preview ?? ''].filter(Boolean).join('\n\n'));
      setRows((have) => have.filter((r) => r.id !== id));
    },
    onDrop: (id) => setRows((have) => have.filter((r) => r.id !== id)),
  };

  if (query.beta !== undefined)
    return (
      <div className="hx-root hx-smallfeats-set">
        <div className="hx-set__page-in">
          <BetaSection flags={beta} onChange={(flag, on) => setBeta((have) => ({ ...have, [flag]: on }))} />
        </div>
      </div>
    );
  return (
    <PortalContext value={portalRef}>
      <div className="hx-root hx-smallfeats hx-dock">
        {goalState === 'met' && <GoalNote text={GOAL} onContinue={() => Promise.resolve()} />}
        <GoalBar
          goal={goalState === 'none' ? null : { text: GOAL, status: goalState, since: 1 }}
          onStop={() => Promise.resolve()}
        />
        <Composer
          value={draft}
          onChange={setDraft}
          onSubmit={() => setDraft('')}
          mode="new"
          onStop={() => Promise.resolve()}
          picker={{
            value: chosen[0],
            agents: [AGENT],
            locked: false,
            onChange: (next) => setChosen([next]),
            multi: multi
              ? {
                  chosen,
                  onToggle: (next) => setChosen((have) => toggleChoice(have, next)),
                }
              : undefined,
          }}
          stash={stash}
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

export default SmallfeatsGallery;
